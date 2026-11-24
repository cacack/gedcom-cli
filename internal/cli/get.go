package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/cacack/gedcom-go/v3/encoder"
	"github.com/cacack/gedcom-go/v3/gedcom"
	"github.com/spf13/cobra"
)

func newGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "get <file> <xref>...",
		Short: "Print records by xref: a person, family, source or any other record",
		Long: `Print records by xref, in the order given, as the GEDCOM lines of each record.

An xref may be given without its @ delimiters. Xrefs are case-sensitive. When
a file defines an xref twice, the first definition is printed.

--format json prints an array holding each record in gedcom-go's own shape.
Its keys are the library's field names (XRef, Names, Husband), not the
lowercase keys 'gedcom list' and 'gedcom validate' print, and they may change
with the library. Each record also has a Type key, its level-0 tag (INDI,
FAM, SUBM, _CUSTOM), which says what the other keys describe.`,
		Example: `  gedcom get family.ged @I1@
  gedcom get family.ged I1 F1
  gedcom get --format json family.ged @I1@`,
		Args: minArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			format, err := outputFormat(cmd)
			if err != nil {
				return err
			}
			doc, _, err := openDocument(cmd, args[0])
			if err != nil {
				return err
			}
			records, err := findRecords(doc, args[1:])
			if err != nil {
				return err
			}
			if format == formatJSON {
				return writeRecordsJSON(cmd.OutOrStdout(), records)
			}
			return writeRecordsText(cmd.OutOrStdout(), doc, records)
		},
	}
}

// findRecords resolves every xref, in order, before anything is written. An
// xref may be given without its @ delimiters.
func findRecords(doc *gedcom.Document, xrefs []string) ([]*gedcom.Record, error) {
	records := make([]*gedcom.Record, 0, len(xrefs))
	var missing []string
	for _, xref := range xrefs {
		// The argument as typed wins, so an xref the file itself malformed
		// stays reachable; otherwise missing delimiters are added.
		delimited := "@" + strings.Trim(xref, "@") + "@"
		record := doc.GetRecord(xref)
		if record == nil {
			record = doc.GetRecord(delimited)
		}
		if record == nil {
			missing = append(missing, delimited)
			continue
		}
		records = append(records, record)
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("no such record: %s", strings.Join(missing, ", "))
	}
	return records, nil
}

// writeRecordsJSON always writes an array: each record's typed entity with
// the record's Type added, or, when it has none, the record itself, which
// has a Type of its own.
func writeRecordsJSON(w io.Writer, records []*gedcom.Record) error {
	out := make([]any, len(records))
	for i, record := range records {
		if record.Entity == nil {
			out[i] = record
			continue
		}
		entity, err := withType(record.Type, record.Entity)
		if err != nil {
			return err
		}
		out[i] = entity
	}
	return writeJSON(w, out)
}

// withType is entity's JSON object with a Type key, the record type, put
// first. No typed entity has a Type field of its own, so the key is not
// duplicated, and the entity's own keys keep their order.
func withType(recordType gedcom.RecordType, entity any) (json.RawMessage, error) {
	object, err := json.Marshal(entity)
	if err != nil {
		return nil, err
	}
	if len(object) < 2 || object[0] != '{' {
		return nil, fmt.Errorf("%s entity is not a JSON object: %s", recordType, object)
	}
	typeJSON, err := json.Marshal(recordType)
	if err != nil {
		return nil, err
	}
	out := append([]byte(`{"Type":`), typeJSON...)
	if string(object) != "{}" {
		out = append(out, ',')
	}
	return append(out, object[1:]...), nil
}

// writeRecordsText writes each record's lines as Encode would write them
// within doc, with no header or trailer.
func writeRecordsText(w io.Writer, doc *gedcom.Document, records []*gedcom.Record) error {
	for _, record := range records {
		if err := encoder.EncodeRecord(w, doc, record, nil); err != nil {
			return err
		}
	}
	return nil
}
