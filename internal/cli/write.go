package cli

import (
	"io"

	"github.com/cacack/gedcom-go/v3/decoder"
	"github.com/cacack/gedcom-go/v3/encoder"
	"github.com/cacack/gedcom-go/v3/gedcom"
)

// The write path. A command that writes a document back decodes it with
// decodeForWrite and encodes it with encodeDocument, so that every line it
// did not change is written exactly as it was read (CONSTITUTION.md C2).
// TestRoundTripByteIdentical holds this path, and every write command, to it.

// decodeForWrite is decodeFileWithDiagnostics recording the file's layout, for
// a command that will write the document back.
func decodeForWrite(path string) (*decoder.DecodeResult, error) {
	opts := decoder.DefaultOptions()
	opts.PreserveLayout = true
	return decodeFileWithOptions(path, opts)
}

// encodeDocument writes doc keeping the layout decodeForWrite recorded. doc
// must come from decodeForWrite: a document from decodeFile carries no layout,
// so it is written normalized rather than byte-identical.
func encodeDocument(w io.Writer, doc *gedcom.Document) error {
	opts := encoder.DefaultOptions()
	opts.PreserveLayout = true
	return encoder.EncodeWithOptions(w, doc, opts)
}
