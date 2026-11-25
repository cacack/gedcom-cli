package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"unicode"

	"github.com/cacack/gedcom-go/v3/gedcom"
	"github.com/cacack/gedcom-go/v3/merge"
	"github.com/spf13/cobra"
)

// combinePrefix is the --strategy that takes --prefix, and combineZip the one
// that takes --dropped.
const (
	combinePrefix = "prefix"
	combineZip    = "zip"
)

// combineStrategies maps each --strategy to the library's collision strategy.
// prefix renames every record of a later file before combining it (see
// combineDocuments), so an xref that still collides is an error.
var combineStrategies = map[string]merge.CollisionStrategy{
	combineZip:    merge.ZipSharedXRefs,
	combinePrefix: merge.ErrorOnCollision,
	"renumber":    merge.RenumberDoc2,
}

// combineReport is what combine prints: the file written, what it holds, what
// each input's xrefs became, and what could not be joined.
type combineReport struct {
	Output          string                  `json:"output"`
	Strategy        string                  `json:"strategy"`
	Stamped         bool                    `json:"stamped"`
	Records         int                     `json:"records"`
	Individuals     int                     `json:"individuals"`
	Families        int                     `json:"families"`
	Inputs          []combineInput          `json:"inputs"`
	HeaderConflicts []combineHeaderConflict `json:"header_conflicts"`
	Conflicts       []combineConflict       `json:"conflicts"`
}

// combineInput is one input file: its declared encoding, and each of its xrefs
// that was renamed in the output.
type combineInput struct {
	File     string            `json:"file"`
	Encoding string            `json:"encoding"`
	Remapped map[string]string `json:"remapped"`
}

// combineHeaderConflict is merge.HeaderConflict naming the input whose value
// was dropped.
type combineHeaderConflict struct {
	File    string `json:"file"`
	Field   string `json:"field"`
	Kept    string `json:"kept"`
	Dropped string `json:"dropped"`
}

// combineConflict is a record zip could not join: the input's record with
// that xref differs from the one already combined in more than its pointers.
type combineConflict struct {
	File   string `json:"file"`
	XRef   string `json:"xref"`
	Reason string `json:"reason"`
}

func newCombineCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "combine <file>... --strategy zip|prefix|renumber -o <out>",
		Short: "Join GEDCOM files into one, writing a new file",
		Long: `Join GEDCOM files into one, with gedcom-go's merge.Combine, and write it to
--output. The files are joined in the order given: the first is kept as it
is, and each later one is added to what came before it. No people are
merged: a person in two unrelated files stays two people.

--strategy says what to do with an xref two files both hold:

  zip       The files were cut from one master (by 'gedcom extract'), so a
            shared xref is the same record. Its two copies are joined into
            one, putting back the HUSB, WIFE, CHIL, FAMC and FAMS lines (and
            other pointers to people and families) each file dropped at its
            boundary. Copies that differ in anything else conflict: every
            conflicting xref is listed, the command exits 1, and nothing is
            written. Nothing is renamed. Records come in the order of the
            first file that holds them. A family split between files gets
            its HUSB, WIFE and CHIL lines back in one file's line order, not
            always the master's (CHIL order is birth order), unless --dropped
            is given.
  prefix    The files are unrelated trees. Every record of each later file
            gets that file's --prefix, whether or not its xref collides, so
            a record's origin can be read from its xref: with --prefix B_, a
            second file's @I1@ becomes @B_I1@. The first file's xrefs are
            kept. Give one prefix per file after the first, each its own,
            with no "@" or whitespace. A prefixed xref that an earlier file
            already holds is an error: the command exits 1, and nothing is
            written. A record whose xref is malformed (not @...@) keeps it,
            and is not listed as renamed.
  renumber  The files are unrelated trees. Only the colliding xrefs of a
            later file are renamed, each to the next free number of its
            kind (@I1@ becomes @I43@). Every other xref is kept.

--dropped, with zip, puts the joined pointer lines back in the master's
order. Save the report of each extract after the first, and pass them in the
order of those files, one per file:

  gedcom extract --format json tree.ged I1 I2 -o a.ged > a.json
  gedcom extract --format json tree.ged I3 I4 -o b.ged > b.json
  gedcom combine a.ged b.ged --strategy zip --dropped b.json -o tree2.ged

Each report's "dropped" list says where every removed line sat in its record.
A report belongs to the file as extract wrote it: do not pass one for a file
edited since. Where a report does not fit its file, the lines are joined as
without it: check the result with 'gedcom diff' against the master. A report
that names another file than the one it is given for is a warning on stderr.

Zipping extracts back reproduces only the records reachable from the people
extracted. A record nothing cites (a SUBM, REPO or SOUR with no pointer to
it) is in no extract, and so not in the output.

The report lists each input's renamed xrefs (JSON "remapped", old to new):
with prefix, every xref of each file after the first; with renumber, the
colliding ones; with zip, none. With prefix and renumber every record of
every input is kept.

The files must share a GEDCOM version (5.5, 5.5.1 or 7.0); mixing them
fails. They may declare different encodings (ANSEL, UTF-8, ...): each is
decoded as it declares, and the output is always UTF-8 (JSON "encoding"
gives each input's). The header is the first file's alone, under every
strategy: each structure of a later file's header (its SOUR, DEST, NOTE,
vendor tags, ...) is dropped, and listed in JSON "header_conflicts" (in
text, a warning on stderr counts the values that differed or that the first
file's header lacks).

The first file's header is then stamped, since the output is a new dataset
made by this tool. --keep-header writes it as read instead.

The output is written normalized, since it was read from no one file. Given
one UTF-8 file and --keep-header, combine writes it back exactly as it was
read. --redact-living applies to the output file, not only to the report.

` + stampHelp,
		Example: `  gedcom combine paternal.ged maternal.ged --strategy zip -o tree.ged
  gedcom combine paternal.ged maternal.ged --strategy zip --dropped maternal.json -o tree.ged
  gedcom combine mine.ged cousin.ged --strategy prefix --prefix C_ -o both.ged
  gedcom combine --format json a.ged b.ged c.ged --strategy prefix --prefix B_,C_ -o all.ged
  gedcom combine a.ged b.ged --strategy renumber -o both.ged`,
		Args: minArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			format, err := outputFormat(cmd)
			if err != nil {
				return err
			}
			flags := cmd.Flags()
			out, err := outputPath(cmd, args...)
			if err != nil {
				return err
			}
			name, _ := flags.GetString("strategy")
			prefixes, _ := flags.GetStringSlice("prefix")
			reports, _ := flags.GetStringSlice("dropped")
			living, err := readLivingOptions(cmd)
			if err != nil {
				return err
			}
			stamp, err := stampRequested(cmd)
			if err != nil {
				return err
			}
			// Changed, not the count: an empty --dropped value is no report,
			// and must not pass for the flag not being given.
			givenDropped := flags.Changed("dropped")
			_, ok := combineStrategies[name]
			switch {
			case !ok:
				return usageError{fmt.Errorf(`invalid --strategy %q: want "zip", "prefix" or "renumber"`, name)}
			case name == combinePrefix && len(prefixes) != len(args)-1:
				return usageError{fmt.Errorf("--strategy prefix needs one --prefix per file after the first: got %d, want %d", len(prefixes), len(args)-1)}
			case name != combinePrefix && len(prefixes) > 0:
				return usageError{errors.New("--prefix needs --strategy prefix")}
			case givenDropped && name != combineZip:
				return usageError{errors.New("--dropped needs --strategy zip")}
			case givenDropped && len(reports) != len(args)-1:
				return usageError{fmt.Errorf("--dropped needs one report per file after the first (the first file needs none): got %d, want %d", len(reports), len(args)-1)}
			}
			if err := checkPrefixes(prefixes); err != nil {
				return err
			}
			var dropped [][]gedcom.DroppedPointer
			for i, path := range reports {
				of, d, err := readDropped(path)
				if err != nil {
					return err
				}
				dropped = append(dropped, d)
				// A report that is not its file's is not an error to the
				// library: the lines are joined as without it. As in extract,
				// with -o - and JSON stderr holds only the JSON report.
				file := args[i+1]
				if of != "" && of != outputName(stdio) && filepath.Base(of) != filepath.Base(file) && (out != stdio || format != formatJSON) {
					fmt.Fprintf(cmd.ErrOrStderr(), "gedcom: warning: %s is the report of %s, not of %s: if it is another file's, the order is not restored\n", path, of, file)
				}
			}
			docs := make([]*gedcom.Document, len(args))
			for i, in := range args {
				res, err := decodeForWrite(in)
				if err != nil {
					return err
				}
				docs[i] = res.Document
			}
			doc, report, err := combineDocuments(args, docs, name, prefixes, dropped)
			if err != nil {
				return err
			}
			report.Strategy = name
			if len(report.Conflicts) > 0 {
				if err := printCombineReport(reportWriter(cmd, out), format, report); err != nil {
					return err
				}
				xrefs := make([]string, len(report.Conflicts))
				for i, c := range report.Conflicts {
					xrefs[i] = c.XRef
				}
				return fmt.Errorf("%s conflict, nothing written: %s", plural(len(xrefs), "record", "records"), strings.Join(xrefs, ", "))
			}

			if living.redact {
				if err := redactDocument(hiddenRecords(doc, presumedLiving(doc, living.cfg))); err != nil {
					return err
				}
			}
			if stamp {
				if err := stampHeader(doc); err != nil {
					return err
				}
				report.Stamped = true
			}
			if err := writeOutput(cmd, out, doc); err != nil {
				return err
			}
			if n := droppedHeaderValues(report.HeaderConflicts); n > 0 && format != formatJSON {
				fmt.Fprintf(cmd.ErrOrStderr(), "gedcom: warning: %s from later files dropped (see --format json)\n", plural(n, "differing header value", "differing header values"))
			}
			report.Output = out
			report.Records = len(doc.Records)
			report.Individuals = len(doc.Individuals())
			report.Families = len(doc.Families())
			return printCombineReport(reportWriter(cmd, out), format, report)
		},
	}
	addOutputFlag(cmd)
	cmd.Flags().String("strategy", "", `how to join records the files share: "zip", "prefix" or "renumber" (required)`)
	cmd.Flags().StringSlice("prefix", nil, "with --strategy prefix, the prefix for each file after the first, comma-separated or repeated")
	cmd.Flags().StringSlice("dropped", nil, "with --strategy zip, the saved 'extract --format json' report of each file after the first, comma-separated or repeated")
	addKeepHeaderFlag(cmd)
	return cmd
}

// checkPrefixes refuses a --prefix that cannot start an xref, or that two
// files would share.
func checkPrefixes(prefixes []string) error {
	for i, prefix := range prefixes {
		switch {
		case prefix == "" || strings.Contains(prefix, "@") || strings.ContainsFunc(prefix, unicode.IsSpace):
			return usageError{fmt.Errorf(`invalid --prefix %q: must not be empty or contain "@" or whitespace`, prefix)}
		case slices.Contains(prefixes[:i], prefix):
			return usageError{fmt.Errorf("--prefix %q is given twice: each file needs its own", prefix)}
		}
	}
	return nil
}

// readDropped reads a saved 'extract --format json' report: the file extract
// wrote, and its "dropped" list as the library reported it.
func readDropped(path string) (output string, dropped []gedcom.DroppedPointer, err error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", nil, err
	}
	var report extractReport
	if err := json.Unmarshal(b, &report); err != nil {
		return "", nil, fmt.Errorf("%s: not an extract --format json report: %w", path, err)
	}
	// extract always writes the list, empty or not.
	if report.Dropped == nil {
		return "", nil, fmt.Errorf(`%s: not an extract --format json report: no "dropped" list`, path)
	}
	dropped = make([]gedcom.DroppedPointer, len(report.Dropped))
	for i, d := range report.Dropped {
		// Every restorable pointer has a position, so one without is from
		// an extract that did not record them, and would order nothing.
		if d.Restorable && d.Position == 0 {
			return "", nil, fmt.Errorf(`%s: dropped pointer %s has no "position": run extract again to save a report that has one`, path, d)
		}
		dropped[i] = d.pointer()
	}
	return report.Output, dropped, nil
}

// combineDocuments joins docs in order with merge.Combine, under the strategy
// named. With combinePrefix, each doc after the first is first renamed whole
// with its prefix, by merge.RemapXRefs. dropped, when not nil, holds for
// each doc after the first what extract reported dropping from it, which zip
// uses to order the pointer lines it joins. A zip conflict does not stop it:
// every input is compared, so the report names every conflict, and the
// document returned must then not be written.
func combineDocuments(files []string, docs []*gedcom.Document, name string, prefixes []string, dropped [][]gedcom.DroppedPointer) (*gedcom.Document, combineReport, error) {
	report := combineReport{
		Inputs:          make([]combineInput, len(docs)),
		HeaderConflicts: []combineHeaderConflict{},
		Conflicts:       []combineConflict{},
	}
	for i, doc := range docs {
		report.Inputs[i] = combineInput{File: files[i], Remapped: map[string]string{}}
		if doc.Header != nil {
			report.Inputs[i].Encoding = string(doc.Header.Encoding)
		}
	}
	out := docs[0]
	for i := 1; i < len(docs); i++ {
		opts := merge.CombineOptions{
			CollisionStrategy: combineStrategies[name],
			// Every input is decoded to Unicode and the output is UTF-8,
			// so a declared encoding is provenance only.
			IgnoreEncodingMismatch: true,
			// The header is the first file's alone: every later file's
			// header structure is dropped and listed in header_conflicts.
			KeepFirstHeader: true,
			// Keep going so that every conflict is reported; nothing is
			// written when there is one.
			KeepFirstOnConflict: true,
		}
		if dropped != nil {
			opts.Zip = &merge.ZipOptions{DroppedFromDoc2: dropped[i-1]}
		}
		doc := docs[i]
		// prefixed is what the prefix strategy renamed, before Combine;
		// the other strategies rename, if at all, in Combine.
		var prefixed map[string]string
		if name == combinePrefix {
			prefix := prefixes[i-1]
			var err error
			doc, prefixed, err = merge.RemapXRefs(doc, func(old string) string { return "@" + prefix + old[1:] })
			if err != nil {
				return nil, report, fmt.Errorf("combine %s: %w", files[i], err)
			}
		}
		combined, res, err := merge.Combine(out, doc, opts)
		if err != nil {
			return nil, report, fmt.Errorf("combine %s: %w", files[i], err)
		}
		out = combined
		report.Inputs[i].Remapped = res.RemappedXRefs
		if prefixed != nil {
			report.Inputs[i].Remapped = prefixed
		}
		for _, c := range res.HeaderConflicts {
			if c.Field == "Encoding" {
				continue // reported per input, and never written
			}
			report.HeaderConflicts = append(report.HeaderConflicts, combineHeaderConflict{File: files[i], Field: c.Field, Kept: c.Doc1, Dropped: c.Doc2})
		}
		for _, c := range res.RecordConflicts {
			report.Conflicts = append(report.Conflicts, combineConflict{File: files[i], XRef: c.XRef, Reason: string(c.Reason)})
		}
	}
	return out, report, nil
}

// printCombineReport writes the report: as JSON, or as a summary line (or, on
// a conflict, none) followed by one tab-separated line per renamed xref (file,
// old, new) or per conflict (file, xref, "conflict", reason).
func printCombineReport(w io.Writer, format string, report combineReport) error {
	if format == formatJSON {
		return writeJSON(w, report)
	}
	if len(report.Conflicts) > 0 {
		for _, c := range report.Conflicts {
			if _, err := fmt.Fprintf(w, "%s\t%s\tconflict\t%s\n", c.File, c.XRef, c.Reason); err != nil {
				return err
			}
		}
		return nil
	}
	if _, err := fmt.Fprintf(w, "wrote %s: %s, %s, %s%s\n", report.Output,
		plural(report.Records, "record", "records"), plural(report.Individuals, "individual", "individuals"),
		plural(report.Families, "family", "families"), stampedText(report.Stamped)); err != nil {
		return err
	}
	for _, in := range report.Inputs {
		for _, old := range slices.Sorted(maps.Keys(in.Remapped)) {
			if _, err := fmt.Fprintf(w, "%s\t%s\t%s\n", in.File, old, in.Remapped[old]); err != nil {
				return err
			}
		}
	}
	return nil
}

// droppedHeaderValues counts the header conflicts where a later file's value
// differed from the one kept, or was not in the first file's header at all. A
// "Tags." conflict for a structure the first file also has (Kept "kept") is
// not counted: the library reports one for every such structure, alike or not.
func droppedHeaderValues(conflicts []combineHeaderConflict) int {
	n := 0
	for _, c := range conflicts {
		if !strings.HasPrefix(c.Field, "Tags.") || c.Kept == "" {
			n++
		}
	}
	return n
}
