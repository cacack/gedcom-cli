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
	code, stdout, stderr := run(t, append(append([]string{"combine", "--strategy", "zip", "--keep-header"}, branches...), "-o", out)...)
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

// zipOrderFixture is a master whose family @F1@ has three children in birth
// order, Quin (@I3@), Rhea (@I4@) and Sol (@I5@), with a MARR between its
// spouse and CHIL lines. The father, Otto (@I1@), has a second family (@F2@),
// and Quin one of his own (@F3@). Every record is cited, so the extracts
// together hold them all.
const zipOrderFixture = "testdata/zip-order.ged"

// extractWithReport runs 'gedcom extract --format json' and saves its report,
// returning the GEDCOM file and the report file.
func extractWithReport(t *testing.T, master, name string, xrefs ...string) (ged, report string) {
	t.Helper()
	dir := t.TempDir()
	ged, report = filepath.Join(dir, name+".ged"), filepath.Join(dir, name+".json")
	code, stdout, stderr := run(t, append(append([]string{"extract", "--keep-header", "--format", "json", master}, xrefs...), "-o", ged)...)
	if code != ExitOK {
		t.Fatalf("extract %s: exit = %d, stderr = %q", name, code, stderr)
	}
	if err := os.WriteFile(report, []byte(stdout), 0o600); err != nil {
		t.Fatal(err)
	}
	return ged, report
}

// TestCombineZipDropped cuts the master with 'gedcom extract' so that the
// first file holds the last-born child of @F1@ and a later one the earlier
// two, and zips the files back. Without the extracts' reports @F1@'s CHIL
// lines come back out of birth order; with them, 'gedcom diff' against the
// master finds nothing.
func TestCombineZipDropped(t *testing.T) {
	master := gedcomRecords(t, zipOrderFixture)
	tests := []struct {
		name string
		sets [][]string
	}{
		{"two files", [][]string{{"I1", "I5", "I7"}, {"I2", "I3", "I4", "I6", "I8"}}},
		{"three files", [][]string{{"I5", "I7"}, {"I1", "I4", "I6"}, {"I2", "I3", "I8"}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var files, reports []string
			for i, set := range tt.sets {
				ged, report := extractWithReport(t, zipOrderFixture, string(rune('a'+i)), set...)
				files = append(files, ged)
				if i > 0 {
					reports = append(reports, "--dropped", report)
				}
				// A person's families are all extracted with the person, so
				// no FAMS or FAMC line is ever cut.
				var got extractReport
				if err := json.Unmarshal([]byte(readText(t, report)), &got); err != nil {
					t.Fatal(err)
				}
				for _, d := range got.Dropped {
					if d.Tag == "FAMS" || d.Tag == "FAMC" {
						t.Errorf("extract dropped %s", d)
					}
				}
			}
			zip := func(extra ...string) (out, diff string) {
				t.Helper()
				out = filepath.Join(t.TempDir(), "zipped.ged")
				args := append(append([]string{"combine", "--strategy", "zip", "--keep-header"}, files...), extra...)
				code, stdout, stderr := run(t, append(args, "-o", out)...)
				if want := "wrote " + out + ": 13 records, 8 individuals, 3 families\n"; code != ExitOK || stderr != "" || stdout != want {
					t.Fatalf("combine: exit = %d, stdout = %q, stderr = %q", code, stdout, stderr)
				}
				code, diff, stderr = run(t, "diff", zipOrderFixture, out)
				if code != ExitOK || stderr != "" {
					t.Fatalf("diff: exit = %d, stderr = %q", code, stderr)
				}
				// Otto has FAMS lines to two families whose wives are in
				// another file, Quin a FAMC and a FAMS, and Sol a FAMC to a
				// family whose other members are: all as in the master.
				zipped := gedcomRecords(t, out)
				for _, key := range []string{"0 @I1@ INDI", "0 @I3@ INDI", "0 @I5@ INDI"} {
					if !reflect.DeepEqual(zipped[key], master[key]) {
						t.Errorf("%s:\n got %q\nwant %q", key, zipped[key], master[key])
					}
				}
				return out, diff
			}

			// This pins a limit of the library, not a behavior to keep: it
			// shows that the cut exercises --dropped. If gedcom-go comes to
			// restore the master's order without reports, this subtest
			// fails; delete it then, with the help and README text on it.
			t.Run("without reports, one file's order", func(t *testing.T) {
				out, diff := zip()
				if !strings.HasPrefix(diff, "changed\t@F1@\tFAM\n") || strings.Count(diff, "changed\t") != 1 {
					t.Errorf("diff = %q, want only @F1@ changed", diff)
				}
				got, want := gedcomRecords(t, out)["0 @F1@ FAM"], master["0 @F1@ FAM"]
				if reflect.DeepEqual(got, want) || !reflect.DeepEqual(slices.Sorted(slices.Values(got)), slices.Sorted(slices.Values(want))) {
					t.Errorf("@F1@ = %q, want the lines of %q in another order", got, want)
				}
			})

			t.Run("with reports, the master's order", func(t *testing.T) {
				out, diff := zip(reports...)
				if want := zipOrderFixture + " and " + out + ": no differences\n"; diff != want {
					t.Errorf("diff = %q, want %q", diff, want)
				}
				// --keep-header on extract and combine gives the master's
				// header back too.
				if _, diff, _ := run(t, "diff", "--include-header", zipOrderFixture, out); !strings.HasSuffix(diff, ": no differences\n") {
					t.Errorf("diff --include-header = %q, want no differences", diff)
				}
				for key, lines := range master {
					if !reflect.DeepEqual(gedcomRecords(t, out)[key], lines) {
						t.Errorf("%s = %q, want %q", key, gedcomRecords(t, out)[key], lines)
					}
				}
			})
		})
	}
}

// TestCombineDroppedReports checks that --dropped takes its reports
// comma-separated too, and that a report that is not one fails the command by
// name, with nothing written.
func TestCombineDroppedReports(t *testing.T) {
	a, _ := extractWithReport(t, zipOrderFixture, "a", "I5", "I7")
	b, bReport := extractWithReport(t, zipOrderFixture, "b", "I1", "I4", "I6")
	c, cReport := extractWithReport(t, zipOrderFixture, "c", "I2", "I3", "I8")
	out := filepath.Join(t.TempDir(), "zipped.ged")
	if code, _, stderr := run(t, "combine", "--strategy", "zip", "--dropped", bReport+","+cReport, a, b, c, "-o", out); code != ExitOK || stderr != "" {
		t.Fatalf("exit = %d, stderr = %q", code, stderr)
	}
	if _, diff, _ := run(t, "diff", zipOrderFixture, out); !strings.HasSuffix(diff, ": no differences\n") {
		t.Errorf("diff = %q, want no differences", diff)
	}

	tests := []struct {
		name, report, want string
	}{
		{"missing", filepath.Join(t.TempDir(), "nope.json"), "nope.json"},
		{"not JSON", writeTemp(t, "text.json", "wrote b.ged: 9 records\n"), "text.json: not an extract --format json report"},
		{"a GEDCOM file", b, "b.ged: not an extract --format json report"},
		{"JSON of another shape", writeTemp(t, "list.json", "[]\n"), "list.json: not an extract --format json report"},
		{"no dropped list", writeTemp(t, "other.json", `{"output": "b.ged"}`), `other.json: not an extract --format json report: no "dropped" list`},
		// As an extract that did not record positions wrote it: it would
		// order nothing.
		{"no positions", writeTemp(t, "old.json", `{"output": "b.ged", "dropped": [{"xref": "@F1@", "tag": "CHIL", "target": "@I5@", "restorable": true}]}`), `old.json: dropped pointer @F1@ CHIL @I5@ (restorable) has no "position"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out := filepath.Join(t.TempDir(), "out.ged")
			code, stdout, stderr := run(t, "combine", "--strategy", "zip", "--dropped", tt.report, a, b, "-o", out)
			if code != ExitError || stdout != "" || !strings.Contains(stderr, tt.want) {
				t.Errorf("exit = %d, stdout = %q, stderr = %q; want exit %d and %q", code, stdout, stderr, ExitError, tt.want)
			}
			if _, err := os.Stat(out); !os.IsNotExist(err) {
				t.Errorf("output was written: %v", err)
			}
		})
	}
}

// TestCombineDroppedMispaired checks that a report given for a file extract
// did not write it for is a warning: the library joins the lines as without
// it, so the file is still written.
func TestCombineDroppedMispaired(t *testing.T) {
	a, _ := extractWithReport(t, zipOrderFixture, "a", "I5", "I7")
	b, bReport := extractWithReport(t, zipOrderFixture, "b", "I1", "I4", "I6")
	c, cReport := extractWithReport(t, zipOrderFixture, "c", "I2", "I3", "I8")
	out := filepath.Join(t.TempDir(), "zipped.ged")
	code, stdout, stderr := run(t, "combine", "--strategy", "zip", "--dropped", cReport, "--dropped", bReport, a, b, c, "-o", out)
	if code != ExitOK || !strings.HasPrefix(stdout, "wrote "+out) {
		t.Fatalf("exit = %d, stdout = %q, stderr = %q", code, stdout, stderr)
	}
	want := "gedcom: warning: " + cReport + " is the report of " + c + ", not of " + b + ": if it is another file's, the order is not restored\n" +
		"gedcom: warning: " + bReport + " is the report of " + b + ", not of " + c + ": if it is another file's, the order is not restored\n"
	if stderr != want {
		t.Errorf("stderr = %q, want %q", stderr, want)
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
	// The second branch's header is the first's, and is dropped whole: each
	// structure is listed, but none differs.
	var headerConflicts []combineHeaderConflict
	for _, tag := range []string{"SOUR", "GEDC", "CHAR", "SUBM"} {
		headerConflicts = append(headerConflicts, combineHeaderConflict{File: branches[1], Field: "Tags." + tag, Kept: "kept", Dropped: "dropped (KeepFirstHeader keeps doc1's header alone)"})
	}
	want := combineReport{
		Output: out, Strategy: "zip", Stamped: true, Records: 15, Individuals: 8, Families: 3,
		Inputs: []combineInput{
			{File: branches[0], Encoding: "UTF-8", Remapped: map[string]string{}},
			{File: branches[1], Encoding: "UTF-8", Remapped: map[string]string{}},
		},
		HeaderConflicts: headerConflicts,
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

// recordXRefs returns the xref of every record of a GEDCOM file, sorted.
func recordXRefs(t *testing.T, path string) []string {
	t.Helper()
	var xrefs []string
	for key := range gedcomRecords(t, path) {
		if fields := strings.Fields(key); len(fields) > 1 && strings.HasPrefix(fields[1], "@") {
			xrefs = append(xrefs, fields[1])
		}
	}
	slices.Sort(xrefs)
	return xrefs
}

// prefixed maps each xref to itself with the prefix, as --strategy prefix
// renames it.
func prefixed(xrefs []string, prefix string) map[string]string {
	renamed := make(map[string]string, len(xrefs))
	for _, xref := range xrefs {
		renamed[xref] = "@" + prefix + xref[1:]
	}
	return renamed
}

// TestCombinePrefix combines minimal, minimal again and the combine fixture.
// Of the fixture's 15 xrefs, @U1@, @I1@, @I2@ and @F1@ collide with minimal's
// and the other 11 collide with nothing: all 15 get the fixture's prefix.
func TestCombinePrefix(t *testing.T) {
	first, later := recordXRefs(t, sample), recordXRefs(t, combineFixture)
	if len(first) != 4 || len(later) != 15 {
		t.Fatalf("fixtures hold %d and %d xrefs, want 4 and 15", len(first), len(later))
	}
	out := filepath.Join(t.TempDir(), "all.ged")
	code, stdout, stderr := run(t, "combine", "--strategy", "prefix", "--prefix", "B_,C_", sample, sample, combineFixture, "-o", out)
	// Each later file's header points at its own, now prefixed, submitter,
	// a value the first file's header does not carry.
	if want := "gedcom: warning: 2 differing header values from later files dropped (see --format json)\n"; code != ExitOK || stderr != want {
		t.Fatalf("exit = %d, stderr = %q, want %q", code, stderr, want)
	}
	// minimal holds 4 records, combine 15; every one is kept, and every one
	// of a later file is a line of the report.
	want := "wrote " + out + ": 23 records, 12 individuals, 5 families, header stamped\n"
	wantXRefs := slices.Clone(first)
	for _, in := range []struct {
		file, prefix string
		xrefs        []string
	}{{sample, "B_", first}, {combineFixture, "C_", later}} {
		renamed := prefixed(in.xrefs, in.prefix)
		for _, old := range in.xrefs {
			want += in.file + "\t" + old + "\t" + renamed[old] + "\n"
			wantXRefs = append(wantXRefs, renamed[old])
		}
	}
	if stdout != want {
		t.Errorf("stdout = %q, want %q", stdout, want)
	}
	slices.Sort(wantXRefs)
	if got := recordXRefs(t, out); !reflect.DeepEqual(got, wantXRefs) {
		t.Errorf("output xrefs = %v, want %v", got, wantXRefs)
	}
	records := gedcomRecords(t, out)
	// The first file's records are as they were.
	for key, lines := range gedcomRecords(t, sample) {
		if key != "0 HEAD" && key != "0 TRLR" && !reflect.DeepEqual(records[key], lines) {
			t.Errorf("%s = %q, want %q", key, records[key], lines)
		}
	}
	// Pointers follow their records, colliding (@F1@) or not (@F3@).
	if got := records["0 @C_I3@ INDI"]; !slices.Contains(got, "1 FAMC @C_F1@") || !slices.Contains(got, "1 FAMS @C_F3@") {
		t.Errorf("@C_I3@ = %q: its pointers were not remapped", got)
	}
	code, stdout, stderr = run(t, "validate", out)
	if code != ExitOK || stderr != "" || !strings.HasSuffix(stdout, ": no findings\n") {
		t.Errorf("validate: exit = %d, stdout = %q, stderr = %q", code, stdout, stderr)
	}

	code, stdout, stderr = run(t, "combine", "--format", "json", "--strategy", "prefix", "--prefix", "C_", sample, combineFixture, "-o", out)
	if code != ExitOK || stderr != "" {
		t.Fatalf("json: exit = %d, stderr = %q", code, stderr)
	}
	var got combineReport
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("stdout is not JSON: %v\n%s", err, stdout)
	}
	wantInputs := []combineInput{
		{File: sample, Encoding: "UTF-8", Remapped: map[string]string{}},
		{File: combineFixture, Encoding: "UTF-8", Remapped: prefixed(later, "C_")},
	}
	if got.Records != 19 || !reflect.DeepEqual(got.Inputs, wantInputs) {
		t.Errorf("records = %d, inputs = %+v\nwant 19 and %+v", got.Records, got.Inputs, wantInputs)
	}
}

// TestCombinePrefixCollision checks that a prefixed xref an earlier file
// already holds fails the command: the first file's @B_I1@ is what the
// second's @I1@ becomes.
func TestCombinePrefixCollision(t *testing.T) {
	first := writeTemp(t, "first.ged", strings.ReplaceAll(readText(t, sample), "@I1@", "@B_I1@"))
	out := filepath.Join(t.TempDir(), "both.ged")
	for _, format := range []string{"text", "json"} {
		code, stdout, stderr := run(t, "combine", "--format", format, "--strategy", "prefix", "--prefix", "B_", first, sample, "-o", out)
		if want := "gedcom: combine " + sample + ": merge: xref collision: @B_I1@\n"; code != ExitError || stdout != "" || stderr != want {
			t.Errorf("%s: exit = %d, stdout = %q, stderr = %q; want exit %d and %q", format, code, stdout, stderr, ExitError, want)
		}
		if _, err := os.Stat(out); !os.IsNotExist(err) {
			t.Errorf("%s: output was written: %v", format, err)
		}
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

// TestCombineHeader holds that the output header is the first file's alone,
// under every strategy: a later file's header lines, those the first file
// lacks included, are not written but listed in header_conflicts.
func TestCombineHeader(t *testing.T) {
	later := writeTemp(t, "later.ged", "0 HEAD\n1 SOUR other-test\n1 DEST GEDCOM 5.5\n1 GEDC\n2 VERS 5.5.1\n2 FORM LINEAGE-LINKED\n"+
		"1 CHAR UTF-8\n1 NOTE written by another program\n1 _X vendor\n0 @I9@ INDI\n0 TRLR\n")
	header := func(path string) string {
		text := readText(t, path)
		return text[:strings.Index(text, "\n0 ")+1]
	}
	want := header(sample)
	for _, strategy := range [][]string{{"zip"}, {"prefix", "--prefix", "B_"}, {"renumber"}} {
		out := filepath.Join(t.TempDir(), "both.ged")
		args := append([]string{"combine", "--keep-header", "--format", "json", sample, later, "-o", out, "--strategy"}, strategy...)
		code, stdout, stderr := run(t, args...)
		if code != ExitOK || stderr != "" {
			t.Fatalf("%s: exit = %d, stderr = %q", strategy[0], code, stderr)
		}
		if got := header(out); got != want {
			t.Errorf("%s: header =\n%s\nwant the first file's:\n%s", strategy[0], got, want)
		}
		var got combineReport
		if err := json.Unmarshal([]byte(stdout), &got); err != nil {
			t.Fatalf("%s: stdout is not JSON: %v\n%s", strategy[0], err, stdout)
		}
		var fields []string
		for _, c := range got.HeaderConflicts {
			fields = append(fields, c.Field)
		}
		for _, field := range []string{"SourceSystem", "Tags.DEST", "Tags.GEDC", "Tags.NOTE", "Tags._X"} {
			if !slices.Contains(fields, field) {
				t.Errorf("%s: header conflicts %v lack %s", strategy[0], fields, field)
			}
		}
	}

	code, _, stderr := run(t, "combine", "--strategy", "renumber", sample, later, "-o", filepath.Join(t.TempDir(), "text.ged"))
	if want := "gedcom: warning: 4 differing header values from later files dropped (see --format json)\n"; code != ExitOK || stderr != want {
		t.Errorf("text: exit = %d, stderr = %q, want %q", code, stderr, want)
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
		{"no output", []string{"combine", "--strategy", "zip", sample, sample}, ExitUsage, "--output (-o) is required"},
		{"output is input", []string{"combine", "--strategy", "zip", sample, input, "-o", input}, ExitUsage, "is the input file"},
		{"no prefix", []string{"combine", "--strategy", "prefix", sample, sample, "-o", out}, ExitUsage, "got 0, want 1"},
		{"too many prefixes", []string{"combine", "--strategy", "prefix", "--prefix", "B_,C_", sample, sample, "-o", out}, ExitUsage, "got 2, want 1"},
		{"empty prefix", []string{"combine", "--strategy", "prefix", "--prefix", "B_,", sample, sample, sample, "-o", out}, ExitUsage, `invalid --prefix ""`},
		{"prefix with @", []string{"combine", "--strategy", "prefix", "--prefix", "B@", sample, sample, "-o", out}, ExitUsage, `invalid --prefix "B@"`},
		{"prefix with a space", []string{"combine", "--strategy", "prefix", "--prefix", "B ", sample, sample, "-o", out}, ExitUsage, `invalid --prefix "B "`},
		{"prefix given twice", []string{"combine", "--strategy", "prefix", "--prefix", "B_,B_", sample, sample, sample, "-o", out}, ExitUsage, `--prefix "B_" is given twice`},
		{"prefix without prefix strategy", []string{"combine", "--strategy", "zip", "--prefix", "B_", sample, sample, "-o", out}, ExitUsage, "--prefix needs --strategy prefix"},
		{"dropped without zip", []string{"combine", "--strategy", "renumber", "--dropped", "b.json", sample, sample, "-o", out}, ExitUsage, "--dropped needs --strategy zip"},
		{"no dropped for a file", []string{"combine", "--strategy", "zip", "--dropped", "b.json", sample, sample, sample, "-o", out}, ExitUsage, "--dropped needs one report per file after the first (the first file needs none): got 1, want 2"},
		{"empty dropped", []string{"combine", "--strategy", "zip", "--dropped", "", sample, sample, "-o", out}, ExitUsage, "got 0, want 1"},
		{"too many dropped", []string{"combine", "--strategy", "zip", "--dropped", "b.json,c.json", sample, sample, "-o", out}, ExitUsage, "got 2, want 1"},
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
		"Every record of each later file", "Only the colliding xrefs", "Nothing is renamed",
		"with prefix, every xref of each file after the first",
		"gedcom extract --format json tree.ged I3 I4 -o b.ged > b.json",
		"--strategy zip --dropped maternal.json -o tree.ged",
		"in one file's line order", "A record nothing cites",
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("help lacks %q:\n%s", want, stdout)
		}
	}
}

// TestCombineToStdout checks that -o - writes the combined file to stdout and
// the report to stderr, as the other write commands do.
func TestCombineToStdout(t *testing.T) {
	in := writeTemp(t, "one.ged", conflicting)
	code, stdout, stderr := run(t, "combine", "--strategy", "zip", in, "-o", "-")
	if code != ExitOK {
		t.Fatalf("exit = %d, stderr = %q", code, stderr)
	}
	if !strings.Contains(stdout, "0 HEAD") || !strings.Contains(stdout, "0 TRLR") {
		t.Errorf("stdout is not a GEDCOM file:\n%s", stdout)
	}
	if !strings.Contains(stderr, "wrote") {
		t.Errorf("the report is not on stderr: %q", stderr)
	}
}
