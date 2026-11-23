package cli

import (
	"fmt"
	"strconv"

	"github.com/cacack/gedcom-go/v3/gedcom"
	"github.com/spf13/cobra"
)

// relativeRow is one person reached by ancestors or descendants: list indi's
// row with the generation it was reached in first.
type relativeRow struct {
	Generation int `json:"generation"`
	indiRow
}

func (r relativeRow) fields() []string {
	return append([]string{strconv.Itoa(r.Generation)}, r.indiRow.fields()...)
}

// walkFunc is a generational walk from gedcom-go, such as
// Document.AncestorsWithGenerations.
type walkFunc func(doc *gedcom.Document, xref string, maxGenerations int) []gedcom.Relative

func newAncestorsCmd() *cobra.Command {
	return newWalkCmd("ancestors", "parents", "grandparents",
		"the husband before the wife, and several parent families (birth, adoptive,\nstep) in file order.",
		(*gedcom.Document).AncestorsWithGenerations)
}

// newWalkCmd builds ancestors or descendants: the two differ only in the
// words of their help and the library walk they call.
func newWalkCmd(name, first, second, order string, walk walkFunc) *cobra.Command {
	cmd := &cobra.Command{
		Use:   name + " <file> <xref>",
		Short: fmt.Sprintf("List a person's %s by generation: %s, %s and on", name, first, second),
		Long: fmt.Sprintf(`List a person's %[1]s, one per line, by generation: 1 is %[2]s,
2 is %[3]s, and so on. The person given is not listed. The xref may be
given without its @ delimiters.

Text output is tab-separated with no header. The fields, which are also the
JSON keys, are generation, xref, name, birth and death: the fields of
'gedcom list indi' with the generation first.

Rows are ordered by generation, then in gedcom-go's order within it:
%[4]s

A person reached by more than one path (pedigree collapse, such as a cousin
marriage) is listed once, at the generation of the shortest path.`, name, first, second, order),
		Example: fmt.Sprintf(`  gedcom %[1]s family.ged @I1@
  gedcom %[1]s --generations 3 family.ged I1
  gedcom %[1]s --format json family.ged @I1@ | jq '.[] | select(.generation == 2)'`, name),
		Args: exactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			format, err := outputFormat(cmd)
			if err != nil {
				return err
			}
			generations, err := cmd.Flags().GetInt("generations")
			if err != nil {
				return err
			}
			if generations < 0 {
				return usageError{fmt.Errorf("invalid --generations %d: want 0 (all) or more", generations)}
			}
			doc, err := decodeFile(args[0])
			if err != nil {
				return err
			}
			found, err := findRecords(doc, args[1:])
			if err != nil {
				return err
			}
			xref := found[0].XRef
			if doc.GetIndividual(xref) == nil {
				return fmt.Errorf("%s is not an individual", xref)
			}
			rows := relativeRows(doc, walk(doc, xref, generations))
			if format == formatJSON {
				return writeJSON(cmd.OutOrStdout(), rows)
			}
			return writeRowsText(cmd.OutOrStdout(), rows)
		},
	}
	cmd.Flags().Int("generations", 0, "list generations 1 to N only; 0 means all")
	return cmd
}

// relativeRows maps a walk to rows in the walk's order. The result is never
// nil, so that no relatives is [] in JSON.
func relativeRows(doc *gedcom.Document, relatives []gedcom.Relative) []row {
	rows := make([]row, 0, len(relatives))
	for _, r := range relatives {
		rows = append(rows, relativeRow{Generation: r.Generation, indiRow: indiRowOf(doc.GetIndividual(r.XRef))})
	}
	return rows
}
