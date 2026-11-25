package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// retagGED is a 7.0 file with a vendor tag (_MILT) on two individuals, one of
// them with a TYPE already, on a family and nested under a PLAC, and with an
// _APID on a source record and on two citations.
const retagGED = "testdata/retag.ged"

// runRetag runs retag on retagGED, returning the exit code, the file written
// ("" if none) and stdout.
func runRetag(t *testing.T, args ...string) (code int, written, stdout string) {
	t.Helper()
	out := filepath.Join(t.TempDir(), "out.ged")
	code, stdout, _ = run(t, append(append([]string{"retag", retagGED}, args...), "-o", out)...)
	got, err := os.ReadFile(out)
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	return code, string(got), stdout
}

// TestRetagChangesOnlyThePath checks the whole output against the input: the
// lines at the path take the new tag and the --add lines, and every other
// line, a line with the same tag elsewhere included, is as read.
func TestRetagChangesOnlyThePath(t *testing.T) {
	input := readFixture(t, retagGED)
	tests := []struct {
		name string
		args []string
		want string
		text string
	}{
		{"rename", []string{"SOUR._APID", "REFN"},
			replaceOnce(t, input, "1 _APID 1,100::0\n", "1 REFN 1,100::0\n"),
			"1 line SOUR._APID renamed REFN, 0 lines added, 1 record changed\n"},
		{"rename and add", []string{"SOUR._APID", "EXID", "--add", "TYPE https://example.com/ids/"},
			replaceOnce(t, input, "1 _APID 1,100::0\n", "1 EXID 1,100::0\n2 TYPE https://example.com/ids/\n"),
			"1 line SOUR._APID renamed EXID, 1 line added, 1 record changed\n"},
		// The value, and the lines under the renamed line, stay; the added
		// lines come first, in the order given.
		{"several lines added", []string{"FAM._MILT", "EVEN", "--add", "TYPE Military Service", "--add", "NOTE"},
			replaceOnce(t, input, "1 WIFE @I1@\n1 _MILT\n", "1 WIFE @I1@\n1 EVEN\n2 TYPE Military Service\n2 NOTE\n"),
			"1 line FAM._MILT renamed EVEN, 2 lines added, 1 record changed\n"},
		{"deeper path", []string{"INDI._MILT.PLAC._MILT", "NOTE"},
			replaceOnce(t, input, "3 _MILT nested, not at the path\n", "3 NOTE nested, not at the path\n"),
			"1 line INDI._MILT.PLAC._MILT renamed NOTE, 0 lines added, 1 record changed\n"},
		{"every line at the path", []string{"INDI._MILT", "_SERV"},
			strings.NewReplacer("1 _MILT\n", "1 _SERV\n", "1 _MILT Sergeant\n", "1 _SERV Sergeant\n").Replace(
				replaceOnce(t, input, "1 WIFE @I1@\n1 _MILT\n", "1 WIFE @I1@\n1 FAMILY\n")),
			"3 lines INDI._MILT renamed _SERV, 0 lines added, 2 records changed\n"},
		{"no line at the path", []string{"INDI._NONE", "EVEN", "--add", "TYPE Other"}, input,
			"0 lines INDI._NONE renamed EVEN, 0 lines added, 0 records changed\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code, got, stdout := runRetag(t, tt.args...)
			if code != ExitOK {
				t.Fatalf("exit = %d, stdout = %q", code, stdout)
			}
			// The family's _MILT is not at INDI._MILT: the replacer above
			// would rename it, so that case marks it first.
			want := strings.Replace(tt.want, "1 FAMILY\n", "1 _MILT\n", 1)
			if got != want {
				t.Errorf("output:\n%s\nwant:\n%s", got, want)
			}
			if !strings.HasSuffix(stdout, tt.text) || !strings.HasPrefix(stdout, "wrote ") {
				t.Errorf("stdout = %q, want it to end %q", stdout, tt.text)
			}
		})
	}
}

// TestRetagOccupied checks that a structure that already has a line --add
// would add stops the write, and is reported with its xref and line.
func TestRetagOccupied(t *testing.T) {
	code, written, stdout := runRetag(t, "INDI._MILT", "EVEN", "--add", "TYPE Military Service")
	if code != ExitError {
		t.Fatalf("exit = %d, want %d", code, ExitError)
	}
	if written != "" {
		t.Errorf("a file was written:\n%s", written)
	}
	for _, want := range []string{"out.ged not written: 3 lines INDI._MILT renamed EVEN", "  has TYPE\t@I1@\tline 21\n"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("stdout lacks %q:\n%s", want, stdout)
		}
	}
}

// TestRetagAddedFindings checks that a tag the version does not permit where
// the line stands stops the write: 7.0 has no EXID under a citation.
func TestRetagAddedFindings(t *testing.T) {
	out := filepath.Join(t.TempDir(), "out.ged")
	code, stdout, stderr := run(t, "retag", "--format", "json", retagGED, "INDI.BIRT.SOUR._APID", "EXID", "-o", out)
	if code != ExitError || !strings.Contains(stderr, "the edit adds 1 finding") {
		t.Fatalf("exit = %d, stderr = %q", code, stderr)
	}
	if _, err := os.Stat(out); !os.IsNotExist(err) {
		t.Errorf("a file was written (stat: %v)", err)
	}
	var report retagReport
	if err := json.Unmarshal([]byte(stdout), &report); err != nil {
		t.Fatalf("stdout is not JSON: %v\n%s", err, stdout)
	}
	if report.Written || report.Renamed != 1 || report.Counts.Error != 1 || len(report.Findings) != 1 ||
		report.Findings[0].Code != "UNEXPECTED_SUBSTRUCTURE" || report.Findings[0].XRef != "@I1@" {
		t.Errorf("report = %+v", report)
	}
}

func TestRetagJSON(t *testing.T) {
	out := filepath.Join(t.TempDir(), "out.ged")
	code, stdout, stderr := run(t, "retag", "--format", "json", retagGED, "SOUR._APID", "EXID", "--add", "TYPE https://example.com/ids/", "-o", out)
	if code != ExitOK || stderr != "" {
		t.Fatalf("exit = %d, stderr = %q", code, stderr)
	}
	var report retagReport
	if err := json.Unmarshal([]byte(stdout), &report); err != nil {
		t.Fatalf("stdout is not JSON: %v\n%s", err, stdout)
	}
	if !report.Written || report.Path != "SOUR._APID" || report.Tag != "EXID" || report.Renamed != 1 || report.Added != 1 ||
		report.RecordsChanged != 1 || report.Occupied == nil || report.Findings == nil {
		t.Errorf("report = %+v", report)
	}
}

// TestRetagStdout checks that -o - puts the GEDCOM on stdout and the report
// on stderr.
func TestRetagStdout(t *testing.T) {
	code, stdout, stderr := run(t, "retag", retagGED, "SOUR._APID", "REFN", "-o", "-")
	if code != ExitOK {
		t.Fatalf("exit = %d, stderr = %q", code, stderr)
	}
	if !strings.Contains(stdout, "1 REFN 1,100::0\n") || !strings.HasPrefix(stderr, "wrote stdout: 1 line SOUR._APID renamed REFN") {
		t.Errorf("stdout = %q, stderr = %q", stdout, stderr)
	}
}

func TestRetagUsageErrors(t *testing.T) {
	for _, args := range [][]string{
		{"SOUR._APID"},                                         // no tag
		{"_APID", "EXID"},                                      // no record type
		{"SOUR.", "EXID"},                                      // empty tag in the path
		{"SOUR.apid", "EXID"},                                  // not a tag
		{"INDI.NOTE.CONT", "NOTE"},                             // a continuation line
		{"SOUR._APID", "exid"},                                 // not a tag
		{"SOUR._APID", "CONC"},                                 // a continuation line
		{"SOUR._APID", "EXID", "--add", "type x"},              // not a tag
		{"SOUR._APID", "EXID", "--add", "TYPE a\nb"},           // a line break
		{"SOUR._APID", "EXID", "--add", "A x", "--add", "A y"}, // the same tag twice
	} {
		code, written, _ := runRetag(t, args...)
		if code != ExitUsage || written != "" {
			t.Errorf("retag %q: exit = %d, want %d; written = %q", args, code, ExitUsage, written)
		}
	}
	if code, _, stderr := run(t, "retag", retagGED, "SOUR._APID", "EXID"); code != ExitUsage || !strings.Contains(stderr, "--output") {
		t.Errorf("no -o: exit = %d, stderr = %q", code, stderr)
	}
	if code, _, _ := run(t, "retag", retagGED, "SOUR._APID", "EXID", "-o", retagGED); code != ExitUsage {
		t.Errorf("-o naming the input: exit = %d, want %d", code, ExitUsage)
	}
}
