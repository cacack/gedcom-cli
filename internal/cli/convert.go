package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/cacack/gedcom-go/v3/converter"
	"github.com/cacack/gedcom-go/v3/gedcom"
	"github.com/spf13/cobra"
)

// convertTargets are the versions convert writes. The output is always UTF-8,
// which GEDCOM 5.5 does not allow, so 5.5 is not a target.
var convertTargets = []string{string(gedcom.Version551), string(gedcom.Version70)}

// convertReport is the library's ConversionReport with lowercase keys, plus
// the files involved, whether the output was written, and what the decoder
// could not read cleanly in the input.
type convertReport struct {
	Input            string           `json:"input"`
	Output           string           `json:"output"`
	Written          bool             `json:"written"`
	SourceVersion    string           `json:"sourceVersion"`
	TargetVersion    string           `json:"targetVersion"`
	Decode           []finding        `json:"decode"`
	DataLoss         []dataLossItem   `json:"dataLoss"`
	Dropped          []conversionNote `json:"dropped"`
	Approximated     []conversionNote `json:"approximated"`
	Normalized       []conversionNote `json:"normalized"`
	Preserved        []conversionNote `json:"preserved"`
	Transformations  []transformation `json:"transformations"`
	ValidationIssues []string         `json:"validationIssues"`
}

type dataLossItem struct {
	Feature string   `json:"feature"`
	Reason  string   `json:"reason"`
	Records []string `json:"records"`
}

type conversionNote struct {
	Path     string `json:"path"`
	Original string `json:"original"`
	Result   string `json:"result"`
	Reason   string `json:"reason"`
}

type transformation struct {
	Type        string   `json:"type"`
	Description string   `json:"description"`
	Count       int      `json:"count"`
	Details     []string `json:"details"`
}

func newConvertCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "convert <file> --to <version> -o <out>",
		Short: "Rewrite a file as another GEDCOM version (5.5.1 or 7.0), in UTF-8",
		Long: `Rewrite a file as GEDCOM 5.5.1 or 7.0 with gedcom-go's converter, writing
the result to a new file in UTF-8 whatever the input's encoding (ANSEL, ASCII,
UTF-16 or a code page). The input is never modified, and the output may not be
the input file. An existing output file is replaced, and only once the whole
result is ready: a failed conversion leaves it as it was.

Lines the conversion does not change keep their layout (indentation, line
terminators, blank lines), so converting a UTF-8 file to the version it already
has writes it back byte-identical.

The report on stdout starts with what the decoder could not read cleanly in
the input; a line it could not parse is copied to the output as it was. Then
comes the library's conversion report, most serious first:

  decode           decoder diagnostics: severity, code, line, message
  data loss        features the target version cannot hold, with the records
  dropped          each value that was dropped: path, original, result, reason
  approximated     values mapped to a near but not exact equivalent
  normalized       values whose form changed but whose meaning did not
  preserved        vendor tags (_TAG) carried through unchanged
  transformations  each kind of change, with a count
  validation       problems found in the converted document

With --fail-on-loss, a conversion that would lose data writes no output file
and exits 1, with the report still on stdout.`,
		Example: `  gedcom convert legacy.ged --to 5.5.1 -o legacy-utf8.ged
  gedcom convert --fail-on-loss legacy.ged --to 7.0 -o legacy-v7.ged
  gedcom convert --format json legacy.ged --to 5.5.1 -o out.ged | jq .dataLoss`,
		Args: exactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			format, err := outputFormat(cmd)
			if err != nil {
				return err
			}
			to, _ := cmd.Flags().GetString("to")
			out, _ := cmd.Flags().GetString("output")
			failOnLoss, _ := cmd.Flags().GetBool("fail-on-loss")
			if !slices.Contains(convertTargets, to) {
				return usageError{fmt.Errorf("invalid --to %q: want %s", to, strings.Join(convertTargets, " or "))}
			}
			if out == "" {
				return usageError{errors.New("--output (-o) is required")}
			}
			if err := checkNotInput(args[0], out); err != nil {
				return err
			}

			report, convErr := convertFile(args[0], out, gedcom.Version(to), failOnLoss)
			if report == nil {
				return convErr
			}
			if format == formatJSON {
				err = writeJSON(cmd.OutOrStdout(), report)
			} else {
				err = writeConvertText(cmd.OutOrStdout(), report)
			}
			if err != nil {
				return err
			}
			return convErr
		},
	}
	cmd.Flags().String("to", "", "version to write: 5.5.1 or 7.0 (required)")
	cmd.Flags().StringP("output", "o", "", "file to write the converted GEDCOM to (required)")
	cmd.Flags().Bool("fail-on-loss", false, "write nothing and exit 1 if the conversion would lose data")
	return cmd
}

// convertFile converts the file at in to version to and writes it to out. It
// returns the report whenever the conversion ran, with an error when data
// loss stopped it (failOnLoss) or the output could not be written.
func convertFile(in, out string, to gedcom.Version, failOnLoss bool) (*convertReport, error) {
	res, err := decodeForWrite(in)
	if err != nil {
		return nil, err
	}
	opts := converter.DefaultOptions()
	opts.StrictDataLoss = failOnLoss
	converted, lib, err := converter.ConvertWithOptions(res.Document, to, opts)
	if lib == nil {
		return nil, fmt.Errorf("convert %s: %w", in, err)
	}
	report := newConvertReport(in, out, lib)
	report.Decode = decodeFindings(res.Diagnostics)
	if errors.Is(err, converter.ErrDataLoss) {
		return report, fmt.Errorf("conversion of %s to %s would lose data (%s); %s not written",
			in, to, plural(len(lib.DataLoss), "item", "items"), out)
	}
	if err != nil {
		return nil, fmt.Errorf("convert %s: %w", in, err)
	}

	if err := writeFileAtomic(filepath.Clean(out), func(w io.Writer) error {
		return encodeDocument(w, converted)
	}); err != nil {
		return nil, err
	}
	report.Written = true
	return report, nil
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

func newConvertReport(in, out string, r *gedcom.ConversionReport) *convertReport {
	report := &convertReport{
		Input:            in,
		Output:           out,
		SourceVersion:    string(r.SourceVersion),
		TargetVersion:    string(r.TargetVersion),
		DataLoss:         []dataLossItem{},
		Dropped:          notes(r.Dropped),
		Approximated:     notes(r.Approximated),
		Normalized:       notes(r.Normalized),
		Preserved:        notes(r.Preserved),
		Transformations:  []transformation{},
		ValidationIssues: append([]string{}, r.ValidationIssues...),
	}
	for _, d := range r.DataLoss {
		report.DataLoss = append(report.DataLoss, dataLossItem{d.Feature, d.Reason, append([]string{}, d.AffectedRecords...)})
	}
	for _, t := range r.Transformations {
		report.Transformations = append(report.Transformations, transformation{t.Type, t.Description, t.Count, append([]string{}, t.Details...)})
	}
	return report
}

func notes(in []gedcom.ConversionNote) []conversionNote {
	out := make([]conversionNote, len(in))
	for i, n := range in {
		out[i] = conversionNote{n.Path, n.Original, n.Result, n.Reason}
	}
	return out
}

// writeConvertText writes a summary line, then each non-empty section of the
// report as a heading with a count and one tab-separated line per item.
func writeConvertText(w io.Writer, r *convertReport) error {
	status := "wrote " + r.Output
	if !r.Written {
		status = r.Output + " not written"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s: %s -> %s, %s\n", r.Input, versionName(r.SourceVersion), r.TargetVersion, status)

	section := func(name string, lines []string) {
		if len(lines) == 0 {
			return
		}
		fmt.Fprintf(&b, "%s (%d)\n", name, len(lines))
		for _, line := range lines {
			fmt.Fprintf(&b, "  %s\n", line)
		}
	}
	noteLines := func(ns []conversionNote) []string {
		lines := make([]string, len(ns))
		for i, n := range ns {
			lines[i] = joinFields(n.Path, n.Original, n.Result, n.Reason)
		}
		return lines
	}

	var decode []string
	for _, f := range r.Decode {
		decode = append(decode, joinFields(f.Severity, f.Code, fmt.Sprintf("line %d", f.Line), f.Message))
	}
	section("decode", decode)
	var loss []string
	for _, d := range r.DataLoss {
		loss = append(loss, joinFields(d.Feature, d.Reason, strings.Join(d.Records, " ")))
	}
	section("data loss", loss)
	section("dropped", noteLines(r.Dropped))
	section("approximated", noteLines(r.Approximated))
	section("normalized", noteLines(r.Normalized))
	section("preserved", noteLines(r.Preserved))
	var changes []string
	for _, t := range r.Transformations {
		changes = append(changes, joinFields(t.Type, t.Description, fmt.Sprint(t.Count)))
	}
	section("transformations", changes)
	var issues []string
	for _, issue := range r.ValidationIssues {
		issues = append(issues, oneLine(issue))
	}
	section("validation", issues)

	_, err := io.WriteString(w, b.String())
	return err
}

// joinFields is one report line: the fields, each on one line, tab-separated.
func joinFields(fields ...string) string {
	for i, f := range fields {
		fields[i] = oneLine(f)
	}
	return strings.Join(fields, "\t")
}

func versionName(v string) string {
	if v == "" {
		return "unknown version"
	}
	return v
}
