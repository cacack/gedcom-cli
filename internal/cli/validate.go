package cli

import (
	"fmt"
	"io"
	"slices"
	"strings"

	gedcomgo "github.com/cacack/gedcom-go/v3"
	"github.com/cacack/gedcom-go/v3/decoder"
	"github.com/cacack/gedcom-go/v3/validator"
	"github.com/spf13/cobra"
)

// severities are the levels of a finding, most severe first. They are the
// library's own levels, lowercased.
var severities = []string{"error", "warning", "info"}

const (
	sourceDecode   = "decode"
	sourceValidate = "validate"
)

// finding is one decode diagnostic or validation issue, as the library
// reported it.
type finding struct {
	Source      string `json:"source"`
	Severity    string `json:"severity"`
	Code        string `json:"code"`
	Message     string `json:"message"`
	Line        int    `json:"line,omitempty"`
	XRef        string `json:"xref,omitempty"`
	RelatedXRef string `json:"relatedXref,omitempty"`
	Context     string `json:"context,omitempty"`
}

type severityCounts struct {
	Error   int `json:"error"`
	Warning int `json:"warning"`
	Info    int `json:"info"`
}

type validateReport struct {
	File string `json:"file"`
	// MinSeverity is the filter Counts and Findings were limited by.
	MinSeverity string         `json:"minSeverity"`
	Counts      severityCounts `json:"counts"`
	Findings    []finding      `json:"findings"`
}

func newValidateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "validate <file>",
		Short: "Report what the decoder could not read cleanly and what validation finds",
		Long: `Report the problems in a file: the lines the decoder could not read cleanly
(source "decode") and the issues validation finds in the records (source
"validate"). Findings are grouped by severity, errors first.

--min-severity limits the findings shown and counted. The exit code does not
depend on it: 1 when the file has any error-severity finding, otherwise 0.
With --format json, exit 1 with a report on stdout means the file has errors;
exit 1 with nothing on stdout means the file could not be read.

Findings are the library's, as reported. The decoder and the validator both
report a duplicated xref, so DUPLICATE_XREF appears once per source. A family
link only one record claims (a FAMS, FAMC, HUSB, WIFE or CHIL the other
record does not point back to) is an UNRECIPROCATED_<tag> warning on the
individual, with the family as related xref. Duplicate detection
(POTENTIAL_DUPLICATE) is slow on large files and runs only with --min-severity
info.

With --redact-living, the whole file is still validated, but a finding about
a presumed-living person, or a line in their record, reads Living instead of
its message.`,
		Example: `  gedcom validate family.ged
  gedcom validate --min-severity error family.ged
  gedcom validate --format json family.ged | jq '.findings[] | select(.code == "ORPHANED_FAMS")'`,
		Args: exactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			format, err := outputFormat(cmd)
			if err != nil {
				return err
			}
			minSeverity, err := cmd.Flags().GetString("min-severity")
			if err != nil {
				return err
			}
			if !slices.Contains(severities, minSeverity) {
				return usageError{fmt.Errorf("invalid --min-severity %q: want one of %s", minSeverity, strings.Join(severities, ", "))}
			}
			living, err := readLivingOptions(cmd)
			if err != nil {
				return err
			}
			all, err := findings(args[0], minSeverity == "info", living)
			if err != nil {
				return err
			}
			report := newValidateReport(args[0], all, minSeverity)
			if format == formatJSON {
				err = writeJSON(cmd.OutOrStdout(), report)
			} else {
				err = writeReportText(cmd.OutOrStdout(), report)
			}
			if err != nil {
				return err
			}
			// The exit code is decided on every finding, not on the filtered ones.
			if errs := countSeverities(all).Error; errs == 1 {
				return fmt.Errorf("1 error in %s", args[0])
			} else if errs > 1 {
				return fmt.Errorf("%d errors in %s", errs, args[0])
			}
			return nil
		},
	}
	cmd.Flags().String("min-severity", "warning", "least severe findings to report: error, warning or info")
	return cmd
}

// findings decodes the file at path and validates it, returning the decode
// diagnostics followed by the validation issues, each in the library's order.
//
// Duplicate detection is the validator's costliest check and reports at info
// severity, so it runs only when withDuplicates is set.
//
// Validation sees the whole document. With --redact-living, the findings about
// a record that redaction hides keep their code, xrefs and line but not their
// message or context, which can quote a name, date or place.
func findings(path string, withDuplicates bool, living livingOptions) ([]finding, error) {
	res, err := decodeFileWithDiagnostics(path)
	if err != nil {
		return nil, err
	}
	// Strict makes the library return every severity: --min-severity filters
	// afterwards, and the exit code needs every error.
	issues := gedcomgo.ValidateAllWithOptions(res.Document, &validator.ValidateOptions{
		Strictness:             validator.StrictnessStrict,
		SkipDuplicateDetection: !withDuplicates,
	})

	all := decodeFindings(res.Diagnostics)
	for _, i := range issues {
		all = append(all, finding{
			Source:      sourceValidate,
			Severity:    severityName(i.Severity),
			Code:        i.Code,
			Message:     i.Message,
			Line:        i.LineNumber,
			XRef:        i.RecordXRef,
			RelatedXRef: i.RelatedXRef,
		})
	}
	if living.redact {
		redactFindings(all, res.Document, hiddenRecords(res.Document, presumedLiving(res.Document, living.cfg)))
	}
	return all, nil
}

// decodeFindings is the decoder's diagnostics as findings, in its order.
func decodeFindings(diags decoder.Diagnostics) []finding {
	out := make([]finding, 0, len(diags))
	for _, d := range diags {
		out = append(out, finding{
			Source:   sourceDecode,
			Severity: severityName(d.Severity),
			Code:     d.Code,
			Message:  d.Message,
			Line:     d.Line,
			Context:  d.Context,
		})
	}
	return out
}

// severityName is a library severity as one of severities. A level this
// command does not know is reported as an error, so that it is never filtered
// out, left uncounted or missing from the text report.
func severityName(severity fmt.Stringer) string {
	name := strings.ToLower(severity.String())
	if !slices.Contains(severities, name) {
		return "error"
	}
	return name
}

// newValidateReport keeps the findings at minSeverity or above and counts them.
func newValidateReport(path string, all []finding, minSeverity string) validateReport {
	shown := make([]finding, 0, len(all))
	for _, f := range all {
		if slices.Index(severities, f.Severity) <= slices.Index(severities, minSeverity) {
			shown = append(shown, f)
		}
	}
	return validateReport{File: path, MinSeverity: minSeverity, Counts: countSeverities(shown), Findings: shown}
}

func countSeverities(findings []finding) severityCounts {
	var counts severityCounts
	for _, f := range findings {
		switch f.Severity {
		case "error":
			counts.Error++
		case "warning":
			counts.Warning++
		case "info":
			counts.Info++
		}
	}
	return counts
}

func writeReportText(w io.Writer, report validateReport) error {
	if len(report.Findings) == 0 {
		_, err := fmt.Fprintf(w, "%s: no findings\n", report.File)
		return err
	}
	return writeFindingGroups(w, report.Counts, report.Findings)
}

// writeFindingGroups writes findings grouped by severity, most severe first:
// a heading with each group's count, then one indented line per finding.
func writeFindingGroups(w io.Writer, c severityCounts, findings []finding) error {
	counts := map[string]int{"error": c.Error, "warning": c.Warning, "info": c.Info}
	for _, severity := range severities {
		if counts[severity] == 0 {
			continue
		}
		if _, err := fmt.Fprintf(w, "%s (%d)\n", severity, counts[severity]); err != nil {
			return err
		}
		for _, f := range findings {
			if f.Severity != severity {
				continue
			}
			if _, err := fmt.Fprintf(w, "  %s\n", findingLine(f)); err != nil {
				return err
			}
		}
	}
	return nil
}

// findingLine is one finding as text: where it is, its code, its message and
// its source, tab-separated.
func findingLine(f finding) string {
	var where []string
	if f.XRef != "" {
		where = append(where, f.XRef)
	}
	if f.Line > 0 {
		where = append(where, fmt.Sprintf("line %d", f.Line))
	}
	if len(where) == 0 {
		where = append(where, "-")
	}
	message := f.Message
	if f.RelatedXRef != "" {
		message += " (related: " + f.RelatedXRef + ")"
	}
	if f.Context != "" {
		message += fmt.Sprintf(" (context: %q)", f.Context)
	}
	return strings.Join([]string{oneLine(strings.Join(where, " ")), oneLine(f.Code), oneLine(message), f.Source}, "\t")
}
