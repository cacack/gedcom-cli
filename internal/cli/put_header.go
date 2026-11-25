package cli

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/cacack/gedcom-go/v3/decoder"
	"github.com/cacack/gedcom-go/v3/gedcom"
	"github.com/spf13/cobra"
)

// putHeaderReport is what put-header did: whether it replaced the header, and
// what the decoder could not read cleanly in the header given.
type putHeaderReport struct {
	Input    string         `json:"input"`
	Header   string         `json:"header"`
	Output   string         `json:"output"`
	Written  bool           `json:"written"`
	Replaced bool           `json:"replaced"`
	Counts   severityCounts `json:"counts"`
	Findings []finding      `json:"findings"`
}

func newPutHeaderCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "put-header <file> <header.ged> -o <out>",
		Short: "Replace the header of a file from a file of GEDCOM header lines",
		Long: `Write file to a new file with its header (HEAD) replaced by the lines of
header.ged, as 'gedcom get <file> HEAD' prints them: a "0 HEAD" line first,
then its substructures, with no records and no TRLR. "-" reads them from
stdin. Lines can be removed, changed and added in one run.

Every line outside the header is written exactly as it was read. A header
whose lines are unchanged is left as it was, so putting back an unedited copy
writes the file byte-identical. A replaced header is written as its new
lines, without the old header's indentation or blank lines.

The header declares how the rest of the file is read. A header whose GEDC.VERS
or CHAR differs from the file's header as 'gedcom get' prints it, or adds or
removes either line, is refused: nothing is written and the exit code is 1.
Use 'gedcom convert' to change the version or the character set. The file is
written in UTF-8, so for a file in ANSEL or ASCII 'gedcom get' prints CHAR
UTF-8, and that is the CHAR to give.

What the decoder could not read cleanly in header.ged is reported as
'gedcom validate' reports it, with line numbers counted in header.ged. When
any finding is an error, nothing is written and the exit code is 1, with the
report printed. What the version requires of a header (SOUR, SUBM, GEDC and
CHAR in 5.5.1) is not checked, nor whether SUBM points at a record the file
defines: run 'gedcom validate' on the output.

A failure before the report exists (an unreadable file, header.ged not
starting with "0 HEAD" or holding records or TRLR, a changed GEDC.VERS or
CHAR) prints no report, only the error on stderr.

With --redact-living the file is written unredacted, and the header is never
redacted, as in 'gedcom get'.

-o is required and may not name file or header.ged. An existing output file is
replaced only once the whole result is written. With -o - the GEDCOM goes to
stdout and the report to stderr.`,
		Example: `  # The edit loop for the header: print it, change its lines, put it back.
  gedcom get family.ged HEAD > head.ged
  $EDITOR head.ged
  gedcom put-header family.ged head.ged -o family-edited.ged
  gedcom diff --include-header family.ged family-edited.ged

  gedcom get family.ged HEAD | grep -v '^1 DEST ' | gedcom put-header family.ged - -o fixed.ged
  gedcom put-header --format json family.ged head.ged -o family-edited.ged | jq .replaced`,
		Args: exactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			format, err := outputFormat(cmd)
			if err != nil {
				return err
			}
			out, err := outputPath(cmd, args[0], args[1])
			if err != nil {
				return err
			}
			report, putErr := putHeader(cmd, args[0], args[1], out)
			if report == nil {
				return putErr
			}
			w := reportWriter(cmd, out)
			if format == formatJSON {
				err = writeJSON(w, report)
			} else {
				err = writePutHeaderText(w, report)
			}
			if err != nil {
				return err
			}
			return putErr
		},
	}
	addOutputFlag(cmd)
	return cmd
}

// putHeader replaces the header of the file at in with the one at header and
// writes the result to out. It returns the report whenever the header was
// read, with an error when its findings stopped the write.
func putHeader(cmd *cobra.Command, in, header, out string) (*putHeaderReport, error) {
	res, err := decodeForWrite(in)
	if err != nil {
		return nil, err
	}
	doc := res.Document
	frag, err := decodeHeaderFragment(cmd, header)
	if err != nil {
		return nil, err
	}

	findings := decodeFindings(frag.Diagnostics)
	report := &putHeaderReport{
		Input:    in,
		Header:   header,
		Output:   out,
		Counts:   countSeverities(findings),
		Findings: findings,
	}
	if n := report.Counts.Error; n > 0 {
		return report, fmt.Errorf("%s in %s; %s not written", plural(n, "error", "errors"), inputName(header), outputName(out))
	}

	// The decoder reads any lines as a document, so what makes them a
	// header alone is checked on what it read.
	given := frag.Document
	switch {
	case given.Layout.Head != 1:
		return nil, fmt.Errorf(`%s: the first line is not "0 HEAD"`, inputName(header))
	case len(given.Records) > 0:
		return nil, fmt.Errorf("%s: holds records after the header; 'gedcom put' puts records", inputName(header))
	case given.Layout.Trailer != 0:
		return nil, fmt.Errorf("%s: holds a TRLR line; give the header only", inputName(header))
	}
	old := doc.Header
	if old == nil {
		old = &gedcom.Header{}
	}
	printed, err := printedHeader(old)
	if err != nil {
		return nil, err
	}
	for _, path := range [][]string{{"GEDC", "VERS"}, {"CHAR"}} {
		was, is := headerValue(printed, path), headerValue(given.Header, path)
		if was != is {
			return nil, fmt.Errorf("%s: %s %s differs from the file's %s; use 'gedcom convert' to change it",
				inputName(header), strings.Join(path, "."), is, was)
		}
	}

	same, err := sameHeaderLines(old, given.Header)
	if err != nil {
		return nil, err
	}
	if !same {
		// Line numbers of header.ged would name lines of the file's source,
		// whose layout the encoder would then borrow; the new lines have none.
		for _, tag := range given.Header.Tags {
			tag.LineNumber = 0
		}
		doc.Header, doc.Vendor, doc.Schema = given.Header, given.Vendor, given.Schema
		report.Replaced = true
	}

	if err := writeOutput(cmd, out, doc); err != nil {
		return nil, err
	}
	report.Written = true
	return report, nil
}

// decodeHeaderFragment decodes the header lines at path, or stdin for "-", as
// a document of their own, recording its layout.
func decodeHeaderFragment(cmd *cobra.Command, path string) (*decoder.DecodeResult, error) {
	var r io.Reader = cmd.InOrStdin()
	if path != stdio {
		f, err := os.Open(filepath.Clean(path))
		if err != nil {
			return nil, err
		}
		defer f.Close()
		r = f
	}
	opts := decoder.DefaultOptions()
	opts.PreserveLayout = true
	res, err := decoder.DecodeWithDiagnostics(r, opts)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", inputName(path), err)
	}
	return res, nil
}

// printedHeader is h as 'gedcom get <file> HEAD' prints it, read back. The
// encoder writes UTF-8 and declares it in CHAR, so for a file in another
// character set the header given back says UTF-8, as the file written will.
func printedHeader(h *gedcom.Header) (*gedcom.Header, error) {
	var lines bytes.Buffer
	if err := encodeHeader(&lines, h); err != nil {
		return nil, err
	}
	res, err := decoder.DecodeWithDiagnostics(&lines, decoder.DefaultOptions())
	if err != nil {
		return nil, err
	}
	if res.Document.Header == nil {
		return &gedcom.Header{}, nil
	}
	return res.Document.Header, nil
}

// headerValue is the line at path in h, as "absent" or its value quoted.
func headerValue(h *gedcom.Header, path []string) string {
	tag := h.Find(path...)
	if tag == nil {
		return "absent"
	}
	return fmt.Sprintf("%q", tag.Value)
}

// sameHeaderLines reports whether a and b encode to the same lines, so that
// replacing a with b would change nothing but the layout.
func sameHeaderLines(a, b *gedcom.Header) (bool, error) {
	var aLines, bLines bytes.Buffer
	if err := encodeHeader(&aLines, a); err != nil {
		return false, err
	}
	if err := encodeHeader(&bLines, b); err != nil {
		return false, err
	}
	return bytes.Equal(aLines.Bytes(), bLines.Bytes()), nil
}

// writePutHeaderText writes a summary line, then the header's findings as
// validate groups them.
func writePutHeaderText(w io.Writer, r *putHeaderReport) error {
	var line string
	switch {
	case !r.Written:
		line = fmt.Sprintf("%s not written\n", outputName(r.Output))
	case r.Replaced:
		line = fmt.Sprintf("wrote %s: header replaced\n", outputName(r.Output))
	default:
		line = fmt.Sprintf("wrote %s: header unchanged\n", outputName(r.Output))
	}
	if _, err := io.WriteString(w, line); err != nil {
		return err
	}
	return writeFindingGroups(w, r.Counts, r.Findings)
}
