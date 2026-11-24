package cli

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

// families has a person (@I1@) with birth and adoptive parents, a sibling, a
// half-sibling through a parent's second marriage, and two marriages with a
// child in each. @I13@ has no family at all. @I14@ has a FAMS link to @F3@,
// which does not name her.
const families = "testdata/families.ged"

// relativesI1 is the text output for @I1@.
const relativesI1 = "self\t\t@I1@\tAnn /Example/\t4 APR 1920\t1 JAN 1990\t\n" +
	"parent\t@F1@\t@I2@\tBert /Example/\t1890\t\tbirth\n" +
	"parent\t@F1@\t@I3@\tClara /Sample/\t\t\tbirth\n" +
	"parent\t@F2@\t@I5@\tEarl /Placeholder/\t\t\tadopted\n" +
	"parent\t@F2@\t@I6@\tFaye /Placeholder/\t\t\tadopted\n" +
	"sibling\t@F1@\t@I4@\tDora /Example/\t1922\t\t\n" +
	"half-sibling\t@F5@\t@I8@\tHal /Example/\t1930\t\t\n" +
	"spouse\t@F3@\t@I9@\tIvan /Sample/\t\t\t\n" +
	"spouse\t@F4@\t@I11@\tKarl /Placeholder/\t\t\t\n" +
	"child\t@F3@\t@I10@\tJill /Sample/\t1945\t\tbirth\n" +
	"child\t@F4@\t@I12@\tLena /Placeholder/\t\t\tfoster\n"

func TestRelativesText(t *testing.T) {
	for _, xref := range []string{"@I1@", "I1"} {
		code, stdout, stderr := run(t, "relatives", families, xref)
		if code != ExitOK || stderr != "" {
			t.Fatalf("%s: exit = %d, stderr = %q", xref, code, stderr)
		}
		if stdout != relativesI1 {
			t.Errorf("%s: stdout =\n%s\nwant\n%s", xref, stdout, relativesI1)
		}
	}
}

func TestRelativesJSON(t *testing.T) {
	code, stdout, stderr := run(t, "relatives", "--format", "json", families, "@I1@")
	if code != ExitOK || stderr != "" {
		t.Fatalf("exit = %d, stderr = %q", code, stderr)
	}
	var got struct {
		Person    map[string]any   `json:"person"`
		Relatives []map[string]any `json:"relatives"`
	}
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("stdout is not a JSON object: %v\n%s", err, stdout)
	}
	wantPerson := map[string]any{"xref": "@I1@", "name": "Ann /Example/", "birth": "4 APR 1920", "death": "1 JAN 1990", "living": false}
	if !reflect.DeepEqual(got.Person, wantPerson) {
		t.Errorf("person = %v\nwant %v", got.Person, wantPerson)
	}
	// The JSON holds the same facts as the text, line for line.
	lines := strings.Split(strings.TrimSuffix(relativesI1, "\n"), "\n")[1:]
	if len(got.Relatives) != len(lines) {
		t.Fatalf("%d relatives, want %d:\n%s", len(got.Relatives), len(lines), stdout)
	}
	keys := []string{"relationship", "family", "xref", "name", "birth", "death", "pedigree"}
	for n, line := range lines {
		want := map[string]any{}
		for k, field := range strings.Split(line, "\t") {
			want[keys[k]] = field
		}
		if _, ok := got.Relatives[n]["living"].(bool); !ok {
			t.Errorf("relatives[%d] has no boolean living: %v", n, got.Relatives[n])
		}
		delete(got.Relatives[n], "living")
		if !reflect.DeepEqual(got.Relatives[n], want) {
			t.Errorf("relatives[%d] = %v\nwant %v", n, got.Relatives[n], want)
		}
	}
}

// TestRelativesSeveralFamilies checks that each family a person is in shows
// separately, through its own family xref and pedigree.
func TestRelativesSeveralFamilies(t *testing.T) {
	_, stdout, _ := run(t, "relatives", families, "@I1@")
	for _, want := range []string{
		"parent\t@F1@\t@I2@\tBert /Example/\t1890\t\tbirth\n",
		"parent\t@F2@\t@I5@\tEarl /Placeholder/\t\t\tadopted\n",
		"spouse\t@F3@\t@I9@",
		"spouse\t@F4@\t@I11@",
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("stdout missing %q:\n%s", want, stdout)
		}
	}
	// Hal is Ann's sibling through Bert's other family, not Ann's own.
	if !strings.Contains(stdout, "half-sibling\t@F5@\t@I8@") {
		t.Errorf("half-sibling not marked by the family they share:\n%s", stdout)
	}
}

// TestRelativesFromAnotherSide checks relationships read from a child's and a
// spouse's own record, not only the fixture's central person.
func TestRelativesFromAnotherSide(t *testing.T) {
	code, stdout, stderr := run(t, "relatives", families, "@I2@")
	if code != ExitOK || stderr != "" {
		t.Fatalf("exit = %d, stderr = %q", code, stderr)
	}
	want := "self\t\t@I2@\tBert /Example/\t1890\t\t\n" +
		"spouse\t@F1@\t@I3@\tClara /Sample/\t\t\t\n" +
		"spouse\t@F5@\t@I7@\tGwen /Sample/\t\t\t\n" +
		"child\t@F1@\t@I1@\tAnn /Example/\t4 APR 1920\t1 JAN 1990\tbirth\n" +
		"child\t@F1@\t@I4@\tDora /Example/\t1922\t\t\n" +
		"child\t@F5@\t@I8@\tHal /Example/\t1930\t\t\n"
	if stdout != want {
		t.Errorf("stdout =\n%s\nwant\n%s", stdout, want)
	}
}

func TestRelativesNone(t *testing.T) {
	code, stdout, stderr := run(t, "relatives", families, "@I13@")
	if code != ExitOK || stderr != "" || stdout != "self\t\t@I13@\tMoss /Example/\t\t\t\n" {
		t.Errorf("text: exit = %d, stderr = %q, stdout = %q", code, stderr, stdout)
	}
	code, stdout, stderr = run(t, "relatives", "--format", "json", families, "@I13@")
	if code != ExitOK || stderr != "" || !strings.Contains(stdout, `"relatives": []`) {
		t.Errorf("json: exit = %d, stderr = %q, stdout =\n%s", code, stderr, stdout)
	}
}

// TestRelativesOneSidedSpouseLink checks that a FAMS link the family does not
// return reports neither the family's spouses nor its children.
func TestRelativesOneSidedSpouseLink(t *testing.T) {
	code, stdout, stderr := run(t, "relatives", families, "@I14@")
	if code != ExitOK || stderr != "" || stdout != "self\t\t@I14@\tNell /Example/\t\t\t\n" {
		t.Errorf("exit = %d, stderr = %q, stdout = %q", code, stderr, stdout)
	}
}

func TestRelativesErrors(t *testing.T) {
	tests := []struct {
		name string
		args []string
		code int
		msg  string
	}{
		{"unknown xref", []string{families, "@I99@"}, ExitError, "no such record: @I99@"},
		{"not an individual", []string{families, "F1"}, ExitError, "@F1@ is a FAM record, not an individual"},
		{"missing xref", []string{families}, ExitUsage, "accepts 2 arg(s)"},
		{"too many xrefs", []string{families, "I1", "I2"}, ExitUsage, "accepts 2 arg(s)"},
		{"missing file", []string{"testdata/does-not-exist.ged", "I1"}, ExitError, "does-not-exist.ged"},
	}
	for _, tt := range tests {
		for _, format := range []string{formatText, formatJSON} {
			t.Run(tt.name+"/"+format, func(t *testing.T) {
				args := append([]string{"relatives", "--format", format}, tt.args...)
				code, stdout, stderr := run(t, args...)
				if code != tt.code {
					t.Errorf("exit = %d, want %d", code, tt.code)
				}
				if stdout != "" {
					t.Errorf("stdout = %q, want nothing", stdout)
				}
				if !strings.Contains(stderr, tt.msg) {
					t.Errorf("stderr = %q, want it to contain %q", stderr, tt.msg)
				}
			})
		}
	}
}

// TestRelativesRedactLiving checks that --redact-living hides a presumed-living
// relative's name while keeping the xref.
func TestRelativesRedactLiving(t *testing.T) {
	code, stdout, stderr := run(t, "relatives", "--redact-living", families, "@I1@")
	if code != ExitOK || stderr != "" {
		t.Fatalf("exit = %d, stderr = %q", code, stderr)
	}
	if strings.Contains(stdout, "Clara") {
		t.Errorf("a presumed-living relative's name is printed:\n%s", stdout)
	}
	if !strings.Contains(stdout, "@I3@") {
		t.Errorf("the presumed-living relative's xref is missing:\n%s", stdout)
	}
	if !strings.Contains(stdout, "Ann /Example/") {
		t.Errorf("a deceased person's name is hidden:\n%s", stdout)
	}
}
