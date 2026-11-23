package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/cacack/gedcom-go/v3/decoder"
	"github.com/cacack/gedcom-go/v3/encoder"
	"github.com/cacack/gedcom-go/v3/gedcom"
	"github.com/spf13/cobra"
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

// stdio is the path that names stdin for an input and stdout for -o.
const stdio = "-"

// addOutputFlag registers the -o flag every write command takes.
func addOutputFlag(cmd *cobra.Command) {
	cmd.Flags().StringP("output", "o", "", `file to write the result to, or "-" for stdout (required)`)
}

// outputPath reads -o: it is required, and may not name any of the input
// files ins ("-" for stdin is skipped).
func outputPath(cmd *cobra.Command, ins ...string) (string, error) {
	out, err := cmd.Flags().GetString("output")
	if err != nil {
		return "", err
	}
	if out == "" {
		return "", usageError{errors.New("--output (-o) is required")}
	}
	if out == stdio {
		return out, nil
	}
	outInfo, outErr := os.Stat(out)
	for _, in := range ins {
		if in == stdio {
			continue
		}
		inInfo, err := os.Stat(in)
		if err != nil {
			return "", err
		}
		if outErr == nil && os.SameFile(inInfo, outInfo) {
			return "", usageError{fmt.Errorf("output %s is the input file %s; write to a new file", out, in)}
		}
	}
	return out, nil
}

// reportWriter is where a write command prints its report: stdout, unless
// -o - puts the GEDCOM there, when the report goes to stderr.
func reportWriter(cmd *cobra.Command, out string) io.Writer {
	if out == stdio {
		return cmd.ErrOrStderr()
	}
	return cmd.OutOrStdout()
}

// outputName is out as a report names it.
func outputName(out string) string {
	if out == stdio {
		return "stdout"
	}
	return out
}

// writeOutput encodes doc to out: stdout for "-", otherwise a file replaced
// only once the whole document is written.
func writeOutput(cmd *cobra.Command, out string, doc *gedcom.Document) error {
	if out == stdio {
		return encodeDocument(cmd.OutOrStdout(), doc)
	}
	return writeFileAtomic(filepath.Clean(out), func(w io.Writer) error {
		return encodeDocument(w, doc)
	})
}

// writeFileAtomic writes path through a temporary file in the same directory,
// renamed over path only once write and the sync succeed, so a failure leaves
// no partial file and an existing file at path untouched.
func writeFileAtomic(path string, write func(io.Writer) error) (err error) {
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = tmp.Close()
			_ = os.Remove(tmp.Name())
		}
	}()
	if err = write(tmp); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	if err = tmp.Chmod(0o644); err != nil {
		return err
	}
	if err = tmp.Sync(); err != nil {
		return err
	}
	if err = tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// plural is n with noun, pluralized with an s unless n is 1.
func plural(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return fmt.Sprintf("%d %ss", n, noun)
}
