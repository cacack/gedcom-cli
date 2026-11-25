package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/cacack/gedcom-go/v3/encoder"
	"github.com/cacack/gedcom-go/v3/gedcom"
	"github.com/spf13/cobra"
)

// headerArg is the argument that names the header in 'gedcom get'.
const headerArg = "HEAD"

func newGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "get <file> <xref|HEAD>...",
		Short: "Print records by xref: a person, family, source or any other record, or the header",
		Long: `Print records by xref, in the order given, as the GEDCOM lines of each record.

An xref may be given without its @ delimiters. Xrefs are case-sensitive. When
a file defines an xref twice, the first definition is printed.

HEAD, exactly, prints the header's lines instead, and may be given among the
xrefs; @HEAD@ is an xref. The header is printed as the file has it, even with
--redact-living: like a submitter record, it is never redacted, so check any
names in its lines yourself. 'gedcom put-header' takes the header back;
'gedcom put' replaces records only.

--format json prints an array holding each record in gedcom-go's own shape.
Its keys are the library's field names (XRef, Names, Husband), not the
lowercase keys 'gedcom list' and 'gedcom validate' print, and they may change
with the library. Each record also has a Type key, its level-0 tag (INDI,
FAM, SUBM, _CUSTOM), which says what the other keys describe; the header's
is HEAD.`,
		Example: `  gedcom get family.ged @I1@
  gedcom get family.ged I1 F1
  gedcom get family.ged HEAD @U1@
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
			selected, err := selectRecords(doc, args[1:])
			if err != nil {
				return err
			}
			if format == formatJSON {
				return writeRecordsJSON(cmd.OutOrStdout(), selected)
			}
			return writeRecordsText(cmd.OutOrStdout(), doc, selected)
		},
	}
}

// selectRecords resolves get's arguments, in order: each is the
// *gedcom.Header for HEAD, or the *gedcom.Record findRecords finds.
func selectRecords(doc *gedcom.Document, args []string) ([]any, error) {
	isHeader := func(arg string) bool { return arg == headerArg }
	records, err := findRecords(doc, slices.DeleteFunc(slices.Clone(args), isHeader))
	if err != nil {
		return nil, err
	}
	header := doc.Header
	if header == nil {
		header = &gedcom.Header{}
	}
	selected := make([]any, 0, len(args))
	for _, arg := range args {
		if isHeader(arg) {
			selected = append(selected, header)
			continue
		}
		selected = append(selected, records[0])
		records = records[1:]
	}
	return selected, nil
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

// writeRecordsJSON always writes an array: the header, or each record's typed
// entity, with Type added, or, when a record has none, the record itself,
// which has a Type of its own.
func writeRecordsJSON(w io.Writer, selected []any) error {
	out := make([]any, len(selected))
	for i, item := range selected {
		var err error
		switch item := item.(type) {
		case *gedcom.Header:
			out[i], err = withType(headerArg, item)
		case *gedcom.Record:
			if item.Entity == nil {
				out[i] = item
				continue
			}
			out[i], err = withType(item.Type, item.Entity)
		}
		if err != nil {
			return err
		}
	}
	return writeJSON(w, out)
}

// withType is entity's JSON object with a Type key, the record type, put
// first. No typed entity, nor the header, has a Type field of its own, so the
// key is not duplicated, and the entity's own keys keep their order.
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

// writeRecordsText writes the header's lines, or each record's, as Encode
// would write them within doc, with no trailer.
func writeRecordsText(w io.Writer, doc *gedcom.Document, selected []any) error {
	for _, item := range selected {
		switch item := item.(type) {
		case *gedcom.Header:
			if err := encodeHeader(w, item); err != nil {
				return err
			}
		case *gedcom.Record:
			if err := encoder.EncodeRecord(w, doc, item, nil); err != nil {
				return err
			}
		}
	}
	return nil
}

// encodeHeader writes the header's lines as Encode would write them.
func encodeHeader(w io.Writer, h *gedcom.Header) error {
	// A stream encoder writes the header Encode writes. It is left without
	// its trailer, so it is flushed rather than closed.
	enc := encoder.NewStreamEncoder(w)
	if err := enc.WriteHeader(h); err != nil {
		return err
	}
	return enc.Flush()
}
