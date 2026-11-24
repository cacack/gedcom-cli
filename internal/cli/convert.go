package cli

import (
	"errors"
	"fmt"
	"io"
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

With -o -, the result goes to stdout and the report to stderr.

The report starts with what the decoder could not read cleanly in
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
and exits 1, with the report still printed.

With --redact-living, what the report says about a presumed-living person
reads Living: a decode finding on a line of their record, and a note,
transformation detail or validation issue naming it. The note that renames
their xref for 7.0 is kept, as an xref is not redacted. The file written is
not redacted.`,
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
			failOnLoss, _ := cmd.Flags().GetBool("fail-on-loss")
			if !slices.Contains(convertTargets, to) {
				return usageError{fmt.Errorf("invalid --to %q: want %s", to, strings.Join(convertTargets, " or "))}
			}
			out, err := outputPath(cmd, args[0])
			if err != nil {
				return err
			}

			report, convErr := convertFile(cmd, args[0], out, gedcom.Version(to), failOnLoss)
			if report == nil {
				return convErr
			}
			w := reportWriter(cmd, out)
			if format == formatJSON {
				err = writeJSON(w, report)
			} else {
				err = writeConvertText(w, report)
			}
			if err != nil {
				return err
			}
			return convErr
		},
	}
	cmd.Flags().String("to", "", "version to write: 5.5.1 or 7.0 (required)")
	addOutputFlag(cmd)
	cmd.Flags().Bool("fail-on-loss", false, "write nothing and exit 1 if the conversion would lose data")
	return cmd
}

// convertFile converts the file at in to version to and writes it to out. It
// returns the report whenever the conversion ran, with an error when data
// loss stopped it (failOnLoss) or the output could not be written.
func convertFile(cmd *cobra.Command, in, out string, to gedcom.Version, failOnLoss bool) (*convertReport, error) {
	living, err := readLivingOptions(cmd)
	if err != nil {
		return nil, err
	}
	res, err := decodeForWrite(in)
	if err != nil {
		return nil, err
	}
	decode := decodeFindings(res.Diagnostics)
	var hidden []string // the xrefs of the records --redact-living hides
	if living.redact {
		srcHidden := hiddenRecords(res.Document, presumedLiving(res.Document, living.cfg))
		redactFindings(decode, res.Document, srcHidden)
		hidden = xrefsOf(srcHidden)
	}
	opts := converter.DefaultOptions()
	opts.StrictDataLoss = failOnLoss
	converted, lib, err := converter.ConvertWithOptions(res.Document, to, opts)
	if lib == nil {
		return nil, fmt.Errorf("convert %s: %w", in, err)
	}
	report := newConvertReport(in, out, lib)
	report.Decode = decode
	if living.redact {
		redactConvertReport(report, hidden)
	}
	if errors.Is(err, converter.ErrDataLoss) {
		return report, fmt.Errorf("conversion of %s to %s would lose data (%s); %s not written",
			in, to, plural(len(lib.DataLoss), "item", "items"), outputName(out))
	}
	if err != nil {
		return nil, fmt.Errorf("convert %s: %w", in, err)
	}

	if err := writeOutput(cmd, out, converted); err != nil {
		return nil, err
	}
	report.Written = true
	return report, nil
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

// redactConvertReport hides what the report says about a record named by one
// of xrefs: a note's original, result and reason, a transformation detail and
// a validation issue read Living. The library reports these as text, so an
// entry is matched by the xref it names. Conversion to 7.0 may rename a
// record, and its normalized note maps the xref to the new one: that note is
// kept, as an xref is not redacted, and the new xref is hidden too.
func redactConvertReport(r *convertReport, xrefs []string) {
	isRename := func(n conversionNote) bool {
		return slices.Contains(xrefs, n.Original) && strings.HasPrefix(n.Result, "@") && strings.HasSuffix(n.Result, "@")
	}
	for _, n := range r.Normalized {
		if isRename(n) {
			xrefs = append(xrefs, n.Result)
		}
	}
	names := func(s string) bool {
		return slices.ContainsFunc(xrefs, func(x string) bool { return strings.Contains(s, x) })
	}
	for _, ns := range [][]conversionNote{r.Dropped, r.Approximated, r.Normalized, r.Preserved} {
		for i, n := range ns {
			if !isRename(n) && (names(n.Path) || names(n.Original) || names(n.Result)) {
				ns[i].Original, ns[i].Result, ns[i].Reason = redactedValue, redactedValue, redactedValue
			}
		}
	}
	for _, t := range r.Transformations {
		for i, d := range t.Details {
			if names(d) {
				t.Details[i] = redactedValue
			}
		}
	}
	for i, issue := range r.ValidationIssues {
		if names(issue) {
			r.ValidationIssues[i] = redactedValue
		}
	}
}

// xrefsOf is the xrefs of records, leaving out any without one.
func xrefsOf(records map[*gedcom.Record]bool) []string {
	var xrefs []string
	for record := range records {
		if record.XRef != "" {
			xrefs = append(xrefs, record.XRef)
		}
	}
	return xrefs
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
	status := "wrote " + outputName(r.Output)
	if !r.Written {
		status = outputName(r.Output) + " not written"
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
