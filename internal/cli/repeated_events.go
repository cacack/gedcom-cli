package cli

import (
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/cacack/gedcom-go/v3/gedcom"
	"github.com/cacack/gedcom-go/v3/merge"
	"github.com/spf13/cobra"
)

// repeatedGroup is the substructures of one record that record the same
// event, each as its lines.
type repeatedGroup struct {
	XRef    string     `json:"xref"`
	Tag     string     `json:"tag"`
	Members [][]string `json:"members"`
}

// repeatedFold is one substructure folded into the first of its group.
type repeatedFold struct {
	XRef string `json:"xref"`
	mergedFold
}

// repeatedFoldReport is what --fold did.
type repeatedFoldReport struct {
	Folded []repeatedFold `json:"folded"`
}

func newRepeatedEventsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "repeated-events <file> [<xref>...] [--fold -o <out>]",
		Short: "Report events a record holds twice, and fold them into one, writing a new file",
		Long: `Report the substructures of one individual or family record that record the
same event: a birth written twice, say, as a merge made without --fold-events
or an import can leave it. Every individual and family is searched, or only
the records <xref> names.

The rule is the one merge-people --fold-events folds by:

` + sameEventHelp + `

The report lists one group per line, tab-separated:

  repeated  xref, tag, lines...
                          the substructures of one record that record the
                          same event, in file order, each as its lines

With --fold, each group is folded into its first member, as --fold-events
folds drop's event into keep's: the first member's lines stay, and each line
of the others it lacks (a citation, a note, a place) is added; the others are
removed. The result is written to --output; the input file is never changed,
and every record with nothing to fold is written exactly as read. Folding the
output again folds nothing. The report then lists, instead:

  folded    xref, tag, lines, into
                          a substructure removed, folded into the first of
                          its group; into is that one once every fold into
                          it is done

A substructure's lines are joined with " | ". With --redact-living, those of
a presumed-living person, and of a family with one as a spouse, read Living;
the file written is not redacted.

-o is required with --fold, and only then, and may not name file. An
existing output file is replaced only once the whole result is written. With
-o - the GEDCOM goes to stdout and the report to stderr.`,
		Example: `  gedcom repeated-events family.ged
  gedcom repeated-events family.ged @I1@ @F2@
  gedcom repeated-events --fold family.ged -o folded.ged
  gedcom repeated-events --format json family.ged | jq '.[] | select(.tag == "BIRT")'`,
		Args: minArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			format, err := outputFormat(cmd)
			if err != nil {
				return err
			}
			opts, err := readLivingOptions(cmd)
			if err != nil {
				return err
			}
			fold, err := cmd.Flags().GetBool("fold")
			if err != nil {
				return err
			}
			out := ""
			if fold {
				if out, err = outputPath(cmd, args[0]); err != nil {
					return err
				}
			} else if cmd.Flags().Changed("output") {
				return usageError{errors.New("--output (-o) writes the folded file: add --fold")}
			}

			res, err := decodeForWrite(args[0])
			if err != nil {
				return err
			}
			doc := res.Document
			xrefs, err := repeatedTargets(doc, args[1:])
			if err != nil {
				return err
			}
			hidden := map[string]bool{}
			if opts.redact {
				for record := range hiddenRecords(doc, presumedLiving(doc, opts.cfg)) {
					hidden[record.XRef] = true
				}
			}

			if !fold {
				groups := newRepeatedGroups(merge.RepeatedEvents(doc, xrefs...), hidden)
				if format == formatJSON {
					return writeJSON(cmd.OutOrStdout(), groups)
				}
				return writeRepeatedText(cmd.OutOrStdout(), groups)
			}

			folded, libReport, err := merge.FoldRepeatedEvents(doc, xrefs...)
			if err != nil {
				return err
			}
			if err := writeOutput(cmd, out, folded); err != nil {
				return err
			}
			report := &repeatedFoldReport{Folded: make([]repeatedFold, 0, len(libReport.Folded))}
			for _, f := range libReport.Folded {
				report.Folded = append(report.Folded, repeatedFold{XRef: f.XRef, mergedFold: mergedFold{
					Tag:   f.Drop.Tags[0].Tag,
					Lines: newMergedPart(f.Drop, hidden[f.XRef]).Lines,
					Into:  newMergedPart(f.Into, hidden[f.XRef]).Lines,
				}})
			}
			w := reportWriter(cmd, out)
			if format == formatJSON {
				return writeJSON(w, report)
			}
			for _, f := range report.Folded {
				if _, err := fmt.Fprintln(w, "folded\t"+f.XRef+"\t"+f.Tag+"\t"+strings.Join(f.Lines, " | ")+"\t"+strings.Join(f.Into, " | ")); err != nil {
					return err
				}
			}
			return nil
		},
	}
	addOutputFlag(cmd)
	cmd.Flags().Bool("fold", false, "fold each group into its first member, writing the result to --output")
	return cmd
}

// repeatedTargets is the xrefs of the records args names, each an individual
// or a family; none when args is empty, which searches every record.
func repeatedTargets(doc *gedcom.Document, args []string) ([]string, error) {
	records, err := findRecords(doc, args)
	if err != nil {
		return nil, err
	}
	xrefs := make([]string, 0, len(records))
	for _, record := range records {
		if record.Type != gedcom.RecordTypeIndividual && record.Type != gedcom.RecordTypeFamily {
			return nil, fmt.Errorf("%s is a %s record; want an individual or a family", record.XRef, record.Type)
		}
		xrefs = append(xrefs, record.XRef)
	}
	return xrefs, nil
}

// newRepeatedGroups is the library's groups as the report prints them, with
// the lines of the records hidden names redacted.
func newRepeatedGroups(groups []merge.EventGroup, hidden map[string]bool) []repeatedGroup {
	out := make([]repeatedGroup, 0, len(groups))
	for _, g := range groups {
		group := repeatedGroup{XRef: g.XRef, Tag: g.Members[0].Tags[0].Tag}
		for _, m := range g.Members {
			group.Members = append(group.Members, newMergedPart(m, hidden[g.XRef]).Lines)
		}
		out = append(out, group)
	}
	return out
}

// writeRepeatedText writes one line per group.
func writeRepeatedText(w io.Writer, groups []repeatedGroup) error {
	for _, g := range groups {
		fields := []string{"repeated", g.XRef, g.Tag}
		for _, m := range g.Members {
			fields = append(fields, strings.Join(m, " | "))
		}
		if _, err := fmt.Fprintln(w, strings.Join(fields, "\t")); err != nil {
			return err
		}
	}
	return nil
}
