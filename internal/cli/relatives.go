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
		Use:   "relatives <file> <xref>",
		Short: "Print a person with their parents, siblings, spouses and children",
		Long: `Print a person and their immediate family, one person per line: first the
person, then their parents, siblings, spouses and children, in file order
(a family's husband before its wife).

Text output is tab-separated with no header. The fields are:

  relationship, family, xref, name, birth, death, pedigree

relationship is self, parent, sibling, half-sibling, spouse or child. family
is the family the relative comes through: for a parent or sibling, a family
the person is a child of; for a half-sibling, the parent's other family; for a
spouse or child, a family that names the person as husband or wife (a FAMS
link the family does not return is ignored). The person's own line has no
family. A person in several families (two marriages, birth and adoptive
parents) has a parent, spouse or child line in each, so a relative can appear
twice; a sibling is listed once, under the first family that links them.

pedigree is the PEDI of the child's link to that family (birth, adopted,
foster, ...) as written, or empty when the file gives none: the person's own
link for a parent, the relative's for a sibling or child. Spouses have none.

--format json prints one object: "person", with the keys of 'gedcom list'
indi (xref, name, birth, death), and "relatives", an array of objects with
those keys plus relationship, family and pedigree. The person is not repeated
in "relatives".

An xref may be given without its @ delimiters.`,
		Example: `  gedcom relatives family.ged @I1@
  gedcom relatives family.ged I1 | awk -F'\t' '$1 == "child"'
  gedcom relatives --format json family.ged I1 | jq '.relatives[] | select(.relationship == "parent")'`,
		Args: exactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			format, err := outputFormat(cmd)
			if err != nil {
				return err
			}
			doc, err := decodeFile(args[0])
			if err != nil {
				return err
			}
			records, err := findRecords(doc, args[1:])
			if err != nil {
				return err
			}
			person, ok := records[0].GetIndividual()
			if !ok {
				return fmt.Errorf("%s is a %s record, not an individual", records[0].XRef, records[0].Type)
			}
			report := relativesReport{
				Person:    newIndiRow(person).(indiRow),
				Relatives: relatives(doc, person),
			}
			if format == formatJSON {
				return writeJSON(cmd.OutOrStdout(), report)
			}
			return writeRelativesText(cmd.OutOrStdout(), report)
		},
	}
}

// relatives lists person's parents, siblings, spouses and children, in that
// order. The result is never nil, so that no relatives is [] in JSON.
func relatives(doc *gedcom.Document, person *gedcom.Individual) []kinRow {
	rows := []kinRow{}
	add := func(i *gedcom.Individual, relationship string, fam *gedcom.Family, pedigree string) {
		rows = append(rows, kinRow{
			indiRow:      newIndiRow(i).(indiRow),
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
	families := spouseFamilies(doc, person)
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

// spouseFamilies is person's FAMS families that name person as husband or
// wife. A one-sided FAMS link would otherwise report another couple's members
// as person's spouse and children.
func spouseFamilies(doc *gedcom.Document, person *gedcom.Individual) []*gedcom.Family {
	var families []*gedcom.Family
	for _, fam := range person.FamiliesAsSpouse(doc) {
		if fam.Husband == person.XRef || fam.Wife == person.XRef {
			families = append(families, fam)
		}
	}
	return families
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
