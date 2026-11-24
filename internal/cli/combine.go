package cli

import (
	"errors"
	"fmt"
	"io"
	"maps"
	"slices"
	"strings"

	"github.com/cacack/gedcom-go/v3/gedcom"
	"github.com/cacack/gedcom-go/v3/merge"
	"github.com/spf13/cobra"
)

// combinePrefix is the --strategy that takes --prefix.
const combinePrefix = "prefix"

// combineStrategies maps each --strategy to the library's collision strategy.
// prefix renames every record of a later file before combining it (see
// combineDocuments), so an xref that still collides is an error.
var combineStrategies = map[string]merge.CollisionStrategy{
	"zip":         merge.ZipSharedXRefs,
	combinePrefix: merge.ErrorOnCollision,
	"renumber":    merge.RenumberDoc2,
}

// combineReport is what combine prints: the file written, what it holds, what
// each input's xrefs became, and what could not be joined.
type combineReport struct {
	Output          string                  `json:"output"`
	Strategy        string                  `json:"strategy"`
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
            first file that holds them, and joined pointer lines in each
            file's line order, so the output holds the master's records but
            not always in the master's order.
  prefix    The files are unrelated trees. Every record of each later file
            gets that file's --prefix, whether or not its xref collides, so
            a record's origin can be read from its xref: with --prefix B_, a
            second file's @I1@ becomes @B_I1@. The first file's xrefs are
            kept. Give one prefix per file after the first. A prefixed xref
            that an earlier file already holds is an error: the command
            exits 1, and nothing is written.
  renumber  The files are unrelated trees. Only the colliding xrefs of a
            later file are renamed, each to the next free number of its
            kind (@I1@ becomes @I43@). Every other xref is kept.

The report lists each input's renamed xrefs (JSON "remapped", old to new):
with prefix, every xref of each file after the first; with renumber, the
colliding ones; with zip, none. With prefix and renumber every record of
every input is kept.

The files must share a GEDCOM version (5.5, 5.5.1 or 7.0); mixing them
fails. They may declare different encodings (ANSEL, UTF-8, ...): each is
decoded as it declares, and the output is always UTF-8 (JSON "encoding"
gives each input's). The header is the first file's: what a later file's
header holds beyond it is dropped, and listed in JSON "header_conflicts"
(in text, a warning on stderr counts the values that differed).

The output is written normalized, since it was read from no one file. Given
one UTF-8 file, combine writes it back exactly as it was read.
--redact-living applies to the output file, not only to the report.`,
		Example: `  gedcom combine paternal.ged maternal.ged --strategy zip -o tree.ged
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
			living, err := readLivingOptions(cmd)
			if err != nil {
				return err
			}
			strategy, ok := combineStrategies[name]
			switch {
			case !ok:
				return usageError{fmt.Errorf(`invalid --strategy %q: want "zip", "prefix" or "renumber"`, name)}
			case name == combinePrefix && len(prefixes) != len(args)-1:
				return usageError{fmt.Errorf("--strategy prefix needs one --prefix per file after the first: got %d, want %d", len(prefixes), len(args)-1)}
			case name == combinePrefix && slices.Contains(prefixes, ""):
				return usageError{errors.New("--prefix must not be empty")}
			case name != combinePrefix && len(prefixes) > 0:
				return usageError{errors.New("--prefix needs --strategy prefix")}
			}
			docs := make([]*gedcom.Document, len(args))
			for i, in := range args {
				res, err := decodeForWrite(in)
				if err != nil {
					return err
				}
				docs[i] = res.Document
			}
			doc, report, err := combineDocuments(args, docs, name, strategy, prefixes)
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
	return cmd
}

// combineDocuments joins docs in order with merge.Combine. With the prefix
// strategy (name is combinePrefix), each doc after the first is first renamed
// whole with its prefix, by merge.RemapXRefs. A zip conflict does not stop it:
// every input is compared, so the report names every conflict, and the
// document returned must then not be written.
func combineDocuments(files []string, docs []*gedcom.Document, name string, strategy merge.CollisionStrategy, prefixes []string) (*gedcom.Document, combineReport, error) {
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
			CollisionStrategy: strategy,
			// Every input is decoded to Unicode and the output is UTF-8,
			// so a declared encoding is provenance only.
			IgnoreEncodingMismatch: true,
			// Keep going so that every conflict is reported; nothing is
			// written when there is one.
			KeepFirstOnConflict: true,
		}
		doc := docs[i]
		if name == combinePrefix {
			prefix := prefixes[i-1]
			prefixed, mapping, err := merge.RemapXRefs(doc, func(old string) string { return "@" + prefix + old[1:] })
			if err != nil {
				return nil, report, fmt.Errorf("combine %s: %w", files[i], err)
			}
			doc = prefixed
			report.Inputs[i].Remapped = mapping
		}
		combined, res, err := merge.Combine(out, doc, opts)
		if err != nil {
			return nil, report, fmt.Errorf("combine %s: %w", files[i], err)
		}
		out = combined
		if name != combinePrefix {
			report.Inputs[i].Remapped = res.RemappedXRefs
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
	if _, err := fmt.Fprintf(w, "wrote %s: %s, %s, %s\n", report.Output,
		plural(report.Records, "record", "records"), plural(report.Individuals, "individual", "individuals"),
		plural(report.Families, "family", "families")); err != nil {
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
// differed from the one kept. A "Tags." conflict is not counted: the library
// reports one for every header structure a later file repeats, alike or not.
func droppedHeaderValues(conflicts []combineHeaderConflict) int {
	n := 0
	for _, c := range conflicts {
		if !strings.HasPrefix(c.Field, "Tags.") {
			n++
		}
	}
	return n
}
