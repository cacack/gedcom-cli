package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"slices"
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

// annDeathHunk is diff's text for the one change to a record that is not
// hidden, which --redact-living must still print.
const annDeathHunk = "changed\t@I1@\tINDI\n  in INDI > DEAT\n  - line 17: 2 DATE 9 OCT 1979\n  + line 17: 2 DATE 9 OCT 1978\n"

// redactLivingLeaks is the known gaps in --redact-living: for a TestRedactLiving
// row, the livingDetails its command still shows. The row fails once it shows
// none of them, so a gap cannot outlive its fix. Do not add a new command here.
var redactLivingLeaks = map[string][]string{
	// convert does not redact its report: the validation finding about Esme
	// quotes her birth date.
	"convert": {"31 FEB", "1980"},
}

// TestRedactLiving runs every command with --redact-living. Add each new
// command here: the test fails for a command with no row.
func TestRedactLiving(t *testing.T) {
	all := []string{"@I1@", "@I2@", "@I3@", "@I4@", "@I5@", "@F1@", "@F2@", "@N1@"}
	out := filepath.Join(t.TempDir(), "out.ged")
	// A changed record for Esme (@I5@), for put to replace hers with.
	esme := filepath.Join(t.TempDir(), "esme.ged")
	if err := os.WriteFile(esme, []byte("0 @I5@ INDI\n1 NAME Esme /Changed/\n1 SEX F\n1 FAMC @F2@\n"), 0o600); err != nil {
		t.Fatal(err)
	}
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
		{"list plac", []string{"list", living, "plac"}, ExitOK, []string{"2\tOldtown, Example County\n"}},
		{"list plac json", []string{"list", "--format", "json", living, "plac"}, ExitOK, []string{`"place": "Oldtown, Example County"`, `"count": 2`}},
		{"list note", []string{"list", living, "note"}, ExitOK, []string{"@N1@\tLiving\n"}},
		{"list note json", []string{"list", "--format", "json", living, "note"}, ExitOK, []string{`"xref": "@N1@"`, `"text": "Living"`}},
		{"extract", []string{"extract", living, "I1", "I3", "I5", "-o", "-"}, ExitOK, []string{"0 @I3@ INDI", "1 NAME Living", "1 CHIL @I5@", "Ann /Example/", "1 JUN 1925"}},
		{"extract json", []string{"extract", "--format", "json", living, "I3", "-o", "-"}, ExitOK, []string{"0 @I3@ INDI", "1 NAME Living"}},
		{"validate", []string{"validate", "--min-severity", "info", living}, ExitError, []string{"INVALID_LEVEL\tLiving", "@I5@ line 46\tINVALID_DATE\tLiving"}},
		{"merge-people", []string{"merge-people", living, "@I3@", "@I5@", "-o", out}, ExitOK, []string{"conflict\tBIRT\tLiving", "repointed\t@F2@\tFAM.CHIL"}},
		{"merge-people json", []string{"merge-people", "--format", "json", living, "@I1@", "@I3@", "-o", out}, ExitOK, []string{"\"lines\": [\n        \"Living\""}},
		{"merge-families", []string{"merge-families", living, "@F1@", "@F2@", "-o", out}, ExitOK, []string{"conflict\tMARR\tLiving", "repointed\t@I5@\tINDI.FAMC"}},
		// living-changed.ged changes Ann's (@I1@) death date and Cleo's (@I3@)
		// birthplace; living-died.ged also gives Cleo a death, so she is living
		// in one file only and must still be hidden whichever side that is.
		{"diff", []string{"diff", living, "testdata/living-changed.ged"}, ExitOK, []string{"changed\t@I3@\tINDI\n  Living\n", annDeathHunk}},
		{"diff json", []string{"diff", "--format", "json", living, "testdata/living-changed.ged"}, ExitOK, []string{`"redacted": true`, `"redacted": false`, "9 OCT 1978"}},
		{"diff living in old only", []string{"diff", living, "testdata/living-died.ged"}, ExitOK, []string{"changed\t@I3@\tINDI\n  Living\n", annDeathHunk}},
		{"diff living in new only", []string{"diff", "testdata/living-died.ged", living}, ExitOK, []string{"changed\t@I3@\tINDI\n  Living\n", "  in INDI > DEAT\n  - line 17: 2 DATE 9 OCT 1978\n"}},
		{"validate json", []string{"validate", "--format", "json", "--min-severity", "info", living}, ExitError, []string{`"code": "INVALID_DATE"`, `"message": "Living"`}},
		// TestCombineRedactLiving checks the file written.
		{"combine", []string{"combine", "--strategy", "zip", living, living, "-o", filepath.Join(t.TempDir(), "out.ged")}, ExitOK, []string{"5 individuals"}},
		{"ancestors", []string{"ancestors", living, "@I5@"}, ExitOK, []string{"1\t@I3@\tLiving\t\t\ttrue", "Ann /Example/"}},
		{"descendants", []string{"descendants", living, "@I1@"}, ExitOK, []string{"1\t@I3@\tLiving", "2\t@I5@\tLiving"}},
		{"relatives", []string{"relatives", living, "@I3@"}, ExitOK, []string{"self\t\t@I3@\tLiving", "child\t@F2@\t@I5@\tLiving", "Bob /Example/"}},
		{"duplicates", []string{"duplicates", living, living}, ExitOK, []string{"@I3@\tLiving\t\t\ttrue", "Ann /Example/"}},
		// put, delete and media add report xrefs only.
		{"put", []string{"put", living, esme, "-o", out}, ExitOK, []string{"replaced\t@I5@"}},
		{"delete", []string{"delete", "--force", living, "@I5@", "-o", out}, ExitOK, []string{"deleted @I5@", "@F2@\tFAM.CHIL\t@I5@"}},
		// The fixture has no media, so there is nothing to report.
		{"media check", []string{"media", "check", "--root", "testdata", living}, ExitOK, nil},
		{"media add", []string{"media", "add", living, "--to", "@I3@", "--path", "photos/cleo.jpg", "-o", out}, ExitOK, []string{"@I3@\t@O1@\tcreated"}},
		// See redactLivingLeaks.
		{"convert", []string{"convert", living, "--to", "7.0", "-o", out}, ExitOK, []string{"5.5.1 -> 7.0"}},
	}
	covered := map[string]bool{}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args := append([]string{"--redact-living", "--reference-year", referenceYear}, tt.args...)
			code, stdout, stderr := run(t, args...)
			if code != tt.code {
				t.Fatalf("exit = %d, want %d, stderr = %q", code, tt.code, stderr)
			}
			leaks, leaked := redactLivingLeaks[tt.name], false
			for _, detail := range livingDetails {
				if slices.Contains(leaks, detail) {
					leaked = leaked || strings.Contains(stdout, detail)
					continue
				}
				if strings.Contains(stdout, detail) || strings.Contains(stderr, detail) {
					t.Errorf("output shows %q:\n%s%s", detail, stdout, stderr)
				}
			}
			if leaks != nil && !leaked {
				t.Errorf("output no longer shows any of %q: drop %q from redactLivingLeaks", leaks, tt.name)
			}
			for _, want := range tt.keep {
				if !strings.Contains(stdout, want) {
					t.Errorf("stdout lacks %q:\n%s", want, stdout)
				}
			}
		})
		cmd, _, err := newRootCmd().Find(tt.args)
		if err != nil {
			t.Fatal(err)
		}
		covered[cmd.CommandPath()] = true
	}
	for _, cmd := range allCommands(newRootCmd()) {
		if !cmd.HasSubCommands() && !covered[cmd.CommandPath()] {
			t.Errorf("command %q has no row in TestRedactLiving", cmd.CommandPath())
		}
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
