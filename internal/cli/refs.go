package cli

import (
	"fmt"
	"io"
	"maps"
	"slices"
	"strings"

	"github.com/cacack/gedcom-go/v3/gedcom"
	"github.com/spf13/cobra"
)

func newRefsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "refs <file> <xref|->...",
		Short: "List the pointers to records by xref: who uses this source, media record or person",
		Long: `List every pointer to the records given by xref, one per line: the record
holding it, the tag path to it and the record it points at, tab-separated, as
'gedcom delete' reports the pointers that stop it. "-" stands for the header
(in --format json, a "from" of ""). Pointers are found at any depth
(INDI.BIRT.SOUR.OBJE) and in the header; a pointer a record holds to itself is
not listed. Nothing is changed.

An xref may be given without its @ delimiters. "-" reads more xrefs from
stdin, separated by spaces or newlines, as for 'gedcom get'. However many
xrefs are given, the file is read once. The pointers are listed by xref in
the order given, each xref once, then in file order.

A record nothing points at lists nothing, with exit code 0. An xref the file
does not define, or defines more than once, is an error, and nothing is
listed.

--format json prints an array of objects with the keys 'gedcom delete' uses:
from, path and to. --redact-living does not change the output: it is xrefs
and tag paths only.`,
		Example: `  gedcom refs family.ged @O1@
  gedcom refs family.ged S1 R1
  gedcom list --format json family.ged obje | jq -r '.[].xref' | gedcom refs family.ged -
  gedcom refs --format json family.ged @S1@ | jq -r '.[].from'`,
		Args: minArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			format, err := outputFormat(cmd)
			if err != nil {
				return err
			}
			xrefs, err := readXRefs(cmd.InOrStdin(), args[1:])
			if err != nil {
				return err
			}
			// Decoded unredacted, like delete: the output is xrefs and tag
			// paths, which redaction would only cut short.
			doc, err := decodeFile(args[0])
			if err != nil {
				return err
			}
			pointers, err := findPointers(doc, args[0], xrefs)
			if err != nil {
				return err
			}
			if format == formatJSON {
				return writeJSON(cmd.OutOrStdout(), pointers)
			}
			return writeRefsText(cmd.OutOrStdout(), pointers)
		},
	}
}

// findPointers is every pointer to the records named by xrefs in doc, read
// from the file at in, by record in the order given, each once.
func findPointers(doc *gedcom.Document, in string, xrefs []string) ([]pointer, error) {
	records, err := findRecords(doc, xrefs)
	if err != nil {
		return nil, err
	}
	pointers := []pointer{}
	seen := make(map[string]bool, len(records))
	for _, record := range records {
		xref := record.XRef
		if seen[xref] {
			continue
		}
		seen[xref] = true
		// RemoveRecord reports the pointers left to a record. It is called
		// on a copy with its own record list and index, so doc keeps every
		// record for the next xref.
		c := *doc
		c.Records = slices.Clone(doc.Records)
		c.XRefMap = maps.Clone(doc.XRefMap)
		refs, err := c.RemoveRecord(xref)
		if err != nil {
			return nil, err
		}
		// The library indexes another record with the same xref in its
		// place, whose pointers would be missed.
		if c.GetRecord(xref) != nil {
			return nil, fmt.Errorf("%s defines %s more than once", inputName(in), xref)
		}
		for _, ref := range refs {
			pointers = append(pointers, pointer{From: ref.XRef, Path: strings.Join(ref.Path, "."), To: xref})
		}
	}
	return pointers, nil
}

// writeRefsText writes one pointer per line.
func writeRefsText(w io.Writer, pointers []pointer) error {
	var b strings.Builder
	for _, p := range pointers {
		fmt.Fprintln(&b, p.line())
	}
	_, err := io.WriteString(w, b.String())
	return err
}
