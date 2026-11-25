package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// repeatedGED holds an individual with one birth written twice ("7 DEC
// 1977", "07 Dec 1977"), the second with a citation and a note, and two
// residences with no DATE; one with three copies of a death; one with two
// births on different dates; and a family with one marriage written twice.
const repeatedGED = "testdata/repeated.ged"

const (
	repeatedBirth = "repeated\t@I1@\tBIRT\t1 BIRT | 2 DATE 7 DEC 1977 | 2 PLAC Exampletown, Example County\t" +
		"1 BIRT | 2 DATE 07 Dec 1977 | 2 PLAC Exampletown, Example County | 2 SOUR @S1@ | 3 PAGE p. 4 | 2 NOTE Copied from the register\n"
	repeatedDeath    = "repeated\t@I2@\tDEAT\t1 DEAT | 2 DATE 1 JAN 2020\t1 DEAT | 2 DATE 01 Jan 2020 | 2 PLAC Exampleville\t1 DEAT | 2 DATE 1 JAN 2020 | 2 SOUR @S1@\n"
	repeatedMarriage = "repeated\t@F1@\tMARR\t1 MARR | 2 DATE 12 JUN 2000 | 2 PLAC Exampletown Chapel\t1 MARR | 2 DATE 12 JUN 2000 | 2 SOUR @S1@\n"
)

// TestRepeatedEventsReport checks that the report lists each record's
// repeated events, individuals and families, and leaves out a record with one
// birth, births on different dates and residences with no DATE.
func TestRepeatedEventsReport(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{"every record", nil, repeatedBirth + repeatedDeath + repeatedMarriage},
		{"records named", []string{"F1", "@I1@"}, repeatedBirth + repeatedMarriage},
		{"nothing repeated", []string{"@I3@"}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code, stdout, stderr := run(t, append([]string{"repeated-events", repeatedGED}, tt.args...)...)
			if code != ExitOK || stderr != "" {
				t.Fatalf("exit = %d, stderr = %q", code, stderr)
			}
			if stdout != tt.want {
				t.Errorf("stdout:\n%s\nwant:\n%s", stdout, tt.want)
			}
		})
	}

	code, stdout, stderr := run(t, "repeated-events", "--format", "json", repeatedGED, "@F1@")
	if code != ExitOK || stderr != "" {
		t.Fatalf("json: exit = %d, stderr = %q", code, stderr)
	}
	var got []repeatedGroup
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("stdout is not JSON: %v\n%s", err, stdout)
	}
	want := []repeatedGroup{{XRef: "@F1@", Tag: "MARR", Members: [][]string{
		{"1 MARR", "2 DATE 12 JUN 2000", "2 PLAC Exampletown Chapel"},
		{"1 MARR", "2 DATE 12 JUN 2000", "2 SOUR @S1@"},
	}}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("json = %+v, want %+v", got, want)
	}

	if _, stdout, _ = run(t, "repeated-events", "--format", "json", repeatedGED, "@I3@"); stdout != "[]\n" {
		t.Errorf("json with nothing repeated = %q, want []", stdout)
	}
}

// runFold runs repeated-events --fold on in, returning the file written and
// stdout.
func runFold(t *testing.T, in string, args ...string) (written, stdout string) {
	t.Helper()
	out := filepath.Join(t.TempDir(), "out.ged")
	code, stdout, stderr := run(t, append(append([]string{"repeated-events", "--fold", in}, args...), "-o", out)...)
	if code != ExitOK || stderr != "" {
		t.Fatalf("exit = %d, stderr = %q", code, stderr)
	}
	got, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	return string(got), stdout
}

// TestRepeatedEventsFold checks the whole file --fold writes: each group is
// its first member plus the lines of the others it lacked, and every other
// line, the residences and the other records included, is as read.
func TestRepeatedEventsFold(t *testing.T) {
	input := readFixture(t, repeatedGED)
	want := replaceOnce(t, input, "2 PLAC Exampletown, Example County\n1 RESI\n",
		"2 PLAC Exampletown, Example County\n2 SOUR @S1@\n3 PAGE p. 4\n2 NOTE Copied from the register\n1 RESI\n")
	want = replaceOnce(t, want, "1 BIRT\n2 DATE 07 Dec 1977\n2 PLAC Exampletown, Example County\n2 SOUR @S1@\n3 PAGE p. 4\n2 NOTE Copied from the register\n", "")
	want = replaceOnce(t, want, "1 DEAT\n2 DATE 1 JAN 2020\n1 DEAT\n2 DATE 01 Jan 2020\n2 PLAC Exampleville\n1 DEAT\n2 DATE 1 JAN 2020\n2 SOUR @S1@\n",
		"1 DEAT\n2 DATE 1 JAN 2020\n2 PLAC Exampleville\n2 SOUR @S1@\n")
	want = replaceOnce(t, want, "2 PLAC Exampletown Chapel\n1 MARR\n2 DATE 12 JUN 2000\n2 SOUR @S1@\n",
		"2 PLAC Exampletown Chapel\n2 SOUR @S1@\n")

	got, stdout := runFold(t, repeatedGED)
	if got != want {
		t.Errorf("output:\n%s\nwant:\n%s", got, want)
	}
	// No citation or note line of the input is lost.
	for _, line := range strings.Split(input, "\n") {
		if strings.Contains(line, "SOUR @S1@") || strings.Contains(line, "PAGE") || strings.Contains(line, "NOTE") {
			if !strings.Contains(got, line+"\n") {
				t.Errorf("output lacks %q", line)
			}
		}
	}

	// One folded line per substructure removed, naming the record.
	removed := strings.Count(input, "\n1 ") - strings.Count(got, "\n1 ")
	lines := strings.Split(strings.TrimSuffix(stdout, "\n"), "\n")
	if len(lines) != removed || removed != 4 {
		t.Fatalf("%d folded lines for %d substructures removed:\n%s", len(lines), removed, stdout)
	}
	for i, prefix := range []string{"folded\t@I1@\tBIRT\t", "folded\t@I2@\tDEAT\t", "folded\t@I2@\tDEAT\t", "folded\t@F1@\tMARR\t"} {
		if !strings.HasPrefix(lines[i], prefix) {
			t.Errorf("line %d = %q, want it to start %q", i+1, lines[i], prefix)
		}
	}
	wantMarriage := "folded\t@F1@\tMARR\t1 MARR | 2 DATE 12 JUN 2000 | 2 SOUR @S1@\t1 MARR | 2 DATE 12 JUN 2000 | 2 PLAC Exampletown Chapel | 2 SOUR @S1@"
	if lines[3] != wantMarriage {
		t.Errorf("line 4 = %q, want %q", lines[3], wantMarriage)
	}

	// Folding the output again folds nothing, and the report lists nothing.
	folded := writeTemp(t, "folded.ged", got)
	again, stdout := runFold(t, folded)
	if again != got || stdout != "" {
		t.Errorf("second fold changed the file or reported %q", stdout)
	}
	if _, stdout, _ = run(t, "repeated-events", folded); stdout != "" {
		t.Errorf("report on the output = %q, want nothing", stdout)
	}
}

func TestRepeatedEventsFoldJSON(t *testing.T) {
	_, stdout := runFold(t, repeatedGED, "--format", "json", "@F1@")
	var got repeatedFoldReport
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("stdout is not JSON: %v\n%s", err, stdout)
	}
	want := repeatedFoldReport{Folded: []repeatedFold{{XRef: "@F1@", mergedFold: mergedFold{
		Tag:   "MARR",
		Lines: []string{"1 MARR", "2 DATE 12 JUN 2000", "2 SOUR @S1@"},
		Into:  []string{"1 MARR", "2 DATE 12 JUN 2000", "2 PLAC Exampletown Chapel", "2 SOUR @S1@"},
	}}}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("json = %+v, want %+v", got, want)
	}
}

// TestRepeatedEventsFoldStdout checks that with -o - the GEDCOM goes to
// stdout and the report to stderr.
func TestRepeatedEventsFoldStdout(t *testing.T) {
	code, stdout, stderr := run(t, "repeated-events", "--fold", repeatedGED, "@F1@", "-o", "-")
	if code != ExitOK {
		t.Fatalf("exit = %d, stderr = %q", code, stderr)
	}
	if !strings.HasPrefix(stdout, "0 HEAD\n") || !strings.HasPrefix(stderr, "folded\t@F1@\tMARR\t") {
		t.Errorf("stdout = %q, stderr = %q", stdout, stderr)
	}
}

func TestRepeatedEventsErrors(t *testing.T) {
	out := filepath.Join(t.TempDir(), "out.ged")
	tests := []struct {
		name string
		args []string
		code int
		want string
	}{
		{"output without fold", []string{repeatedGED, "-o", out}, ExitUsage, "add --fold"},
		{"fold without output", []string{"--fold", repeatedGED}, ExitUsage, "--output (-o) is required"},
		{"output is the input", []string{"--fold", repeatedGED, "-o", repeatedGED}, ExitUsage, "is the input file"},
		{"no such record", []string{repeatedGED, "@I9@"}, ExitError, "no such record: @I9@"},
		{"not an individual or family", []string{repeatedGED, "@S1@"}, ExitError, "@S1@ is a SOUR record; want an individual or a family"},
		{"no file", nil, ExitUsage, "requires at least 1 arg"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code, stdout, stderr := run(t, append([]string{"repeated-events"}, tt.args...)...)
			if code != tt.code || stdout != "" || !strings.Contains(stderr, tt.want) {
				t.Errorf("exit = %d, stdout = %q, stderr = %q; want exit %d and %q", code, stdout, stderr, tt.code, tt.want)
			}
		})
	}
	if _, err := os.Stat(out); !os.IsNotExist(err) {
		t.Errorf("an error wrote %s", out)
	}
}
