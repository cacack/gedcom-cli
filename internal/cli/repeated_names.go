package cli

import (
	"github.com/cacack/gedcom-go/v3/gedcom"
	"github.com/cacack/gedcom-go/v3/merge"
	"github.com/spf13/cobra"
)

// samePersonalNameHelp is the rule for two NAME values that are the same
// name, gedcom-go's SamePersonalName: list names' same_as uses it, and
// repeated-names starts from it.
const samePersonalNameHelp = `Two NAME values are equal, as gedcom-go decides it, when they hold the same
words in the same order, case ignored: a word is a run of letters, digits
and combining marks, and every other character (spaces, punctuation, the
surname slashes) only separates words. So "John /Smith/ Jr." and
"john  smith, jr" are equal, while "O'Brien" and "OBrien" are not, nor are
"John Smith" and "Smith John". A value with no words equals none.`

func newRepeatedNamesCmd() *cobra.Command {
	return newRepeatedCmd(repeatedCommand{
		use:   "repeated-names <file> [<xref>...] [--fold -o <out>]",
		short: "Report names a person holds twice, and fold them into one, writing a new file",
		long: `Report the level-1 NAME structures of one individual that are the same name:
a name written twice, often with different citations under each copy, as a
merge or an import can leave it. Every individual is searched, or only those
<xref> names.

` + samePersonalNameHelp + `

Two NAMEs are the same name, as gedcom-go decides it, when:

  - their values are equal
  - both have a TYPE, written identically line for line, or neither has
  - each of NPFX, GIVN, NICK, SPFX, SURN and NSFX occurs at most once under
    each, and one both have is written identically line for line; one only
    one of them has does not keep them apart

Citations, notes and the other lines under a NAME are not compared. 'gedcom
list --repeated <file> names' lists the names whose values alone are equal;
'gedcom validate --style NAME_PART_MISMATCH <file>' reports a GIVN or SURN
that disagrees with its NAME value.

` + repeatedReportHelp("the NAMEs of one individual that are the same\nname", "a citation, a note, a name piece") + `

A NAME's lines are joined with " | ". With --redact-living, those of a
presumed-living person read Living; the file written is not redacted.

` + repeatedOutputHelp,
		example: `  gedcom repeated-names family.ged
  gedcom repeated-names family.ged @I1@ @I2@
  gedcom repeated-names --fold family.ged -o folded.ged
  gedcom repeated-names --format json family.ged | jq -r '.[].xref'`,
		types: []gedcom.RecordType{gedcom.RecordTypeIndividual},
		want:  "an individual",
		find: func(doc *gedcom.Document, xrefs ...string) []repeatedFound {
			var found []repeatedFound
			for _, g := range merge.RepeatedNames(doc, xrefs...) {
				found = append(found, repeatedFound{XRef: g.XRef, Members: g.Members})
			}
			return found
		},
		fold: merge.FoldRepeatedNames,
	})
}
