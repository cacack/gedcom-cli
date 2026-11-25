package cli

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"slices"

	"github.com/cacack/gedcom-go/v3/gedcom"
	"github.com/spf13/cobra"
)

// extractReport is what extract prints: the file written, what it holds, and
// the pointers cut at the boundary.
type extractReport struct {
	Output      string           `json:"output"`
	Records     int              `json:"records"`
	Individuals int              `json:"individuals"`
	Families    int              `json:"families"`
	Stamped     bool             `json:"stamped"`
	Dropped     []droppedPointer `json:"dropped"`
}

// droppedPointer is gedcom.DroppedPointer with lowercase JSON keys.
type droppedPointer struct {
	XRef       string `json:"xref"`
	Tag        string `json:"tag"`
	Target     string `json:"target"`
	Restorable bool   `json:"restorable"`
	Position   int    `json:"position"`
}

// pointer is d as the library reported it.
func (d droppedPointer) pointer() gedcom.DroppedPointer {
	return gedcom.DroppedPointer{XRef: d.XRef, Tag: d.Tag, Target: d.Target, Restorable: d.Restorable, Position: d.Position}
}

func (d droppedPointer) String() string {
	tag := d.Tag
	if tag == "" {
		tag = "pointer"
	}
	restorable := "not restorable"
	if d.Restorable {
		restorable = "restorable"
	}
	return fmt.Sprintf("%s %s %s (%s)", d.XRef, tag, d.Target, restorable)
}

// submitterValue replaces a submitter's name under --strip-submitter.
const submitterValue = "Submitter"

func newExtractCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "extract <file> <xref>... -o <out>",
		Short: "Write a branch of the tree to its own file: the people given, their families and what they cite",
		Long: `Write the individuals given, every family one of them is a member of, and the
sources, notes, media, repositories and submitters those records cite, as a
self-contained GEDCOM file. This is gedcom-go's bounded subset: other
individuals are not pulled in, however they are related.

An xref may be given without its @ delimiters. "-" reads more xrefs from
stdin, separated by spaces or newlines. Every word read is taken as an xref,
so feed it the xref column only, such as the JSON output of 'gedcom ancestors',
'gedcom descendants' or 'gedcom list ... indi' through jq -r '.[].xref'; their
text output carries names and flags too.

A pointer in an included record to an individual or family left out, such as
a family's CHIL for a child not extracted, is removed, so the output has no
dangling pointers. Each one is listed on stderr as a warning (except with
"-o -" and --format json, when stderr holds only the JSON report) and in the
report's "dropped" list, with whether combining the file with one cut from the
same master that holds the target is guaranteed to put it back (restorable),
and where the line sat among its record's level-1 lines (JSON "position",
from 1; 0 when not recorded).
A pointer in an included record that resolves to nothing stops the extract
instead, since dropping it would hide a defect: 'gedcom validate' finds them.

To put the extracts of one master back together, save the JSON report of each
and give 'gedcom combine --strategy zip' the reports of the files after the
first as --dropped: it uses the positions to put a split family's HUSB, WIFE
and CHIL lines back in the master's order. Without them the lines come back
in one file's order. Such a round trip reproduces only the records reachable
from the people extracted: a record nothing cites (a SUBM, REPO or SOUR with
no pointer to it) is in no extract, and so not in the combined file.

Records keep their xrefs, and every line that is kept is written as it was
read. The header is the input's, stamped, since the output is a new dataset
made by this tool; --keep-header writes it as read, for a cut to be zipped
back byte for byte. The header names the same submitter, so the output
carries the submitter's name, address, phone and email, unless
--strip-submitter replaces each submitter record with one named ` + submitterValue + `.
The rest of the header is copied unchanged, even with --strip-submitter:
check its note and copyright lines (with --keep-header, its source and
corporation lines too) before sharing the file.
--redact-living applies to the output file, not only to the report.

-o is required and may not name the input file; "-o -" writes the GEDCOM to
stdout and the report to stderr. An existing output file is replaced, and only
once the whole file is written.

` + stampHelp,
		Example: `  gedcom extract tree.ged @I1@ @I2@ -o couple.ged
  gedcom descendants --format json tree.ged I1 | jq -r '.[].xref' | gedcom extract tree.ged - -o branch.ged
  gedcom extract --redact-living --strip-submitter tree.ged I1 -o share.ged
  gedcom extract --format json tree.ged I3 I4 -o b.ged > b.json
  gedcom combine a.ged b.ged --strategy zip --dropped b.json -o tree2.ged`,
		Args: minArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			format, err := outputFormat(cmd)
			if err != nil {
				return err
			}
			out, err := outputPath(cmd, args[0])
			if err != nil {
				return err
			}
			strip, err := cmd.Flags().GetBool("strip-submitter")
			if err != nil {
				return err
			}
			living, err := readLivingOptions(cmd)
			if err != nil {
				return err
			}
			stamp, err := stampRequested(cmd)
			if err != nil {
				return err
			}
			xrefs, err := readXRefs(cmd.InOrStdin(), args[1:])
			if err != nil {
				return err
			}
			res, err := decodeForWrite(args[0])
			if err != nil {
				return err
			}
			doc := res.Document
			if living.redact {
				if err := redactDocument(hiddenRecords(doc, presumedLiving(doc, living.cfg))); err != nil {
					return err
				}
			}
			if strip {
				stripSubmitters(doc)
			}
			records, err := findRecords(doc, xrefs)
			if err != nil {
				return err
			}
			seeds := make([]string, len(records))
			for i, record := range records {
				seeds[i] = record.XRef
			}
			branch, dropped, err := doc.BoundedSubset(seeds)
			if err != nil {
				return err
			}
			// BoundedSubset does not carry the source layout over, as Clone
			// does. A Layout is safe to share: the encoder writes a source
			// line only where the record still reads as it.
			branch.Layout = doc.Layout
			if stamp {
				if err := stampHeader(branch); err != nil {
					return err
				}
			}
			if err := writeOutput(cmd, out, branch); err != nil {
				return err
			}
			report := newExtractReport(out, branch, dropped)
			report.Stamped = stamp
			// With -o - and JSON the report goes to stderr, and its dropped
			// list stands in for the warnings so stderr is one JSON document.
			if out != stdio || format != formatJSON {
				for _, d := range report.Dropped {
					fmt.Fprintf(cmd.ErrOrStderr(), "gedcom: warning: dropped pointer %s\n", d)
				}
			}
			w := reportWriter(cmd, out)
			if format == formatJSON {
				return writeJSON(w, report)
			}
			_, err = fmt.Fprintf(w, "wrote %s: %s, %s, %s, %s dropped%s\n", outputName(out),
				plural(report.Records, "record", "records"), plural(report.Individuals, "individual", "individuals"),
				plural(report.Families, "family", "families"), plural(len(report.Dropped), "pointer", "pointers"),
				stampedText(report.Stamped))
			return err
		},
	}
	addOutputFlag(cmd)
	cmd.Flags().Bool("strip-submitter", false, "replace each submitter record with one named "+submitterValue+", dropping its address, phone, email and notes (the header SUBM pointer is kept)")
	addKeepHeaderFlag(cmd)
	return cmd
}

// readXRefs is the xrefs given, with "-" replaced by the xrefs on stdin.
func readXRefs(stdin io.Reader, args []string) ([]string, error) {
	if i := slices.Index(args, stdio); i >= 0 && slices.Contains(args[i+1:], stdio) {
		return nil, usageError{errors.New(`"-" may be given only once`)}
	}
	var xrefs []string
	for _, arg := range args {
		if arg != stdio {
			xrefs = append(xrefs, arg)
			continue
		}
		scanner := bufio.NewScanner(stdin)
		scanner.Split(bufio.ScanWords)
		n := 0
		for scanner.Scan() {
			xrefs = append(xrefs, scanner.Text())
			n++
		}
		if err := scanner.Err(); err != nil {
			return nil, fmt.Errorf("read stdin: %w", err)
		}
		if n == 0 {
			return nil, errors.New("no xrefs on stdin")
		}
	}
	return xrefs, nil
}

// stripSubmitters replaces each submitter record, in place, with one that
// keeps only its xref and is named submitterValue. The record's tags are
// dropped, so the library writes it from the stripped entity.
func stripSubmitters(doc *gedcom.Document) {
	for _, record := range doc.Records {
		if e, ok := record.Entity.(*gedcom.Submitter); ok {
			*e = gedcom.Submitter{XRef: e.XRef, Name: submitterValue}
			record.Tags, record.Value = nil, ""
		}
	}
}

func newExtractReport(out string, branch *gedcom.Document, dropped []gedcom.DroppedPointer) extractReport {
	report := extractReport{
		Output:      outputName(out),
		Records:     len(branch.Records),
		Individuals: len(branch.Individuals()),
		Families:    len(branch.Families()),
		Dropped:     make([]droppedPointer, len(dropped)),
	}
	for i, d := range dropped {
		report.Dropped[i] = droppedPointer{XRef: d.XRef, Tag: d.Tag, Target: d.Target, Restorable: d.Restorable, Position: d.Position}
	}
	return report
}
