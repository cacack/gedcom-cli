package cli

import (
	"fmt"
	"io"

	"github.com/cacack/gedcom-go/v3/gedcom"
	"github.com/spf13/cobra"
)

// Relationships, as relatives prints them.
const (
	relSelf        = "self"
	relParent      = "parent"
	relSibling     = "sibling"
	relHalfSibling = "half-sibling"
	relSpouse      = "spouse"
	relChild       = "child"
)

// kinRow is one relative in relatives: a list indi row plus how they are
// related.
type kinRow struct {
	indiRow
	Relationship string `json:"relationship"`
	Family       string `json:"family"`
	Pedigree     string `json:"pedigree"`
}

// relativesReport is the JSON form of relatives.
type relativesReport struct {
	Person    indiRow  `json:"person"`
	Relatives []kinRow `json:"relatives"`
}

func (r kinRow) fields() []string {
	return []string{r.Relationship, r.Family, r.XRef, r.Name, r.Birth, r.Death, r.Pedigree}
}

func newRelativesCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "relatives <file> <xref>...",
		Short: "Print people with their parents, siblings, spouses and children",
		Long: `Print a person and their immediate family, one person per line: first the
person, then their parents, siblings, spouses and children, in file order
(a family's husband before its wife).

Text output is tab-separated with no header. The fields are:

  relationship, family, xref, name, birth, death, pedigree

relationship is self, parent, sibling, half-sibling, spouse or child. family
is the family the relative comes through: for a parent or sibling, a family
the person is a child of; for a half-sibling, the parent's other family; for a
spouse or child, a family the person is a husband or wife in. The person's
own line has no family. A person in several families (two marriages, birth
and adoptive parents) has a parent, spouse or child line in each, so a
relative can appear twice; a sibling is listed once, under the first family
that links them.

pedigree is the PEDI of the child's link to that family (birth, adopted,
foster, ...) as written, or empty when the file gives none: the person's own
link for a parent, the relative's for a sibling or child. Spouses have none.

--format json prints one object: "person", with the keys of 'gedcom list'
indi (xref, name, birth, death), and "relatives", an array of objects with
those keys plus relationship, family and pedigree. The person is not repeated
in "relatives".

Several people may be given in one call, which reads the file once. An xref
may be given without its @ delimiters. "-" reads more xrefs from stdin,
separated by spaces or newlines, as for 'gedcom extract'. With more than one
xref, or with "-", each person is reported once, in the order first given:
text output prints each person's lines in turn, each starting with its self
line, and --format json prints an array of the objects above, even when stdin
held a single xref. A script whose list may hold one xref should send it on
stdin to get an array every time.

` + linkRule,
		Example: `  gedcom relatives family.ged @I1@
  gedcom relatives family.ged I1 | awk -F'\t' '$1 == "child"'
  gedcom relatives --format json family.ged I1 | jq '.relatives[] | select(.relationship == "parent")'
  gedcom relatives --format json family.ged I1 I2 I3
  printf '%s\n' $XREFS | gedcom relatives --format json family.ged -
  gedcom duplicates --format json old-export.ged family.ged | jq -r '.[].b.xref' | gedcom relatives --format json family.ged -`,
		Args: minArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			format, err := outputFormat(cmd)
			if err != nil {
				return err
			}
			// One xref typed as it is keeps the single-object output; any
			// other call may name several people, so prints an array.
			single := len(args) == 2 && args[1] != stdio
			xrefs, err := readXRefs(cmd.InOrStdin(), args[1:])
			if err != nil {
				return err
			}
			doc, living, err := openDocument(cmd, args[0])
			if err != nil {
				return err
			}
			records, err := findRecords(doc, xrefs)
			if err != nil {
				return err
			}
			reports := make([]relativesReport, 0, len(records))
			seen := make(map[*gedcom.Individual]bool, len(records))
			for _, record := range records {
				person, ok := record.GetIndividual()
				if !ok {
					return fmt.Errorf("%s is a %s record, not an individual", record.XRef, record.Type)
				}
				if seen[person] {
					continue
				}
				seen[person] = true
				reports = append(reports, relativesReport{
					Person:    indiRowOf(person, living[person]),
					Relatives: relatives(doc, living, person),
				})
			}
			if format == formatJSON {
				if single {
					return writeJSON(cmd.OutOrStdout(), reports[0])
				}
				return writeJSON(cmd.OutOrStdout(), reports)
			}
			for _, report := range reports {
				if err := writeRelativesText(cmd.OutOrStdout(), report); err != nil {
					return err
				}
			}
			return nil
		},
	}
}

// relatives lists person's parents, siblings, spouses and children, in that
// order. The result is never nil, so that no relatives is [] in JSON.
func relatives(doc *gedcom.Document, living map[*gedcom.Individual]bool, person *gedcom.Individual) []kinRow {
	rows := []kinRow{}
	add := func(i *gedcom.Individual, relationship string, fam *gedcom.Family, pedigree string) {
		rows = append(rows, kinRow{
			indiRow:      indiRowOf(i, living[i]),
			Relationship: relationship,
			Family:       fam.XRef,
			Pedigree:     pedigree,
		})
	}
	for _, link := range person.ParentLinks(doc) {
		add(link.Individual, relParent, link.Family, link.Pedigree)
	}
	for _, sib := range person.Siblings(doc) {
		relationship := relSibling
		if sib.Half {
			relationship = relHalfSibling
		}
		add(sib.Individual, relationship, sib.Family, pedigreeIn(sib.Individual, sib.Family))
	}
	families := person.FamiliesAsSpouse(doc)
	for _, fam := range families {
		if spouse := otherSpouse(doc, fam, person); spouse != nil {
			add(spouse, relSpouse, fam, "")
		}
	}
	for _, fam := range families {
		for _, child := range fam.ChildrenIndividuals(doc) {
			add(child, relChild, fam, pedigreeIn(child, fam))
		}
	}
	return rows
}

// otherSpouse is the spouse in fam who is not person, or nil when the family
// names no one else.
func otherSpouse(doc *gedcom.Document, fam *gedcom.Family, person *gedcom.Individual) *gedcom.Individual {
	spouse := fam.HusbandIndividual(doc)
	if fam.Husband == person.XRef {
		spouse = fam.WifeIndividual(doc)
	}
	if spouse == nil || spouse.XRef == person.XRef {
		return nil
	}
	return spouse
}

// pedigreeIn is the pedigree on child's FAMC link to fam, or "" when the link
// gives none.
func pedigreeIn(child *gedcom.Individual, fam *gedcom.Family) string {
	for _, link := range child.ChildInFamilies {
		if link.FamilyXRef == fam.XRef {
			return link.Pedigree
		}
	}
	return ""
}

func writeRelativesText(w io.Writer, report relativesReport) error {
	rows := make([]row, 0, len(report.Relatives)+1)
	rows = append(rows, kinRow{indiRow: report.Person, Relationship: relSelf})
	for _, r := range report.Relatives {
		rows = append(rows, r)
	}
	return writeRowsText(w, rows)
}
