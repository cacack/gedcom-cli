package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"
)

// pointer is one pointer to a deleted record, as the library reports it.
type pointer struct {
	// From is the record holding the pointer, or "" for the header.
	From string `json:"from"`
	// Path is the tag path to the pointer's line, such as FAM.CHIL.
	Path string `json:"path"`
	To   string `json:"to"`
}

// deleteReport is what delete removed and the pointers left to it: they
// stopped the write, or, with --force, dangle in the output.
type deleteReport struct {
	Input      string    `json:"input"`
	Output     string    `json:"output"`
	Written    bool      `json:"written"`
	Deleted    []string  `json:"deleted"`
	References []pointer `json:"references"`
}

func newDeleteCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "delete <file> <xref>... -o <out>",
		Short: "Remove records by xref, refusing while other records point at them",
		Long: `Write file to a new file without the records given by xref. An xref may be
given without its @ delimiters. Every other line is written exactly as it was
read.

Pointers to a deleted record left in the rest of the file (a family's CHIL, a
citation's SOUR, the header's SUBM) are listed, each as the record holding it,
the tag path to it and the record it points at; "-" stands for the header
(in --format json, a "from" of ""). Pointers held by records deleted in the
same command do not count. While any are left, nothing is written and the exit
code is 1, with the report printed. --force deletes anyway and writes the
pointers as they are, dangling: remove them with put.

A failure before the report exists (an unreadable file, an xref the file does
not define, or defines more than once) prints no report, only the error on
stderr.

-o is required and may not name the input file. An existing output file is
replaced only once the whole result is written. With -o - the GEDCOM goes to
stdout and the report to stderr.`,
		Example: `  gedcom delete family.ged @I9@ -o family-edited.ged
  gedcom delete family.ged I9 F4 -o family-edited.ged
  gedcom delete --force --format json family.ged @S2@ -o family-edited.ged | jq .references`,
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
			force, err := cmd.Flags().GetBool("force")
			if err != nil {
				return err
			}
			report, deleteErr := deleteRecords(cmd, args[0], args[1:], out, force)
			if report == nil {
				return deleteErr
			}
			w := reportWriter(cmd, out)
			if format == formatJSON {
				err = writeJSON(w, report)
			} else {
				err = writeDeleteText(w, report)
			}
			if err != nil {
				return err
			}
			return deleteErr
		},
	}
	addOutputFlag(cmd)
	cmd.Flags().Bool("force", false, "delete even while other records point at the records, leaving those pointers dangling")
	return cmd
}

// deleteRecords removes the records named by xrefs from the file at in and
// writes the result to out. It returns the report whenever every xref was
// found once, with an error when pointers left to the records stopped the
// write.
func deleteRecords(cmd *cobra.Command, in string, xrefs []string, out string, force bool) (*deleteReport, error) {
	res, err := decodeForWrite(in)
	if err != nil {
		return nil, err
	}
	doc := res.Document
	records, err := findRecords(doc, xrefs)
	if err != nil {
		return nil, err
	}

	report := &deleteReport{Input: in, Output: out, Deleted: []string{}, References: []pointer{}}
	deleted := make(map[string]bool, len(records))
	for _, record := range records {
		if deleted[record.XRef] {
			continue
		}
		deleted[record.XRef] = true
		report.Deleted = append(report.Deleted, record.XRef)
	}
	// Each removal reports the pointers left at that moment, so a pointer
	// held by a record removed later is reported too, and dropped below.
	for _, xref := range report.Deleted {
		refs, err := doc.RemoveRecord(xref)
		if err != nil {
			return nil, err
		}
		// The library indexes another record with the same xref in its
		// place, which this command could neither report nor tell apart.
		if doc.GetRecord(xref) != nil {
			return nil, fmt.Errorf("%s defines %s more than once; %s not written", inputName(in), xref, outputName(out))
		}
		for _, ref := range refs {
			if ref.XRef != "" && deleted[ref.XRef] {
				continue
			}
			report.References = append(report.References, pointer{From: ref.XRef, Path: strings.Join(ref.Path, "."), To: xref})
		}
	}

	if n := len(report.References); n > 0 && !force {
		return report, fmt.Errorf("%s left to the deleted records; %s not written (--force deletes anyway)",
			plural(n, "pointer", "pointers"), outputName(out))
	}
	if err := writeOutput(cmd, out, doc); err != nil {
		return nil, err
	}
	report.Written = true
	return report, nil
}

// writeDeleteText writes a summary line, then the pointers left to the
// deleted records, one per line: holder, tag path and target, tab-separated.
func writeDeleteText(w io.Writer, r *deleteReport) error {
	var b strings.Builder
	deleted := oneLine(strings.Join(r.Deleted, " "))
	heading := "dangling"
	if r.Written {
		fmt.Fprintf(&b, "wrote %s: deleted %s\n", outputName(r.Output), deleted)
	} else {
		fmt.Fprintf(&b, "%s not written: pointers left to %s\n", outputName(r.Output), deleted)
		heading = "references"
	}
	if len(r.References) > 0 {
		fmt.Fprintf(&b, "%s (%d)\n", heading, len(r.References))
	}
	for _, p := range r.References {
		from := p.From
		if from == "" {
			from = "-"
		}
		fmt.Fprintf(&b, "  %s\n", strings.Join([]string{oneLine(from), oneLine(p.Path), oneLine(p.To)}, "\t"))
	}
	_, err := io.WriteString(w, b.String())
	return err
}
