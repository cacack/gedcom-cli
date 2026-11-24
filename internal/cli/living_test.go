package cli

import (
	"encoding/json"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// living is a fixture of five people: Ann (died) and Bob (born more than 110
// years before referenceYear) are not presumed living; Cleo, Dirk (no dates)
// and Esme are. Cleo's and Dirk's family, and the note Cleo points to, are
// hidden by --redact-living with them.
const living = "testdata/living.ged"

// referenceYear fixes the living test, so the tests do not depend on the clock.
const referenceYear = "2026"

// livingDetails is everything the living fixture says about the living: no
// --redact-living output may contain any of it.
var livingDetails = []string{
	"Cleo", "Dirk", "Esme", // names
	"1950", "1975", "1980", // dates
	"Hiddenvale",                                          // places
	"Secret Lane",                                         // address
	"Private note", "Inline note", "not GEDCOM", "31 FEB", // notes, a raw line, a quoted date
}

func TestListLiving(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want []bool // per individual, in file order
	}{
		{"default threshold", nil, []bool{false, false, true, true, true}},
		// With 50 years, Cleo's birth and her family's marriage (Dirk's) are old.
		{"living-years", []string{"--living-years", "50"}, []bool{false, false, false, false, true}},
		// Bob, born 1902 with no death, was within 110 years in 2000.
		{"reference-year", []string{"--reference-year", "2000"}, []bool{false, true, true, true, true}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args := append([]string{"list", "--reference-year", referenceYear}, tt.args...)
			args = append(args, living, "indi")
			code, stdout, stderr := run(t, args...)
			if code != ExitOK || stderr != "" {
				t.Fatalf("exit = %d, stderr = %q", code, stderr)
			}
			lines := strings.Split(strings.TrimSuffix(stdout, "\n"), "\n")
			if len(lines) != len(tt.want) {
				t.Fatalf("got %d lines, want %d:\n%s", len(lines), len(tt.want), stdout)
			}
			for i, line := range lines {
				fields := strings.Split(line, "\t")
				if got, want := fields[len(fields)-1], map[bool]string{true: "true", false: "false"}[tt.want[i]]; got != want {
					t.Errorf("line %d: last column = %q, want %q", i+1, got, want)
				}
			}

			code, stdout, stderr = run(t, append([]string{"--format", "json"}, args...)...)
			if code != ExitOK || stderr != "" {
				t.Fatalf("json: exit = %d, stderr = %q", code, stderr)
			}
			var rows []map[string]any
			if err := json.Unmarshal([]byte(stdout), &rows); err != nil {
				t.Fatalf("stdout is not JSON: %v\n%s", err, stdout)
			}
			var got []bool
			for _, r := range rows {
				b, ok := r["living"].(bool)
				if !ok {
					t.Fatalf("row %v has no boolean living", r)
				}
				got = append(got, b)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("living = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestRedactLiving runs every command with --redact-living. Add each new
// command here.
func TestRedactLiving(t *testing.T) {
	all := []string{"@I1@", "@I2@", "@I3@", "@I4@", "@I5@", "@F1@", "@F2@", "@N1@"}
	out := filepath.Join(t.TempDir(), "out.ged")
	tests := []struct {
		name string
		args []string
		code int
		keep []string // what must still be shown
	}{
		{"info", []string{"info", living}, ExitOK, []string{"INDI  5"}},
		{"get", append([]string{"get", living}, all...), ExitOK, []string{"0 @I3@ INDI", "1 NAME Living", "1 FAMS @F2@", "1 CHIL @I5@", "0 @N1@ NOTE Living", "Ann /Example/", "1 JUN 1925"}},
		{"get json", append([]string{"get", "--format", "json", living}, all...), ExitOK, []string{`"@I3@"`, `"Living"`, "Ann /Example/"}},
		{"list indi", []string{"list", living, "indi"}, ExitOK, []string{"@I3@\tLiving\t\t\ttrue", "Ann /Example/"}},
		{"list fam", []string{"list", living, "fam"}, ExitOK, []string{"@F2@\t@I4@\t@I3@\t\t1", "1 JUN 1925"}},
		{"list json", []string{"list", "--format", "json", living, "indi"}, ExitOK, []string{`"name": "Living"`, "Ann /Example/"}},
		{"extract", []string{"extract", living, "I1", "I3", "I5", "-o", "-"}, ExitOK, []string{"0 @I3@ INDI", "1 NAME Living", "1 CHIL @I5@", "Ann /Example/", "1 JUN 1925"}},
		{"extract json", []string{"extract", "--format", "json", living, "I3", "-o", "-"}, ExitOK, []string{"0 @I3@ INDI", "1 NAME Living"}},
		{"validate", []string{"validate", "--min-severity", "info", living}, ExitError, []string{"INVALID_LEVEL\tLiving", "@I5@ line 46\tINVALID_DATE\tLiving"}},
		{"merge-people", []string{"merge-people", living, "@I3@", "@I5@", "-o", out}, ExitOK, []string{"conflict\tBIRT\tLiving", "repointed\t@F2@\tFAM.CHIL"}},
		{"merge-people json", []string{"merge-people", "--format", "json", living, "@I1@", "@I3@", "-o", out}, ExitOK, []string{"\"lines\": [\n        \"Living\""}},
		{"merge-families", []string{"merge-families", living, "@F1@", "@F2@", "-o", out}, ExitOK, []string{"conflict\tMARR\tLiving", "repointed\t@I5@\tINDI.FAMC"}},
		{"validate json", []string{"validate", "--format", "json", "--min-severity", "info", living}, ExitError, []string{`"code": "INVALID_DATE"`, `"message": "Living"`}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args := append([]string{"--redact-living", "--reference-year", referenceYear}, tt.args...)
			code, stdout, stderr := run(t, args...)
			if code != tt.code {
				t.Fatalf("exit = %d, want %d, stderr = %q", code, tt.code, stderr)
			}
			for _, detail := range livingDetails {
				if strings.Contains(stdout, detail) || strings.Contains(stderr, detail) {
					t.Errorf("output shows %q:\n%s%s", detail, stdout, stderr)
				}
			}
			for _, want := range tt.keep {
				if !strings.Contains(stdout, want) {
					t.Errorf("stdout lacks %q:\n%s", want, stdout)
				}
			}
		})
	}
}

// TestLivingFixtureHasDetails checks that TestRedactLiving tests something:
// without --redact-living the fixture's details are shown.
func TestLivingFixtureHasDetails(t *testing.T) {
	_, got, _ := run(t, "get", living, "@I3@", "@I4@", "@I5@", "@F2@", "@N1@")
	_, findings, _ := run(t, "validate", living)
	for _, detail := range livingDetails {
		if !strings.Contains(got+findings, detail) {
			t.Errorf("unredacted output lacks %q", detail)
		}
	}
}

func TestLivingFlagErrors(t *testing.T) {
	for _, args := range [][]string{
		{"list", "--living-years", "0", living, "indi"},
		{"get", "--living-years", "-5", living, "@I1@"},
		{"validate", "--reference-year", "-1", living},
		{"info", "--redact-living=maybe", living},
	} {
		code, stdout, stderr := run(t, args...)
		if code != ExitUsage || stdout != "" || stderr == "" {
			t.Errorf("%v: exit = %d, stdout = %q, stderr = %q; want a usage error", args, code, stdout, stderr)
		}
	}
}

func TestLivingFlagsHelp(t *testing.T) {
	_, stdout, _ := run(t, "list", "--help")
	for _, want := range []string{"--redact-living", "--living-years int", "(default 110)", "--reference-year int", "0 means the current year"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("help lacks %q:\n%s", want, stdout)
		}
	}
}
