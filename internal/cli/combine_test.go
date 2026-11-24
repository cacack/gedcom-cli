package cli

import (
	"encoding/json"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/cacack/gedcom-go/v3/encoder"
)

// combineFixture is a master of three families over three generations: Cal
// (@I3@), son of @F1@, and Dee (@I4@), daughter of @F2@, are the couple of
// @F3@, with children Gil and Hope.
const combineFixture = "testdata/combine.ged"

// combineBranches is the master cut in two, the paternal and the maternal
// side, as 'gedcom extract' cuts it. Every individual is in one branch, so the
// two zip back into the master. @F3@ is in both, each copy with only its own
// side's spouse and child.
var combineBranches = [][]string{
	{"@I1@", "@I2@", "@I3@", "@I7@"},
	{"@I4@", "@I5@", "@I6@", "@I8@"},
}

// writeBranches writes the bounded subset of each set of individuals of
// master to its own file, and returns the files. It stands in for 'gedcom
// extract', building the subsets with the library call extract makes.
func writeBranches(t *testing.T, master string, sets [][]string) []string {
	t.Helper()
	res, err := decodeForWrite(master)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	var files []string
	for i, set := range sets {
		branch, _, err := res.Document.BoundedSubset(set)
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(dir, "branch"+string(rune('a'+i))+".ged")
		f, err := os.Create(path)
		if err != nil {
			t.Fatal(err)
		}
		if err := encoder.Encode(f, branch); err != nil {
			t.Fatal(err)
		}
		if err := f.Close(); err != nil {
			t.Fatal(err)
		}
		files = append(files, path)
	}
	return files
}

// gedcomRecords splits a GEDCOM file's text into its records, keyed by their
// level-0 line, each the lines it holds.
func gedcomRecords(t *testing.T, path string) map[string][]string {
	t.Helper()
	records := make(map[string][]string)
	var key string
	for _, line := range strings.Split(strings.TrimSuffix(readText(t, path), "\n"), "\n") {
		if strings.HasPrefix(line, "0 ") {
			key = line
		}
		records[key] = append(records[key], line)
	}
	return records
}

func readText(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// TestCombineZipRoundTrip cuts the master into branches and zips them back:
// the output holds every record of the master, line for line.
func TestCombineZipRoundTrip(t *testing.T) {
	branches := writeBranches(t, combineFixture, combineBranches)
	for i, branch := range branches {
		if reflect.DeepEqual(gedcomRecords(t, branch), gedcomRecords(t, combineFixture)) {
			t.Fatalf("branch %d is the whole master; the test cuts nothing", i)
		}
	}
	out := filepath.Join(t.TempDir(), "tree.ged")
	code, stdout, stderr := run(t, append(append([]string{"combine", "--strategy", "zip"}, branches...), "-o", out)...)
	if code != ExitOK || stderr != "" {
		t.Fatalf("exit = %d, stderr = %q", code, stderr)
	}
	if want := "wrote " + out + ": 15 records, 8 individuals, 3 families\n"; stdout != want {
		t.Errorf("stdout = %q, want %q", stdout, want)
	}

	got, want := gedcomRecords(t, out), gedcomRecords(t, combineFixture)
	if !reflect.DeepEqual(slices.Sorted(maps.Keys(got)), slices.Sorted(maps.Keys(want))) {
		t.Fatalf("records = %v, want %v", slices.Sorted(maps.Keys(got)), slices.Sorted(maps.Keys(want)))
	}
	for key, lines := range want {
		if key == "0 @F3@ FAM" {
			// The family the cut split: each branch's file held half its
			// pointer lines, numbered by that file, so the library joins
			// them in a file's line order, not the master's (see
			// merge.ZipSharedXRefs). The lines are all back.
			lines, got[key] = slices.Sorted(slices.Values(lines)), slices.Sorted(slices.Values(got[key]))
		}
		if !reflect.DeepEqual(got[key], lines) {
			t.Errorf("%s:\n got %q\nwant %q", key, got[key], lines)
		}
	}

	code, stdout, stderr = run(t, "validate", out)
	if code != ExitOK || stderr != "" || !strings.HasSuffix(stdout, ": no findings\n") {
		t.Errorf("validate: exit = %d, stdout = %q, stderr = %q", code, stdout, stderr)
	}
}

func TestCombineZipJSON(t *testing.T) {
	branches := writeBranches(t, combineFixture, combineBranches)
	out := filepath.Join(t.TempDir(), "tree.ged")
	code, stdout, stderr := run(t, append(append([]string{"combine", "--format", "json", "--strategy", "zip"}, branches...), "-o", out)...)
	if code != ExitOK || stderr != "" {
		t.Fatalf("exit = %d, stderr = %q", code, stderr)
	}
	var got combineReport
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("stdout is not JSON: %v\n%s", err, stdout)
	}
	want := combineReport{
		Output: out, Strategy: "zip", Records: 15, Individuals: 8, Families: 3,
		Inputs: []combineInput{
			{File: branches[0], Encoding: "UTF-8", Remapped: map[string]string{}},
			{File: branches[1], Encoding: "UTF-8", Remapped: map[string]string{}},
		},
		HeaderConflicts: []combineHeaderConflict{},
		Conflicts:       []combineConflict{},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("report = %+v\nwant %+v", got, want)
	}
}

// conflicting is the minimal fixture with Ada renamed and her family married:
// zipped with it, @I1@ and @F1@ differ in more than pointers.
const conflicting = `0 HEAD
1 SOUR gedcom-cli-test
1 GEDC
2 VERS 5.5.1
2 FORM LINEAGE-LINKED
1 CHAR UTF-8
1 SUBM @U1@
0 @U1@ SUBM
1 NAME Test Submitter
0 @I1@ INDI
1 NAME Ada /Sample/
1 SEX F
1 FAMS @F1@
0 @F1@ FAM
1 HUSB @I2@
1 WIFE @I1@
1 MARR
2 DATE 1900
0 TRLR
`

func TestCombineZipConflict(t *testing.T) {
	other := writeTemp(t, "other.ged", conflicting)
	out := filepath.Join(t.TempDir(), "tree.ged")

	code, stdout, stderr := run(t, "combine", "--strategy", "zip", sample, other, "-o", out)
	if code != ExitError {
		t.Fatalf("exit = %d, want %d, stderr = %q", code, ExitError, stderr)
	}
	if want := other + "\t@I1@\tconflict\tlines\n" + other + "\t@F1@\tconflict\tlines\n"; stdout != want {
		t.Errorf("stdout = %q, want %q", stdout, want)
	}
	if want := "gedcom: 2 records conflict, nothing written: @I1@, @F1@\n"; stderr != want {
		t.Errorf("stderr = %q, want %q", stderr, want)
	}
	if _, err := os.Stat(out); !os.IsNotExist(err) {
		t.Errorf("output was written: %v", err)
	}

	code, stdout, _ = run(t, "combine", "--format", "json", "--strategy", "zip", sample, other, "-o", out)
	if code != ExitError {
		t.Fatalf("json: exit = %d, want %d", code, ExitError)
	}
	var got combineReport
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("stdout is not JSON: %v\n%s", err, stdout)
	}
	want := []combineConflict{{File: other, XRef: "@I1@", Reason: "lines"}, {File: other, XRef: "@F1@", Reason: "lines"}}
	if got.Output != "" || !reflect.DeepEqual(got.Conflicts, want) {
		t.Errorf("output = %q, conflicts = %+v, want none written and %+v", got.Output, got.Conflicts, want)
	}
	if _, err := os.Stat(out); !os.IsNotExist(err) {
		t.Errorf("output was written: %v", err)
	}
}

func TestCombinePrefix(t *testing.T) {
	out := filepath.Join(t.TempDir(), "all.ged")
	code, stdout, stderr := run(t, "combine", "--strategy", "prefix", "--prefix", "B_,C_", sample, sample, combineFixture, "-o", out)
	if code != ExitOK || stderr != "" {
		t.Fatalf("exit = %d, stderr = %q", code, stderr)
	}
	// minimal holds 4 records, combine 15; every one is kept.
	if want := "wrote " + out + ": 23 records, 12 individuals, 5 families\n"; !strings.HasPrefix(stdout, want) {
		t.Errorf("stdout = %q, want it to start %q", stdout, want)
	}
	for _, want := range []string{sample + "\t@I1@\t@B_I1@\n", sample + "\t@F1@\t@B_F1@\n", sample + "\t@U1@\t@B_U1@\n", combineFixture + "\t@I1@\t@C_I1@\n"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("stdout lacks %q:\n%s", want, stdout)
		}
	}
	// @F3@ collides with nothing, so it keeps its xref.
	if strings.Contains(stdout, combineFixture+"\t@F3@") {
		t.Errorf("stdout renames @F3@:\n%s", stdout)
	}
	records := gedcomRecords(t, out)
	for _, want := range []string{"0 @I1@ INDI", "0 @B_I1@ INDI", "0 @C_I1@ INDI", "0 @F3@ FAM", "0 @B_F1@ FAM", "0 @C_F1@ FAM"} {
		if _, ok := records[want]; !ok {
			t.Errorf("output lacks %q", want)
		}
	}
	// @I3@ collides with nothing either, but points to @F1@, which did.
	if got := records["0 @I3@ INDI"]; !slices.Contains(got, "1 FAMC @C_F1@") || !slices.Contains(got, "1 FAMS @F3@") {
		t.Errorf("@I3@ = %q: its pointers were not remapped", got)
	}

	code, stdout, stderr = run(t, "combine", "--format", "json", "--strategy", "prefix", "--prefix", "B_", sample, sample, "-o", out)
	if code != ExitOK || stderr != "" {
		t.Fatalf("json: exit = %d, stderr = %q", code, stderr)
	}
	var got combineReport
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("stdout is not JSON: %v\n%s", err, stdout)
	}
	want := []combineInput{
		{File: sample, Encoding: "UTF-8", Remapped: map[string]string{}},
		{File: sample, Encoding: "UTF-8", Remapped: map[string]string{"@U1@": "@B_U1@", "@I1@": "@B_I1@", "@I2@": "@B_I2@", "@F1@": "@B_F1@"}},
	}
	if got.Records != 8 || !reflect.DeepEqual(got.Inputs, want) {
		t.Errorf("records = %d, inputs = %+v\nwant 8 and %+v", got.Records, got.Inputs, want)
	}
}

func TestCombineRenumber(t *testing.T) {
	out := filepath.Join(t.TempDir(), "both.ged")
	code, stdout, stderr := run(t, "--format", "json", "combine", "--strategy", "renumber", sample, sample, "-o", out)
	if code != ExitOK || stderr != "" {
		t.Fatalf("exit = %d, stderr = %q", code, stderr)
	}
	var got combineReport
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("stdout is not JSON: %v\n%s", err, stdout)
	}
	want := map[string]string{"@U1@": "@U2@", "@I1@": "@I3@", "@I2@": "@I4@", "@F1@": "@F2@"}
	if got.Records != 8 || !reflect.DeepEqual(got.Inputs[1].Remapped, want) {
		t.Errorf("records = %d, remapped = %v, want 8 and %v", got.Records, got.Inputs[1].Remapped, want)
	}
	if lines := gedcomRecords(t, out)["0 @F2@ FAM"]; !reflect.DeepEqual(lines, []string{"0 @F2@ FAM", "1 HUSB @I4@", "1 WIFE @I3@"}) {
		t.Errorf("@F2@ = %q", lines)
	}
}

// anselFile declares ANSEL and spells René with ANSEL's combining acute (0xE2)
// before the e, which decodes to e and U+0301. Its header source differs
// from minimal's.
const anselFile = "0 HEAD\n1 SOUR other-test\n1 GEDC\n2 VERS 5.5.1\n2 FORM LINEAGE-LINKED\n1 CHAR ANSEL\n" +
	"0 @I1@ INDI\n1 NAME Ren\xe2e /Sample/\n0 TRLR\n"

func TestCombineEncodings(t *testing.T) {
	ansel := writeTemp(t, "ansel.ged", anselFile)
	out := filepath.Join(t.TempDir(), "both.ged")
	code, stdout, stderr := run(t, "combine", "--format", "json", "--strategy", "prefix", "--prefix", "A_", sample, ansel, "-o", out)
	if code != ExitOK || stderr != "" {
		t.Fatalf("exit = %d, stderr = %q", code, stderr)
	}
	var got combineReport
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("stdout is not JSON: %v\n%s", err, stdout)
	}
	if got.Inputs[0].Encoding != "UTF-8" || got.Inputs[1].Encoding != "ANSEL" {
		t.Errorf("inputs = %+v, want UTF-8 and ANSEL", got.Inputs)
	}
	if want := (combineHeaderConflict{File: ansel, Field: "SourceSystem", Kept: "gedcom-cli-test", Dropped: "other-test"}); !slices.Contains(got.HeaderConflicts, want) {
		t.Errorf("header conflicts = %+v, want %+v", got.HeaderConflicts, want)
	}
	text := readText(t, out)
	for _, want := range []string{"1 CHAR UTF-8\n", "1 NAME Rene\u0301 /Sample/\n"} {
		if !strings.Contains(text, want) {
			t.Errorf("output lacks %q:\n%s", want, text)
		}
	}

	out = filepath.Join(t.TempDir(), "text.ged")
	code, _, stderr = run(t, "combine", "--strategy", "prefix", "--prefix", "A_", sample, ansel, "-o", out)
	if want := "gedcom: warning: 1 differing header value from later files dropped (see --format json)\n"; code != ExitOK || stderr != want {
		t.Errorf("text: exit = %d, stderr = %q, want %q", code, stderr, want)
	}

	v7 := writeTemp(t, "v7.ged", "0 HEAD\n1 GEDC\n2 VERS 7.0\n0 @I9@ INDI\n0 TRLR\n")
	out = filepath.Join(t.TempDir(), "mixed.ged")
	code, _, stderr = run(t, "combine", "--strategy", "zip", sample, v7, "-o", out)
	if code != ExitError || !strings.Contains(stderr, "incompatible versions: 5.5.1 vs 7.0") {
		t.Errorf("mixed versions: exit = %d, stderr = %q", code, stderr)
	}
	if _, err := os.Stat(out); !os.IsNotExist(err) {
		t.Errorf("output was written: %v", err)
	}
}

func TestCombineRedactLiving(t *testing.T) {
	out := filepath.Join(t.TempDir(), "share.ged")
	code, stdout, stderr := run(t, "combine", "--redact-living", "--reference-year", referenceYear, "--strategy", "zip", living, living, "-o", out)
	if code != ExitOK {
		t.Fatalf("exit = %d, stderr = %q", code, stderr)
	}
	text := readText(t, out)
	for _, detail := range livingDetails {
		if strings.Contains(text+stdout+stderr, detail) {
			t.Errorf("output shows %q:\n%s", detail, text)
		}
	}
	for _, want := range []string{"0 @I3@ INDI\n1 NAME Living\n", "Ann /Example/"} {
		if !strings.Contains(text, want) {
			t.Errorf("output lacks %q:\n%s", want, text)
		}
	}
}

func TestCombineErrors(t *testing.T) {
	out := filepath.Join(t.TempDir(), "out.ged")
	input := writeTemp(t, "in.ged", readText(t, sample))
	tests := []struct {
		name string
		args []string
		code int
		want string
	}{
		{"no files", []string{"combine", "--strategy", "zip", "-o", out}, ExitUsage, "requires at least 1 arg"},
		{"no strategy", []string{"combine", sample, sample, "-o", out}, ExitUsage, `invalid --strategy ""`},
		{"bad strategy", []string{"combine", "--strategy", "merge", sample, sample, "-o", out}, ExitUsage, `invalid --strategy "merge"`},
		{"no output", []string{"combine", "--strategy", "zip", sample, sample}, ExitUsage, "--output is required"},
		{"output is input", []string{"combine", "--strategy", "zip", sample, input, "-o", input}, ExitUsage, "is the input file"},
		{"no prefix", []string{"combine", "--strategy", "prefix", sample, sample, "-o", out}, ExitUsage, "got 0, want 1"},
		{"too many prefixes", []string{"combine", "--strategy", "prefix", "--prefix", "B_,C_", sample, sample, "-o", out}, ExitUsage, "got 2, want 1"},
		{"prefix without prefix strategy", []string{"combine", "--strategy", "zip", "--prefix", "B_", sample, sample, "-o", out}, ExitUsage, "--prefix needs --strategy prefix"},
		{"missing file", []string{"combine", "--strategy", "zip", sample, "testdata/nope.ged", "-o", out}, ExitError, "nope.ged"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code, stdout, stderr := run(t, tt.args...)
			if code != tt.code || stdout != "" || !strings.Contains(stderr, tt.want) {
				t.Errorf("exit = %d, stdout = %q, stderr = %q; want exit %d and %q", code, stdout, stderr, tt.code, tt.want)
			}
			if _, err := os.Stat(out); !os.IsNotExist(err) {
				t.Errorf("output was written: %v", err)
			}
		})
	}
}

func TestCombineHelp(t *testing.T) {
	_, stdout, _ := run(t, "combine", "--help")
	for _, want := range []string{
		"--strategy zip -o", "--strategy prefix --prefix C_", "--strategy renumber -o",
		"must share a GEDCOM version", "the output is always UTF-8",
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("help lacks %q:\n%s", want, stdout)
		}
	}
}
