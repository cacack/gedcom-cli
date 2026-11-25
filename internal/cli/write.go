package cli

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"math/rand/v2"
	"os"
	"path/filepath"
	"slices"
	"strings"

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

// inputName is the input path as a message names it.
func inputName(path string) string {
	if path == stdio {
		return "stdin"
	}
	return path
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

// stampSource is the HEAD.SOUR, and its NAME, of a header stamped by this
// tool; its VERS is version(), as 'gedcom --version' prints it.
const stampSource = "gedcom-cli"

// exportStructures are the header structures that describe the input's
// export rather than the file written (its destination, date and file name):
// a command that makes a new dataset removes them when it stamps the header.
var exportStructures = []string{"DEST", "DATE", "FILE"}

// stampHelp is the help of every command that stamps the header.
const stampHelp = `A stamped header names this tool as the system that wrote the file: its
SOUR structure is replaced by "1 SOUR ` + stampSource + `", with VERS (the version
'gedcom --version' prints) and NAME ` + stampSource + ` under it, where the SOUR was
(at the end of the header if it had none). The input's SOUR is copied
nowhere: 'gedcom get <file> HEAD' prints it from the input, which is never
written. The report says whether the header was stamped (JSON "stamped").`

// exportStampHelp is the help of a command that stamps a new dataset.
const exportStampHelp = `Stamping a new dataset also removes the header's DEST, DATE and FILE lines,
which describe the input's export rather than this file. Every other header
line is kept as read.`

// addStampFlag registers --stamp, the opt-in stamp of a command that edits a
// file in place of its author.
func addStampFlag(cmd *cobra.Command) {
	cmd.Flags().Bool("stamp", false, "stamp the header: replace its SOUR with "+stampSource+" and its version")
}

// addKeepHeaderFlag registers --keep-header, the opt-out of a command that
// stamps the file it writes by default.
func addKeepHeaderFlag(cmd *cobra.Command) {
	cmd.Flags().Bool("keep-header", false, "write the header as read instead of stamping it, as for a cut to be zipped back byte for byte")
}

// stampRequested reads --stamp, or --keep-header where the command stamps by
// default: whether the header of the file it writes is to be stamped.
func stampRequested(cmd *cobra.Command) (bool, error) {
	if cmd.Flags().Lookup("keep-header") != nil {
		keep, err := cmd.Flags().GetBool("keep-header")
		return !keep, err
	}
	return cmd.Flags().GetBool("stamp")
}

// stampedText is the end of a summary line that says the header was stamped.
func stampedText(stamped bool) string {
	if stamped {
		return ", header stamped"
	}
	return ""
}

// stampHeader replaces doc's header SOUR structure with this tool's
// (stampHelp), and removes the level-1 structures tagged remove. Every other
// header line is kept as it was. A header with no Tags is first given the lines the
// encoder writes for it, as 'gedcom get' prints them.
func stampHeader(doc *gedcom.Document, remove ...string) error {
	h := doc.Header
	if h == nil {
		h = &gedcom.Header{}
	}
	if len(h.Tags) == 0 {
		printed, err := printedHeader(h)
		if err != nil {
			return err
		}
		h = printed
	}
	stamp := []*gedcom.Tag{
		{Level: 1, Tag: "SOUR", Value: stampSource},
		{Level: 2, Tag: "VERS", Value: version()},
		{Level: 2, Tag: "NAME", Value: stampSource},
	}
	removed := map[*gedcom.Tag]bool{}
	for _, tag := range h.Tags {
		if tag != nil && tag.Level == 1 && (tag.Tag == "SOUR" || slices.Contains(remove, tag.Tag)) {
			for _, sub := range h.Subtree(tag) {
				removed[sub] = true
			}
		}
	}
	tags := make([]*gedcom.Tag, 0, len(h.Tags)+len(stamp))
	for _, tag := range h.Tags {
		// The stamp takes the place of the first SOUR.
		if stamp != nil && removed[tag] && tag.Level == 1 && tag.Tag == "SOUR" {
			tags, stamp = append(tags, stamp...), nil
		}
		if !removed[tag] {
			tags = append(tags, tag)
		}
	}
	stamped := *h
	stamped.Tags = append(tags, stamp...)
	doc.Header = &stamped
	return nil
}

// writeFileAtomic writes path through a temporary file in the same directory,
// renamed over path only once write and the sync succeed, so a failure leaves
// no partial file and an existing file at path untouched. The file replaced
// keeps its permissions; a new file gets 0666 less the umask, as os.Create
// would give it.
func writeFileAtomic(path string, write func(io.Writer) error) (err error) {
	perm, keep := os.FileMode(0o666), false
	if info, statErr := os.Stat(path); statErr == nil {
		perm, keep = info.Mode().Perm(), true
	}
	tmp, err := createTemp(path, perm)
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
	if keep {
		// The umask applied at create; the replaced file's mode is restored.
		if err = tmp.Chmod(perm); err != nil {
			return err
		}
	}
	if err = tmp.Sync(); err != nil {
		return err
	}
	if err = tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// createTemp creates a new file beside path with mode perm less the umask.
// os.CreateTemp is not used because it always creates the file 0600.
func createTemp(path string, perm os.FileMode) (*os.File, error) {
	dir, base := filepath.Split(path)
	for range 100 {
		name := filepath.Join(dir, fmt.Sprintf(".%s.%d.tmp", base, rand.Uint32()))
		f, err := os.OpenFile(name, os.O_RDWR|os.O_CREATE|os.O_EXCL, perm)
		if !errors.Is(err, fs.ErrExist) {
			return f, err
		}
	}
	return nil, fmt.Errorf("create a temporary file beside %s: too many attempts", path)
}

// plural is n with one when n is 1, otherwise with many.
func plural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return fmt.Sprintf("%d %s", n, many)
}

// batchRow is one row of a --batch file: its tab-separated fields and the
// line it is on.
type batchRow struct {
	line   int
	fields []string
}

// readBatch reads the rows of a --batch file: tab-separated lines of
// minFields to maxFields fields, the first minFields of them not empty; want
// names them for the error. Blank lines and lines starting with # are skipped.
// Every malformed line is reported, by line.
func readBatch(name string, minFields, maxFields int, want string) ([]batchRow, error) {
	f, err := os.Open(filepath.Clean(name))
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var rows []batchRow
	var errs []string
	scanner := bufio.NewScanner(f)
	for n := 1; scanner.Scan(); n++ {
		line := strings.TrimRight(scanner.Text(), "\r")
		if strings.TrimSpace(line) == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) < minFields || len(fields) > maxFields || slices.Contains(fields[:minFields], "") {
			errs = append(errs, fmt.Sprintf("%s line %d: want %s, tab-separated", name, n, want))
			continue
		}
		rows = append(rows, batchRow{line: n, fields: fields})
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if len(errs) > 0 {
		return nil, errors.New(strings.Join(errs, "\n"))
	}
	return rows, nil
}
