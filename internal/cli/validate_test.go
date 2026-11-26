package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/cacack/gedcom-go/v3/decoder"
	"github.com/cacack/gedcom-go/v3/validator"
)

const (
	problems     = "testdata/problems.ged"
	warningsOnly = "testdata/warnings.ged"
)

// problemCodes are the findings the library reports on the problems fixture,
// in report order: the decode diagnostic first, then the validation issues.
var problemCodes = []struct{ source, severity, code string }{
	{"decode", "error", "INVALID_LEVEL"},
	{"validate", "error", "DEATH_BEFORE_BIRTH"},
	{"validate", "error", "ORPHANED_FAMS"},
	{"validate", "warning", "MISSING_NAME"},
	{"validate", "info", "POTENTIAL_DUPLICATE"},
}

// validateJSON runs validate with --format json and decodes stdout strictly,
// so a renamed or added key fails the test.
func validateJSON(t *testing.T, args ...string) (code int, report validateReport, stdout, stderr string) {
	t.Helper()
	code, stdout, stderr = run(t, append([]string{"validate", "--format", "json"}, args...)...)
	dec := json.NewDecoder(strings.NewReader(stdout))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&report); err != nil {
		t.Fatalf("stdout is not a validate report: %v\n%s", err, stdout)
	}
	return code, report, stdout, stderr
}

func TestValidateCleanText(t *testing.T) {
	for _, file := range []string{sample, records} {
		code, stdout, stderr := run(t, "validate", file)
		if code != ExitOK || stderr != "" {
			t.Fatalf("%s: exit = %d, stderr = %q", file, code, stderr)
		}
		if want := file + ": no findings\n"; stdout != want {
			t.Errorf("stdout = %q, want %q", stdout, want)
		}
	}
}

func TestValidateCleanJSON(t *testing.T) {
	code, report, stdout, stderr := validateJSON(t, "--min-severity", "info", sample)
	if code != ExitOK || stderr != "" {
		t.Fatalf("exit = %d, stderr = %q", code, stderr)
	}
	if report.File != sample || report.Counts != (severityCounts{}) {
		t.Errorf("unexpected report: %+v", report)
	}
	// An empty report still has a findings array and all three counts.
	for _, want := range []string{`"findings": []`, `"error": 0`, `"warning": 0`, `"info": 0`} {
		if !strings.Contains(stdout, want) {
			t.Errorf("stdout missing %s:\n%s", want, stdout)
		}
	}
}

func TestValidateProblemsText(t *testing.T) {
	code, stdout, stderr := run(t, "validate", problems)
	if code != ExitError {
		t.Fatalf("exit = %d, want %d (stderr %q)", code, ExitError, stderr)
	}
	if want := "gedcom: 3 errors in " + problems + "\n"; stderr != want {
		t.Errorf("stderr = %q, want %q", stderr, want)
	}
	// Groups come errors first, each with its count; info is below the default.
	errorsAt, warningsAt := strings.Index(stdout, "error (3)\n"), strings.Index(stdout, "warning (1)\n")
	if errorsAt != 0 || warningsAt < errorsAt {
		t.Errorf("want an error group of 3, then a warning group of 1:\n%s", stdout)
	}
	if strings.Contains(stdout, "info (") || strings.Contains(stdout, "POTENTIAL_DUPLICATE") {
		t.Errorf("info findings shown at the default --min-severity:\n%s", stdout)
	}
	for _, want := range []string{
		"@I1@ line 13\tINVALID_LEVEL\t",
		"\tdecode\n",
		"@I1@\tDEATH_BEFORE_BIRTH\t",
		"@I1@ line 18\tORPHANED_FAMS\t",
		"(related: @F9@)",
		"@I2@ line 19\tMISSING_NAME\t",
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("stdout missing %q:\n%s", want, stdout)
		}
	}
}

func TestValidateProblemsJSON(t *testing.T) {
	code, report, _, stderr := validateJSON(t, "--min-severity", "info", problems)
	if code != ExitError || !strings.Contains(stderr, "3 errors in "+problems) {
		t.Fatalf("exit = %d, stderr = %q", code, stderr)
	}
	if report.File != problems || report.Counts != (severityCounts{Error: 3, Warning: 1, Info: 1}) {
		t.Errorf("file = %q, counts = %+v", report.File, report.Counts)
	}
	if len(report.Findings) != len(problemCodes) {
		t.Fatalf("got %d findings, want %d: %+v", len(report.Findings), len(problemCodes), report.Findings)
	}
	for i, want := range problemCodes {
		got := report.Findings[i]
		if got.Source != want.source || got.Severity != want.severity || got.Code != want.code {
			t.Errorf("finding %d = %s/%s/%s, want %s/%s/%s", i, got.Source, got.Severity, got.Code, want.source, want.severity, want.code)
		}
		if got.Message == "" {
			t.Errorf("finding %d (%s) has no message", i, got.Code)
		}
	}
	if d := report.Findings[0]; d.Line != 13 || d.Context != "this line is not GEDCOM" || d.XRef != "@I1@" {
		t.Errorf("decode diagnostic = %+v", d)
	}
	if f := report.Findings[2]; f.XRef != "@I1@" || f.RelatedXRef != "@F9@" || f.Line != 18 {
		t.Errorf("ORPHANED_FAMS = %+v", f)
	}
}

// TestValidateDecodeFindingXRef checks that a decode finding names the record
// its line is in, and that one in the header names none. The unreadable BIRT
// date is reported twice: by the decoder and, since gedcom-go v3.17.0, by the
// validator.
func TestValidateDecodeFindingXRef(t *testing.T) {
	const fixture = "testdata/decode-xref.ged"
	code, report, stdout, _ := validateJSON(t, fixture)
	if code != ExitError || len(report.Findings) != 4 {
		t.Fatalf("exit = %d, findings = %+v", code, report.Findings)
	}
	header, record := report.Findings[0], report.Findings[1]
	if header.Source != sourceDecode || header.Code != "INVALID_LEVEL" || header.Line != 7 || header.XRef != "" {
		t.Errorf("header finding = %+v, want no xref", header)
	}
	if record.Source != sourceDecode || record.Code != "INVALID_VALUE" || record.Line != 15 || record.XRef != "@I1@" {
		t.Errorf("record finding = %+v, want xref @I1@", record)
	}
	// The key is absent, not empty, on a finding outside any record.
	if strings.Count(stdout, `"xref"`) != 3 {
		t.Errorf("want an xref key on the three findings in records only:\n%s", stdout)
	}

	code, stdout, _ = run(t, "validate", fixture)
	if code != ExitError {
		t.Fatalf("exit = %d", code)
	}
	for _, want := range []string{"\n  line 7\tINVALID_LEVEL\t", "\n  @I1@ line 15\tINVALID_VALUE\t", "\n  @F1@ line 19\tINVALID_DATE\t"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("stdout missing %q:\n%s", want, stdout)
		}
	}

	_, stdout, _ = run(t, "validate", "--help")
	if !strings.Contains(stdout, "xref is the record a validate finding is\nabout, or the record a decode finding's line is in") {
		t.Errorf("help does not document the xref key:\n%s", stdout)
	}
}

// TestValidateMinSeverity checks that the filter limits findings and counts
// but never the exit code.
func TestValidateMinSeverity(t *testing.T) {
	tests := []struct {
		min    string
		counts severityCounts
	}{
		{"error", severityCounts{Error: 3}},
		{"warning", severityCounts{Error: 3, Warning: 1}},
		{"info", severityCounts{Error: 3, Warning: 1, Info: 1}},
	}
	for _, tt := range tests {
		t.Run(tt.min, func(t *testing.T) {
			code, report, _, stderr := validateJSON(t, "--min-severity", tt.min, problems)
			if code != ExitError || stderr == "" {
				t.Errorf("exit = %d, stderr = %q: want %d with a summary", code, stderr, ExitError)
			}
			if report.Counts != tt.counts {
				t.Errorf("counts = %+v, want %+v", report.Counts, tt.counts)
			}
			if want := tt.counts.Error + tt.counts.Warning + tt.counts.Info; len(report.Findings) != want {
				t.Errorf("got %d findings, want %d", len(report.Findings), want)
			}
		})
	}
}

// TestValidateReportsMinSeverity checks that the JSON report names the filter
// its counts were limited by.
func TestValidateReportsMinSeverity(t *testing.T) {
	if _, report, _, _ := validateJSON(t, problems); report.MinSeverity != "warning" {
		t.Errorf("default minSeverity = %q, want warning", report.MinSeverity)
	}
	if _, report, _, _ := validateJSON(t, "--min-severity", "info", problems); report.MinSeverity != "info" {
		t.Errorf("minSeverity = %q, want info", report.MinSeverity)
	}
}

type fakeSeverity string

func (s fakeSeverity) String() string { return string(s) }

// TestSeverityName pins the library's three levels and checks that a level
// this command does not know is never hidden: it is reported as an error.
func TestSeverityName(t *testing.T) {
	for in, want := range map[string]string{"ERROR": "error", "WARNING": "warning", "INFO": "info", "UNKNOWN(3)": "error"} {
		if got := severityName(fakeSeverity(in)); got != want {
			t.Errorf("severityName(%q) = %q, want %q", in, got, want)
		}
	}
	for _, s := range []fmt.Stringer{
		decoder.SeverityError, decoder.SeverityWarning, decoder.SeverityInfo,
		validator.SeverityError, validator.SeverityWarning, validator.SeverityInfo,
	} {
		if !slices.Contains(severities, strings.ToLower(s.String())) {
			t.Errorf("library severity %q is not one of %v", s, severities)
		}
	}
}

func TestValidateDefaultMinSeverityIsWarning(t *testing.T) {
	_, byDefault, _, _ := validateJSON(t, problems)
	_, explicit, _, _ := validateJSON(t, "--min-severity", "warning", problems)
	if !slices.Equal(byDefault.Findings, explicit.Findings) || byDefault.Counts != explicit.Counts {
		t.Errorf("default report differs from --min-severity warning:\n%+v\n%+v", byDefault, explicit)
	}
}

func TestValidateWarningsOnly(t *testing.T) {
	code, stdout, stderr := run(t, "validate", warningsOnly)
	if code != ExitOK || stderr != "" {
		t.Fatalf("exit = %d, stderr = %q", code, stderr)
	}
	if !strings.HasPrefix(stdout, "warning (1)\n") || !strings.Contains(stdout, "\tMISSING_NAME\t") {
		t.Errorf("stdout = %q", stdout)
	}

	// Filtered down to nothing, the file reads as clean and still exits 0.
	code, stdout, stderr = run(t, "validate", "--min-severity", "error", warningsOnly)
	if code != ExitOK || stderr != "" {
		t.Fatalf("exit = %d, stderr = %q", code, stderr)
	}
	if want := warningsOnly + ": no findings\n"; stdout != want {
		t.Errorf("stdout = %q, want %q", stdout, want)
	}
}

// TestValidateUnreciprocatedLinks checks that a family link only one record
// claims is a warning naming the individual, with the family as related xref,
// in either direction.
func TestValidateUnreciprocatedLinks(t *testing.T) {
	const fixture = "testdata/unreciprocated.ged"
	want := []finding{
		{Code: "UNRECIPROCATED_FAMS", XRef: "@I1@", RelatedXRef: "@F1@", Line: 13},
		{Code: "UNRECIPROCATED_FAMC", XRef: "@I4@", RelatedXRef: "@F1@", Line: 25},
		{Code: "UNRECIPROCATED_HUSB", XRef: "@I5@", RelatedXRef: "@F2@", Line: 39},
		{Code: "UNRECIPROCATED_WIFE", XRef: "@I6@", RelatedXRef: "@F2@", Line: 40},
		{Code: "UNRECIPROCATED_CHIL", XRef: "@I7@", RelatedXRef: "@F2@", Line: 41},
	}
	code, report, _, stderr := validateJSON(t, "--min-severity", "info", fixture)
	if code != ExitOK || stderr != "" {
		t.Fatalf("exit = %d, stderr = %q", code, stderr)
	}
	if report.Counts != (severityCounts{Warning: len(want)}) || len(report.Findings) != len(want) {
		t.Fatalf("counts = %+v, findings = %+v", report.Counts, report.Findings)
	}
	for i, w := range want {
		got := report.Findings[i]
		if got.Source != sourceValidate || got.Severity != "warning" || got.Code != w.Code ||
			got.XRef != w.XRef || got.RelatedXRef != w.RelatedXRef || got.Line != w.Line || got.Message == "" {
			t.Errorf("finding %d = %+v, want %s on %s (related %s) at line %d", i, got, w.Code, w.XRef, w.RelatedXRef, w.Line)
		}
	}

	code, stdout, _ := run(t, "validate", fixture)
	if code != ExitOK || !strings.HasPrefix(stdout, "warning (5)\n") ||
		!strings.Contains(stdout, "@I1@ line 13\tUNRECIPROCATED_FAMS\t") || !strings.Contains(stdout, "(related: @F1@)") {
		t.Errorf("exit = %d, stdout:\n%s", code, stdout)
	}

	// Files whose family links all point back report none.
	for _, file := range []string{sample, records, lineage} {
		_, report, _, _ := validateJSON(t, "--min-severity", "info", file)
		for _, f := range report.Findings {
			if strings.HasPrefix(f.Code, "UNRECIPROCATED_") {
				t.Errorf("%s: unexpected %+v", file, f)
			}
		}
	}
}

func TestValidateSingleErrorSummary(t *testing.T) {
	path := filepath.Join(t.TempDir(), "one.ged")
	data := "0 HEAD\n1 GEDC\n2 VERS 5.5.1\n1 CHAR UTF-8\n1 SUBM @U1@\n0 @U1@ SUBM\n1 NAME Test Submitter\n" +
		"0 @I1@ INDI\n1 NAME Hal /Mock/\n1 FAMS @F9@\n0 TRLR\n"
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	code, _, stderr := run(t, "validate", path)
	if want := "gedcom: 1 error in " + path + "\n"; code != ExitError || stderr != want {
		t.Errorf("exit = %d, stderr = %q, want %d and %q", code, stderr, ExitError, want)
	}
}

func TestValidateFailures(t *testing.T) {
	garbage := filepath.Join(t.TempDir(), "garbage.ged")
	if err := os.WriteFile(garbage, []byte("not a GEDCOM file\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name string
		args []string
		want int
	}{
		{"missing file", []string{"validate", "testdata/does-not-exist.ged"}, ExitError},
		{"missing file as JSON", []string{"validate", "--format", "json", "testdata/does-not-exist.ged"}, ExitError},
		{"undecodable file", []string{"validate", garbage}, ExitError},
		{"unreadable path", []string{"validate", "testdata"}, ExitError},
		{"invalid min-severity", []string{"validate", "--min-severity", "loud", sample}, ExitUsage},
		{"min-severity is lowercase", []string{"validate", "--min-severity", "ERROR", sample}, ExitUsage},
		{"invalid min-severity wins over a missing file", []string{"validate", "--min-severity", "loud", "testdata/does-not-exist.ged"}, ExitUsage},
		{"missing argument", []string{"validate"}, ExitUsage},
		{"too many arguments", []string{"validate", sample, records}, ExitUsage},
		{"bad format", []string{"validate", "--format", "xml", sample}, ExitUsage},
		{"unknown style check", []string{"validate", "--style", "NAME_LOUD", sample}, ExitUsage},
		{"style codes are upper case", []string{"validate", "--style", "name_all_caps", sample}, ExitUsage},
		{"unknown skipped style check", []string{"validate", "--style", "all", "--skip-style", "all", sample}, ExitUsage},
		{"skip-style without style", []string{"validate", "--skip-style", "NAME_ALL_CAPS", sample}, ExitUsage},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code, stdout, stderr := run(t, tt.args...)
			if code != tt.want {
				t.Errorf("exit = %d, want %d (stderr %q)", code, tt.want, stderr)
			}
			if stdout != "" {
				t.Errorf("stdout = %q, want it empty", stdout)
			}
			if stderr == "" {
				t.Error("stderr is empty, want the reason")
			}
		})
	}
}

const nameStyle = "testdata/name-style.ged"

// nameStyleFindings are the findings --style all reports on the name-style
// fixture: one person per check, each on that person's offending NAME line.
var nameStyleFindings = []finding{
	{Code: validator.CodeNameAllCaps, XRef: "@I1@", Line: 11},
	{Code: validator.CodeNameTextAfterSurname, XRef: "@I2@", Line: 14},
	{Code: validator.CodeNameSpacing, XRef: "@I3@", Line: 16},
	{Code: validator.CodeNameQuotesOrParens, XRef: "@I4@", Line: 18},
	{Code: validator.CodeNameUncertainMark, XRef: "@I5@", Line: 20},
	{Code: validator.CodeNamePartPunctuation, XRef: "@I6@", Line: 22},
	{Code: validator.CodeNameVendorTag, XRef: "@I7@", Line: 25},
}

// TestValidateStyleOffByDefault checks that the style checks run only when
// asked for: without --style, a file full of style habits is clean.
func TestValidateStyleOffByDefault(t *testing.T) {
	code, stdout, stderr := run(t, "validate", "--min-severity", "info", nameStyle)
	if code != ExitOK || stderr != "" {
		t.Fatalf("exit = %d, stderr = %q", code, stderr)
	}
	if want := nameStyle + ": no findings\n"; stdout != want {
		t.Errorf("stdout = %q, want %q", stdout, want)
	}
}

func TestValidateStyleJSON(t *testing.T) {
	code, report, _, stderr := validateJSON(t, "--style", "all", nameStyle)
	// Style findings alone never fail the file.
	if code != ExitOK || stderr != "" {
		t.Fatalf("exit = %d, stderr = %q", code, stderr)
	}
	if report.Counts != (severityCounts{Warning: len(nameStyleFindings)}) || len(report.Findings) != len(nameStyleFindings) {
		t.Fatalf("counts = %+v, findings = %+v", report.Counts, report.Findings)
	}
	for i, w := range nameStyleFindings {
		got := report.Findings[i]
		if got.Source != sourceValidate || got.Severity != "warning" || got.Code != w.Code ||
			got.XRef != w.XRef || got.Line != w.Line || !strings.HasPrefix(got.Message, "Name style: ") {
			t.Errorf("finding %d = %+v, want %s on %s at line %d", i, got, w.Code, w.XRef, w.Line)
		}
	}
}

func TestValidateStyleText(t *testing.T) {
	code, stdout, stderr := run(t, "validate", "--style", "all", nameStyle)
	if code != ExitOK || stderr != "" {
		t.Fatalf("exit = %d, stderr = %q", code, stderr)
	}
	if !strings.HasPrefix(stdout, fmt.Sprintf("warning (%d)\n", len(nameStyleFindings))) {
		t.Errorf("stdout = %q", stdout)
	}
	want := "  @I1@ line 11\tNAME_ALL_CAPS\tName style: a given name or surname with a word of two or more letters all in capitals\tvalidate\n"
	if !strings.Contains(stdout, want) {
		t.Errorf("stdout lacks %q:\n%s", want, stdout)
	}
}

// TestValidateStyleSelect checks that checks are chosen by code, comma-separated
// or repeated, and that --skip-style leaves codes out.
func TestValidateStyleSelect(t *testing.T) {
	codes := func(report validateReport) []string {
		var out []string
		for _, f := range report.Findings {
			out = append(out, f.Code)
		}
		return out
	}
	tests := []struct {
		name string
		args []string
		want []string
	}{
		{"one", []string{"--style", "NAME_SPACING"}, []string{"NAME_SPACING"}},
		{"comma-separated", []string{"--style", "NAME_VENDOR_TAG,NAME_ALL_CAPS"}, []string{"NAME_ALL_CAPS", "NAME_VENDOR_TAG"}},
		{"repeated", []string{"--style", "NAME_VENDOR_TAG", "--style", "NAME_ALL_CAPS"}, []string{"NAME_ALL_CAPS", "NAME_VENDOR_TAG"}},
		{"all but some", []string{"--style", "all", "--skip-style", "NAME_TEXT_AFTER_SURNAME,NAME_SPACING"},
			[]string{"NAME_ALL_CAPS", "NAME_QUOTES_OR_PARENS", "NAME_UNCERTAIN_MARK", "NAME_PART_PUNCTUATION", "NAME_VENDOR_TAG"}},
		{"skip what is chosen", []string{"--style", "NAME_SPACING", "--skip-style", "NAME_SPACING"}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code, report, _, stderr := validateJSON(t, append(tt.args, nameStyle)...)
			if code != ExitOK || stderr != "" {
				t.Fatalf("exit = %d, stderr = %q", code, stderr)
			}
			if got := codes(report); !slices.Equal(got, tt.want) {
				t.Errorf("codes = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestValidateStyleClean checks that names written Given /Surname/ in mixed
// case, with clean parts, draw no style findings.
func TestValidateStyleClean(t *testing.T) {
	for _, file := range []string{sample, records, living} {
		code, stdout, stderr := run(t, "validate", "--style", "all", file)
		plainCode, plain, plainErr := run(t, "validate", file)
		if code != plainCode || stderr != plainErr {
			t.Errorf("%s: exit = %d, stderr = %q; without --style %d, %q", file, code, stderr, plainCode, plainErr)
		}
		if stdout != plain {
			t.Errorf("%s: --style all adds findings:\n%s\nwithout it:\n%s", file, stdout, plain)
		}
	}
}

// TestValidateStyleKeepsExitCode checks that style findings leave the exit
// code and the error summary to the other findings.
func TestValidateStyleKeepsExitCode(t *testing.T) {
	b, err := os.ReadFile(problems)
	if err != nil {
		t.Fatal(err)
	}
	file := writeTemp(t, "style-errors.ged", strings.Replace(string(b), "0 TRLR\n", "0 @I99@ INDI\n1 NAME Zed /EXAMPLE/\n0 TRLR\n", 1))
	_, _, plainErr := run(t, "validate", file)
	code, stdout, stderr := run(t, "validate", "--style", "all", file)
	if code != ExitError || stderr != plainErr {
		t.Errorf("exit = %d, stderr = %q, want %d and %q", code, stderr, ExitError, plainErr)
	}
	if !strings.Contains(stdout, "\tNAME_ALL_CAPS\t") {
		t.Errorf("stdout lacks the style finding:\n%s", stdout)
	}
}

// TestValidateStyleHelp checks that the help lists every style check the
// library has, with its code, and calls them style.
func TestValidateStyleHelp(t *testing.T) {
	_, stdout, _ := run(t, "validate", "--help")
	if !strings.Contains(stdout, "style, not conformance") {
		t.Errorf("help does not say the checks are style:\n%s", stdout)
	}
	for _, check := range validator.AllStyleChecks() {
		if !strings.Contains(stdout, check.Code) || !strings.Contains(stdout, check.Description) {
			t.Errorf("help lacks %s: %s", check.Code, check.Description)
		}
	}
}
