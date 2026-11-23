package cli

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/cacack/gedcom-go/v3/gedcom"
)

// lineage is a synthetic tree with pedigree collapse: Ann's parents Bert and
// Cleo are cousins, so Hal and Ida are her great-grandparents twice over.
const lineage = "testdata/lineage.ged"

const (
	ancestorsOfI1 = "1\t@I2@\tBert /Example/\t1920\t1990\n" +
		"1\t@I3@\tCleo /Sample/\t\t\n" +
		"2\t@I4@\tDan /Example/\t\t\n" +
		"2\t@I5@\tEva /Other/\t\t\n" +
		"2\t@I7@\tGus /Sample/\t\t\n" +
		"2\t@I6@\tFaye /Example/\t\t\n" +
		"3\t@I8@\tHal /Example/\t1850\t\n" +
		"3\t@I9@\tIda /Example/\t\t\n"
	descendantsOfI8 = "1\t@I4@\tDan /Example/\t\t\n" +
		"1\t@I6@\tFaye /Example/\t\t\n" +
		"2\t@I2@\tBert /Example/\t1920\t1990\n" +
		"2\t@I3@\tCleo /Sample/\t\t\n" +
		"3\t@I1@\tAnn /Example/\t1950\t\n"
)

// firstLines is the first n lines of text.
func firstLines(text string, n int) string {
	lines := strings.SplitAfter(text, "\n")
	return strings.Join(lines[:n], "")
}

func TestWalkText(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{"ancestors, pedigree collapse listed once", []string{"ancestors", lineage, "@I1@"}, ancestorsOfI1},
		{"ancestors, bare xref", []string{"ancestors", lineage, "I1"}, ancestorsOfI1},
		{"ancestors, generations 0 is all", []string{"ancestors", "--generations", "0", lineage, "@I1@"}, ancestorsOfI1},
		{"ancestors, generations 1", []string{"ancestors", "--generations", "1", lineage, "@I1@"}, firstLines(ancestorsOfI1, 2)},
		{"ancestors, generations 2", []string{"ancestors", "--generations", "2", lineage, "@I1@"}, firstLines(ancestorsOfI1, 6)},
		{"ancestors, none", []string{"ancestors", lineage, "@I8@"}, ""},
		{"descendants, pedigree collapse listed once", []string{"descendants", lineage, "@I8@"}, descendantsOfI8},
		{"descendants, generations 2", []string{"descendants", "--generations", "2", lineage, "@I8@"}, firstLines(descendantsOfI8, 4)},
		{"descendants, none", []string{"descendants", lineage, "@I1@"}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code, stdout, stderr := run(t, tt.args...)
			if code != ExitOK || stderr != "" {
				t.Fatalf("exit = %d, stderr = %q", code, stderr)
			}
			if stdout != tt.want {
				t.Errorf("stdout =\n%s\nwant\n%s", stdout, tt.want)
			}
		})
	}
}

func TestWalkJSON(t *testing.T) {
	code, stdout, stderr := run(t, "descendants", "--format", "json", lineage, "@I8@")
	if code != ExitOK || stderr != "" {
		t.Fatalf("exit = %d, stderr = %q", code, stderr)
	}
	var got []map[string]any
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("stdout is not JSON: %v\n%s", err, stdout)
	}
	want := []map[string]any{
		{"generation": float64(1), "xref": "@I4@", "name": "Dan /Example/", "birth": "", "death": ""},
		{"generation": float64(1), "xref": "@I6@", "name": "Faye /Example/", "birth": "", "death": ""},
		{"generation": float64(2), "xref": "@I2@", "name": "Bert /Example/", "birth": "1920", "death": "1990"},
		{"generation": float64(2), "xref": "@I3@", "name": "Cleo /Sample/", "birth": "", "death": ""},
		{"generation": float64(3), "xref": "@I1@", "name": "Ann /Example/", "birth": "1950", "death": ""},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v\nwant %v", got, want)
	}
}

// TestWalkJSONEmpty checks that no relatives is an empty array, not null.
func TestWalkJSONEmpty(t *testing.T) {
	code, stdout, stderr := run(t, "ancestors", "--format", "json", lineage, "@I8@")
	if code != ExitOK || stderr != "" {
		t.Fatalf("exit = %d, stderr = %q", code, stderr)
	}
	if strings.TrimSpace(stdout) != "[]" {
		t.Errorf("stdout = %q, want []", stdout)
	}
}

// TestRelativeRowsSkipsUnresolved checks that a walk result naming no
// individual is skipped, not dereferenced.
func TestRelativeRowsSkipsUnresolved(t *testing.T) {
	doc, err := decodeFile(lineage)
	if err != nil {
		t.Fatal(err)
	}
	rows := relativeRows(doc, []gedcom.Relative{{XRef: "@I99@", Generation: 1}, {XRef: "@I2@", Generation: 1}})
	if len(rows) != 1 || rows[0].(relativeRow).XRef != "@I2@" {
		t.Errorf("rows = %+v, want only @I2@", rows)
	}
}

func TestWalkErrors(t *testing.T) {
	tests := []struct {
		name   string
		args   []string
		code   int
		stderr string
	}{
		{"unknown xref", []string{"ancestors", lineage, "@I99@"}, ExitError, "no such record: @I99@"},
		{"unknown bare xref", []string{"descendants", lineage, "I99"}, ExitError, "no such record: @I99@"},
		{"not an individual", []string{"descendants", lineage, "@F1@"}, ExitError, "@F1@ is not an individual"},
		{"negative generations", []string{"ancestors", "--generations", "-1", lineage, "@I1@"}, ExitUsage, "invalid --generations -1"},
		{"missing xref", []string{"ancestors", lineage}, ExitUsage, "accepts 2 arg(s)"},
		{"missing file", []string{"descendants", "testdata/does-not-exist.ged", "@I1@"}, ExitError, "does-not-exist.ged"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code, stdout, stderr := run(t, tt.args...)
			if code != tt.code {
				t.Errorf("exit = %d, want %d (stderr %q)", code, tt.code, stderr)
			}
			if stdout != "" {
				t.Errorf("stdout = %q, want nothing on failure", stdout)
			}
			if !strings.Contains(stderr, tt.stderr) {
				t.Errorf("stderr = %q, want it to contain %q", stderr, tt.stderr)
			}
		})
	}
}
