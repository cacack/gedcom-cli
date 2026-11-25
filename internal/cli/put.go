package cli

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/cacack/gedcom-go/v3/decoder"
	"github.com/cacack/gedcom-go/v3/encoder"
	"github.com/cacack/gedcom-go/v3/gedcom"
	"github.com/spf13/cobra"
)

// putReport is what put did: the records it replaced, added and left as they
// were, and what the decoder could not read cleanly in the fragment.
type putReport struct {
	Input   string `json:"input"`
	Records string `json:"records"`
	Output  string `json:"output"`
	Written bool   `json:"written"`
	Stamped bool   `json:"stamped"`
	// StampedFrom is the value of the SOUR the stamp replaced.
	StampedFrom string         `json:"stamped_from,omitempty"`
	Replaced    []string       `json:"replaced"`
	Added       []string       `json:"added"`
	Unchanged   []string       `json:"unchanged"`
	Counts      severityCounts `json:"counts"`
	Findings    []finding      `json:"findings"`
}

func newPutCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "put <file> <records.ged> -o <out>",
		Short: "Replace records by xref, or add new ones, from a file of GEDCOM records",
		Long: `Write file to a new file with the records of records.ged put in: each one
replaces the record with the same xref, in its place, or is appended when the
xref is new. records.ged holds level-0 records only, with no HEAD or TRLR, as
'gedcom get' prints them; "-" reads them from stdin. 'gedcom put-header'
replaces the header.

Every other line is written exactly as it was read. A record whose lines are
unchanged is left as it was, so putting back an unedited copy writes the file
byte-identical. A replaced record is written as its new lines, without the old
record's indentation or blank lines.

What the decoder could not read cleanly in records.ged is reported as
'gedcom validate' reports it, with line numbers counted in records.ged. When
any finding is an error, nothing is written and the exit code is 1, with the
report printed. Pointers are not checked: a put record may point at an xref
the file does not define, or replace a record with one of another type. Run
'gedcom validate' on the output to find them.

A failure before the report exists (an unreadable file, a record in
records.ged without an xref) prints no report, only the error on stderr.

The header is written as read, since the file is edited in place of its
author. --stamp stamps it instead: only the header's SOUR structure then
differs. With an empty records.ged (/dev/null), put --stamp
stamps a file and changes nothing else.

-o is required and may not name file or records.ged. An existing output file is
replaced only once the whole result is written. With -o - the GEDCOM goes to
stdout and the report to stderr.

` + stampHelp,
		Example: `  # The edit loop: print a record, change its lines, put it back.
  gedcom get family.ged @I1@ > i1.ged
  $EDITOR i1.ged
  gedcom put family.ged i1.ged -o family-edited.ged
  git diff --no-index family.ged family-edited.ged

  gedcom get family.ged @I1@ | sed 's/Exmaple/Example/' | gedcom put family.ged - -o fixed.ged
  gedcom put --format json family.ged new-people.ged -o family-edited.ged | jq .added
  gedcom put --stamp family.ged /dev/null -o stamped.ged`,
		Args: exactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			format, err := outputFormat(cmd)
			if err != nil {
				return err
			}
			out, err := outputPath(cmd, args[0], args[1])
			if err != nil {
				return err
			}
			report, putErr := putRecords(cmd, args[0], args[1], out)
			if report == nil {
				return putErr
			}
			w := reportWriter(cmd, out)
			if format == formatJSON {
				err = writeJSON(w, report)
			} else {
				err = writePutText(w, report)
			}
			if err != nil {
				return err
			}
			return putErr
		},
	}
	addOutputFlag(cmd)
	addStampFlag(cmd)
	return cmd
}

// putRecords puts the records of the fragment at records into the file at in
// and writes the result to out. It returns the report whenever the fragment
// was read, with an error when its findings or its records stopped the write.
func putRecords(cmd *cobra.Command, in, records, out string) (*putReport, error) {
	res, err := decodeForWrite(in)
	if err != nil {
		return nil, err
	}
	doc := res.Document
	fragment, diags, err := decodeFragment(cmd, records, doc)
	if err != nil {
		return nil, err
	}

	findings := decodeFindings(diags)
	report := &putReport{
		Input:     in,
		Records:   records,
		Output:    out,
		Replaced:  []string{},
		Added:     []string{},
		Unchanged: []string{},
		Counts:    countSeverities(findings),
		Findings:  findings,
	}
	if n := report.Counts.Error; n > 0 {
		return report, fmt.Errorf("%s in %s; %s not written", plural(n, "error", "errors"), inputName(records), outputName(out))
	}

	// The decoder reports an xref the fragment defines twice as an error, so
	// each record here names a different one.
	for i, record := range fragment {
		if record.XRef == "" {
			return nil, fmt.Errorf("%s: record %d (%s) has no xref", inputName(records), i+1, record.Type)
		}

		old := doc.GetRecord(record.XRef)
		switch {
		case old == nil:
			err = doc.AddRecord(record)
			report.Added = append(report.Added, record.XRef)
		case sameLines(doc, old, record):
			report.Unchanged = append(report.Unchanged, record.XRef)
		default:
			err = doc.ReplaceRecord(record)
			report.Replaced = append(report.Replaced, record.XRef)
		}
		if err != nil {
			return nil, err
		}
	}

	stamp, err := stampRequested(cmd)
	if err != nil {
		return nil, err
	}
	if stamp {
		if report.StampedFrom, err = stampHeader(doc); err != nil {
			return nil, err
		}
	}
	if err := writeOutput(cmd, out, doc); err != nil {
		return nil, err
	}
	report.Written, report.Stamped = true, stamp
	return report, nil
}

// decodeFragment reads the records-only fragment at path, or stdin for "-",
// as records of doc.
func decodeFragment(cmd *cobra.Command, path string, doc *gedcom.Document) ([]*gedcom.Record, decoder.Diagnostics, error) {
	var r io.Reader = cmd.InOrStdin()
	if path != stdio {
		f, err := os.Open(filepath.Clean(path))
		if err != nil {
			return nil, nil, err
		}
		defer f.Close()
		r = f
	}
	records, diags, err := decoder.DecodeRecords(r, doc, decoder.DefaultOptions())
	if err != nil {
		return nil, nil, fmt.Errorf("%s: %w", inputName(path), err)
	}
	return records, diags, nil
}

// sameLines reports whether a and b encode to the same lines within doc, so
// that replacing a with b would change nothing but the layout.
func sameLines(doc *gedcom.Document, a, b *gedcom.Record) bool {
	var aLines, bLines bytes.Buffer
	if encoder.EncodeRecord(&aLines, doc, a, nil) != nil || encoder.EncodeRecord(&bLines, doc, b, nil) != nil {
		return false
	}
	return bytes.Equal(aLines.Bytes(), bLines.Bytes())
}

// writePutText writes a summary line, one line per record put, then the
// fragment's findings as validate groups them.
func writePutText(w io.Writer, r *putReport) error {
	var b strings.Builder
	if r.Written {
		fmt.Fprintf(&b, "wrote %s: %d replaced, %d added, %d unchanged%s\n",
			outputName(r.Output), len(r.Replaced), len(r.Added), len(r.Unchanged), stampedText(r.Stamped, r.StampedFrom))
	} else {
		fmt.Fprintf(&b, "%s not written\n", outputName(r.Output))
	}
	for _, group := range []struct {
		name  string
		xrefs []string
	}{{"replaced", r.Replaced}, {"added", r.Added}, {"unchanged", r.Unchanged}} {
		for _, xref := range group.xrefs {
			fmt.Fprintf(&b, "  %s\t%s\n", group.name, oneLine(xref))
		}
	}
	if _, err := io.WriteString(w, b.String()); err != nil {
		return err
	}
	return writeFindingGroups(w, r.Counts, r.Findings)
}
