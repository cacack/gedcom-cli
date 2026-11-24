package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"
)

// mergeFixture holds Ada twice (@I1@, @I2@), each the wife of Carl (@I3@) in
// a family of her own (@F1@, @F2@) and a child of @F3@, which lists both. The
// second Ada's birth year differs and cites @S1@; Finn (@I6@) names her as
// an associate.
const mergeFixture = "testdata/merge.ged"

// mergeI2IntoI1 is merge-people's report for @I2@ into @I1@.
const mergeI2IntoI1 = "conflict\tBIRT\t1 BIRT | 2 DATE 1802 | 2 SOUR @S1@\n" +
	"moved\tDEAT\t1 DEAT | 2 DATE 14 NOV 1878\n" +
	"moved\tFAMS\t1 FAMS @F2@\n" +
	"moved\tNOTE\t1 NOTE Recorded twice in the parish register\n" +
	"deduplicated\tNAME\t1 NAME Ada /Example/\n" +
	"deduplicated\tSEX\t1 SEX F\n" +
	"deduplicated\tFAMC\t1 FAMC @F3@\n" +
	"repointed\t@I6@\tINDI.ASSO\n" +
	"repointed\t@F2@\tFAM.WIFE\n" +
	"collapsed\t@F3@\tFAM.CHIL\n" +
	"candidate\t@I3@\t@F1@\t@F2@\n"

// runMerge runs a merge command on in, writing to a temporary file, and
// returns the output file's path with stdout.
func runMerge(t *testing.T, cmd, in string, args ...string) (out, stdout string) {
	t.Helper()
	out = filepath.Join(t.TempDir(), "out.ged")
	code, stdout, stderr := run(t, append(append([]string{cmd, in}, args...), "-o", out)...)
	if code != ExitOK || stderr != "" {
		t.Fatalf("exit = %d, stderr = %q", code, stderr)
	}
	return out, stdout
}

// assertValid runs validate on path and expects no findings.
func assertValid(t *testing.T, path string) {
	t.Helper()
	code, stdout, stderr := run(t, "validate", path)
	if code != ExitOK || !strings.HasSuffix(stdout, "no findings\n") {
		t.Errorf("validate: exit = %d, stdout = %q, stderr = %q", code, stdout, stderr)
	}
}

func TestMergePeopleText(t *testing.T) {
	input, err := os.ReadFile(mergeFixture)
	if err != nil {
		t.Fatal(err)
	}
	out, stdout := runMerge(t, "merge-people", mergeFixture, "@I1@", "I2")
	if stdout != mergeI2IntoI1 {
		t.Errorf("stdout:\n%s\nwant:\n%s", stdout, mergeI2IntoI1)
	}
	if after, _ := os.ReadFile(mergeFixture); !bytes.Equal(after, input) {
		t.Error("the input file changed")
	}
	assertValid(t, out)

	// The dropped person is gone, and nothing points at her.
	if code, _, stderr := run(t, "get", out, "@I2@"); code != ExitError || !strings.Contains(stderr, "no such record: @I2@") {
		t.Errorf("get @I2@: exit = %d, stderr = %q", code, stderr)
	}
	doc, err := decodeFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if pointedXRefs(doc)["@I2@"] {
		t.Error("a record still points at @I2@")
	}

	// Her facts are the kept person's, and the families are left unmerged.
	code, stdout, _ := run(t, "get", out, "@I1@", "@F1@", "@F2@", "@F3@")
	if code != ExitOK {
		t.Fatalf("get: exit = %d", code)
	}
	for _, want := range []string{"2 DATE 14 NOV 1878\n", "1 FAMS @F1@\n1 FAMS @F2@\n1 FAMC @F3@\n", "0 @F2@ FAM\n1 HUSB @I3@\n1 WIFE @I1@\n"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("merged records lack %q:\n%s", want, stdout)
		}
	}
	if f3 := "0 @F3@ FAM\n1 HUSB @I7@\n1 CHIL @I1@\n"; !strings.HasSuffix(stdout, f3) {
		t.Errorf("@F3@ should list @I1@ once:\n%s", stdout)
	}
}

func TestMergePeopleJSON(t *testing.T) {
	_, stdout := runMerge(t, "merge-people", mergeFixture, "--format", "json", "I1", "I2")
	var got mergeReport
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("stdout is not JSON: %v\n%s", err, stdout)
	}
	if got.Keep != "@I1@" || got.Drop != "@I2@" {
		t.Errorf("keep, drop = %s, %s", got.Keep, got.Drop)
	}
	wantMoved := []mergedPart{
		{Tag: "BIRT", Lines: []string{"1 BIRT", "2 DATE 1802", "2 SOUR @S1@"}, Conflict: true},
		{Tag: "DEAT", Lines: []string{"1 DEAT", "2 DATE 14 NOV 1878"}},
		{Tag: "FAMS", Lines: []string{"1 FAMS @F2@"}},
		{Tag: "NOTE", Lines: []string{"1 NOTE Recorded twice in the parish register"}},
	}
	if !reflect.DeepEqual(got.Moved, wantMoved) {
		t.Errorf("moved = %+v, want %+v", got.Moved, wantMoved)
	}
	if len(got.Deduplicated) != 3 || got.Deduplicated[1].Tag != "SEX" {
		t.Errorf("deduplicated = %+v", got.Deduplicated)
	}
	if want := []mergeRef{{"@I6@", "INDI.ASSO"}, {"@F2@", "FAM.WIFE"}}; !reflect.DeepEqual(got.Repointed, want) {
		t.Errorf("repointed = %+v, want %+v", got.Repointed, want)
	}
	if want := []mergeRef{{"@F3@", "FAM.CHIL"}}; !reflect.DeepEqual(got.Collapsed, want) {
		t.Errorf("collapsed = %+v, want %+v", got.Collapsed, want)
	}
	if want := []familyCandidate{{Partner: "@I3@", Families: []string{"@F1@", "@F2@"}}}; got.Candidates == nil || !reflect.DeepEqual(*got.Candidates, want) {
		t.Errorf("candidates = %+v, want %+v", got.Candidates, want)
	}
}

// TestMergePeopleConflicts merges two different people: the differing name
// and sex are conflicts, a second family link is not, and with no shared
// partner there are no candidates.
func TestMergePeopleConflicts(t *testing.T) {
	out, stdout := runMerge(t, "merge-people", mergeFixture, "I1", "I3")
	want := "conflict\tNAME\t1 NAME Carl /Example/\n" +
		"conflict\tSEX\t1 SEX M\n" +
		"moved\tFAMS\t1 FAMS @F2@\n" +
		"deduplicated\tFAMS\t1 FAMS @F1@\n" +
		"repointed\t@F1@\tFAM.HUSB\n" +
		"repointed\t@F2@\tFAM.HUSB\n"
	if stdout != want {
		t.Errorf("stdout:\n%s\nwant:\n%s", stdout, want)
	}
	assertValid(t, out)
}

// TestMergePeopleRepeatableConflict pins that a tag which may repeat is a
// conflict too when the lines differ: conflict means keep already held the
// tag, not that GEDCOM allows it once.
func TestMergePeopleRepeatableConflict(t *testing.T) {
	in := filepath.Join(t.TempDir(), "occu.ged")
	data := "0 HEAD\n1 GEDC\n2 VERS 5.5.1\n2 FORM LINEAGE-LINKED\n1 CHAR UTF-8\n" +
		"0 @I1@ INDI\n1 NAME Hal /Example/\n1 OCCU Farmer\n" +
		"0 @I2@ INDI\n1 NAME Hal /Example/\n1 OCCU Miller\n1 OCCU Farmer\n0 TRLR\n"
	if err := os.WriteFile(in, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
	_, stdout := runMerge(t, "merge-people", in, "I1", "I2")
	want := "conflict\tOCCU\t1 OCCU Miller\n" +
		"deduplicated\tNAME\t1 NAME Hal /Example/\n" +
		"deduplicated\tOCCU\t1 OCCU Farmer\n"
	if stdout != want {
		t.Errorf("stdout:\n%s\nwant:\n%s", stdout, want)
	}
}

// foldFixture holds Ida twice. Both births are 7 Dec 1977, written two ways,
// with a place in one and a citation in each; the christenings differ in
// date.
const foldFixture = "0 HEAD\n1 GEDC\n2 VERS 5.5.1\n2 FORM LINEAGE-LINKED\n1 CHAR UTF-8\n" +
	"1 SUBM @U1@\n0 @U1@ SUBM\n1 NAME Test Submitter\n" +
	"0 @I1@ INDI\n1 NAME Ida /Example/\n1 SEX F\n1 BIRT\n2 DATE 7 DEC 1977\n2 PLAC Oldtown\n2 SOUR @S1@\n1 CHR\n2 DATE 1 JAN 1978\n" +
	"0 @I2@ INDI\n1 NAME Ida /Example/\n1 SEX F\n1 BIRT\n2 DATE 07 Dec 1977\n2 SOUR @S2@\n2 NOTE Baptism register\n1 CHR\n2 DATE 8 JAN 1978\n" +
	"0 @S1@ SOUR\n1 TITL Example civil register\n0 @S2@ SOUR\n1 TITL Example parish register\n0 TRLR\n"

// TestMergePeopleFoldEvents covers --fold-events: a birth on the same date
// written differently folds into one BIRT holding both citations and the
// note, the report lists it, and christenings on different dates are both
// kept and reported as a conflict.
func TestMergePeopleFoldEvents(t *testing.T) {
	in := writeTemp(t, "fold.ged", foldFixture)
	out, stdout := runMerge(t, "merge-people", in, "--fold-events", "I1", "I2")
	want := "conflict\tCHR\t1 CHR | 2 DATE 8 JAN 1978\n" +
		"folded\tBIRT\t1 BIRT | 2 DATE 07 Dec 1977 | 2 SOUR @S2@ | 2 NOTE Baptism register\t" +
		"1 BIRT | 2 DATE 7 DEC 1977 | 2 PLAC Oldtown | 2 SOUR @S1@ | 2 SOUR @S2@ | 2 NOTE Baptism register\n" +
		"deduplicated\tNAME\t1 NAME Ida /Example/\n" +
		"deduplicated\tSEX\t1 SEX F\n"
	if stdout != want {
		t.Errorf("stdout:\n%s\nwant:\n%s", stdout, want)
	}
	assertValid(t, out)
	code, stdout, _ := run(t, "get", out, "@I1@")
	wantI1 := "0 @I1@ INDI\n1 NAME Ida /Example/\n1 SEX F\n" +
		"1 BIRT\n2 DATE 7 DEC 1977\n2 PLAC Oldtown\n2 SOUR @S1@\n2 SOUR @S2@\n2 NOTE Baptism register\n" +
		"1 CHR\n2 DATE 1 JAN 1978\n1 CHR\n2 DATE 8 JAN 1978\n"
	if code != ExitOK || stdout != wantI1 {
		t.Errorf("get @I1@ = %q, want %q", stdout, wantI1)
	}

	_, stdout = runMerge(t, "merge-people", in, "--fold-events", "--format", "json", "I1", "I2")
	var got mergeReport
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("stdout is not JSON: %v\n%s", err, stdout)
	}
	wantFolded := []mergedFold{{
		Tag:   "BIRT",
		Lines: []string{"1 BIRT", "2 DATE 07 Dec 1977", "2 SOUR @S2@", "2 NOTE Baptism register"},
		Into:  []string{"1 BIRT", "2 DATE 7 DEC 1977", "2 PLAC Oldtown", "2 SOUR @S1@", "2 SOUR @S2@", "2 NOTE Baptism register"},
	}}
	if !reflect.DeepEqual(got.Folded, wantFolded) {
		t.Errorf("folded = %+v, want %+v", got.Folded, wantFolded)
	}
	if wantMoved := []mergedPart{{Tag: "CHR", Lines: []string{"1 CHR", "2 DATE 8 JAN 1978"}, Conflict: true}}; !reflect.DeepEqual(got.Moved, wantMoved) {
		t.Errorf("moved = %+v, want %+v", got.Moved, wantMoved)
	}

	// Without the flag nothing folds: both births are kept, and the report's
	// folded list is empty, not missing.
	_, stdout = runMerge(t, "merge-people", in, "--format", "json", "I1", "I2")
	if !strings.Contains(stdout, "\"folded\": []") || !strings.Contains(stdout, "\"2 DATE 07 Dec 1977\"") {
		t.Errorf("without --fold-events, the report should move the birth and fold nothing:\n%s", stdout)
	}

	// Ida is presumed living, so --redact-living hides the folded lines.
	code, stdout, stderr := run(t, "--redact-living", "--reference-year", referenceYear, "merge-people", "--fold-events", in, "I1", "I2", "-o", filepath.Join(t.TempDir(), "out.ged"))
	if code != ExitOK || !strings.Contains(stdout, "folded\tBIRT\tLiving\tLiving\n") || strings.Contains(stdout, "Oldtown") || strings.Contains(stdout, "1977") {
		t.Errorf("redacted: exit = %d, stderr = %q, stdout:\n%s", code, stderr, stdout)
	}
}

// TestMergeFamiliesFoldEvents folds a marriage on the same date, written two
// ways, the place only drop's has joining keep's.
func TestMergeFamiliesFoldEvents(t *testing.T) {
	in := writeTemp(t, "fold-fam.ged", "0 HEAD\n1 GEDC\n2 VERS 5.5.1\n2 FORM LINEAGE-LINKED\n1 CHAR UTF-8\n"+
		"0 @I1@ INDI\n1 NAME Hal /Example/\n1 SEX M\n1 FAMS @F1@\n1 FAMS @F2@\n"+
		"0 @F1@ FAM\n1 HUSB @I1@\n1 MARR\n2 DATE 5 MAY 2000\n"+
		"0 @F2@ FAM\n1 HUSB @I1@\n1 MARR\n2 DATE 05 May 2000\n2 PLAC Newtown\n0 TRLR\n")
	out, stdout := runMerge(t, "merge-families", in, "--fold-events", "F1", "F2")
	want := "folded\tMARR\t1 MARR | 2 DATE 05 May 2000 | 2 PLAC Newtown\t1 MARR | 2 DATE 5 MAY 2000 | 2 PLAC Newtown\n" +
		"deduplicated\tHUSB\t1 HUSB @I1@\n" +
		"collapsed\t@I1@\tINDI.FAMS\n"
	if stdout != want {
		t.Errorf("stdout:\n%s\nwant:\n%s", stdout, want)
	}
	code, stdout, _ := run(t, "get", out, "@F1@")
	if want := "0 @F1@ FAM\n1 HUSB @I1@\n1 MARR\n2 DATE 5 MAY 2000\n2 PLAC Newtown\n"; code != ExitOK || stdout != want {
		t.Errorf("get @F1@ = %q, want %q", stdout, want)
	}

	// JSON reports the same fold.
	_, stdout = runMerge(t, "merge-families", in, "--fold-events", "--format", "json", "F1", "F2")
	var got mergeReport
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("stdout is not JSON: %v\n%s", err, stdout)
	}
	marr := mergedFold{Tag: "MARR", Lines: []string{"1 MARR", "2 DATE 05 May 2000", "2 PLAC Newtown"}, Into: []string{"1 MARR", "2 DATE 5 MAY 2000", "2 PLAC Newtown"}}
	if len(got.Folded) != 1 || !reflect.DeepEqual(got.Folded[0], marr) {
		t.Errorf("folded = %+v, want [%+v]", got.Folded, marr)
	}

	// Two of drop's marriages fold into one of keep's: each into is keep's
	// once both folds are done.
	two := writeTemp(t, "fold-two.ged", "0 HEAD\n1 GEDC\n2 VERS 5.5.1\n2 FORM LINEAGE-LINKED\n1 CHAR UTF-8\n"+
		"0 @F1@ FAM\n1 MARR\n2 DATE 5 MAY 2000\n"+
		"0 @F2@ FAM\n1 MARR\n2 DATE 5 MAY 2000\n2 NOTE one\n1 MARR\n2 DATE 05 May 2000\n2 NOTE two\n0 TRLR\n")
	_, stdout = runMerge(t, "merge-families", two, "--fold-events", "F1", "F2")
	want = "folded\tMARR\t1 MARR | 2 DATE 5 MAY 2000 | 2 NOTE one\t1 MARR | 2 DATE 5 MAY 2000 | 2 NOTE one | 2 NOTE two\n" +
		"folded\tMARR\t1 MARR | 2 DATE 05 May 2000 | 2 NOTE two\t1 MARR | 2 DATE 5 MAY 2000 | 2 NOTE one | 2 NOTE two\n"
	if stdout != want {
		t.Errorf("two folds, stdout:\n%s\nwant:\n%s", stdout, want)
	}
}

func TestMergeFamilies(t *testing.T) {
	people, _ := runMerge(t, "merge-people", mergeFixture, "I1", "I2")
	out, stdout := runMerge(t, "merge-families", people, "F1", "F2")
	want := "moved\tCHIL\t1 CHIL @I5@\n" +
		"moved\tMARR\t1 MARR | 2 DATE 1820\n" +
		"deduplicated\tHUSB\t1 HUSB @I3@\n" +
		"deduplicated\tWIFE\t1 WIFE @I1@\n" +
		"repointed\t@I5@\tINDI.FAMC\n" +
		"collapsed\t@I1@\tINDI.FAMS\n" +
		"collapsed\t@I3@\tINDI.FAMS\n"
	if stdout != want {
		t.Errorf("stdout:\n%s\nwant:\n%s", stdout, want)
	}
	assertValid(t, out)
	if code, _, _ := run(t, "get", out, "@F2@"); code != ExitError {
		t.Errorf("get @F2@: exit = %d, want %d", code, ExitError)
	}
	code, stdout, _ := run(t, "get", out, "@F1@")
	if want := "0 @F1@ FAM\n1 HUSB @I3@\n1 WIFE @I1@\n1 CHIL @I4@\n1 CHIL @I5@\n1 MARR\n2 DATE 1820\n"; code != ExitOK || stdout != want {
		t.Errorf("get @F1@ = %q, want %q", stdout, want)
	}

	// A differing spouse is a conflict.
	_, stdout = runMerge(t, "merge-families", mergeFixture, "--format", "json", "F1", "F3")
	var got mergeReport
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("stdout is not JSON: %v\n%s", err, stdout)
	}
	husb := mergedPart{Tag: "HUSB", Lines: []string{"1 HUSB @I7@"}, Conflict: true}
	if len(got.Moved) == 0 || !reflect.DeepEqual(got.Moved[0], husb) || got.Candidates != nil {
		t.Errorf("report = %+v, want moved[0] = %+v and no candidates", got, husb)
	}
}

// TestMergeKeepsUninvolvedRecords settles C2 for the merge commands, which
// always request a change and so are not in writeCommands: every record the
// merge does not touch is written byte for byte, in a file with CRLF line
// endings too.
func TestMergeKeepsUninvolvedRecords(t *testing.T) {
	lf, err := os.ReadFile(mergeFixture)
	if err != nil {
		t.Fatal(err)
	}
	crlf := filepath.Join(t.TempDir(), "crlf.ged")
	if err := os.WriteFile(crlf, bytes.ReplaceAll(lf, []byte("\n"), []byte("\r\n")), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, in := range []string{mergeFixture, crlf} {
		t.Run(filepath.Base(in), func(t *testing.T) {
			input, _ := os.ReadFile(in)
			out, _ := runMerge(t, "merge-people", in, "I1", "I2")
			output, err := os.ReadFile(out)
			if err != nil {
				t.Fatal(err)
			}
			// @I1@, @I2@ and the records repointed or collapsed.
			involved := map[string]bool{"@I1@": true, "@I2@": true, "@I6@": true, "@F2@": true, "@F3@": true}
			before, after := recordChunks(input), recordChunks(output)
			for xref, chunk := range before {
				if involved[xref] {
					continue
				}
				if !bytes.Equal(after[xref], chunk) {
					t.Errorf("%s changed:\n got %q\nwant %q", xref, after[xref], chunk)
				}
			}
			if bytes.Contains(input, []byte("\r\n")) && bytes.Count(output, []byte("\n")) != bytes.Count(output, []byte("\r\n")) {
				t.Error("output mixes line endings")
			}
		})
	}
}

var recordStart = regexp.MustCompile(`(?m)^0 (@[^@]+@ )?(\S+)`)

// recordChunks splits a GEDCOM file into its records' bytes, by xref, or by
// tag for a record without one (HEAD, TRLR).
func recordChunks(b []byte) map[string][]byte {
	chunks := map[string][]byte{}
	starts := recordStart.FindAllSubmatchIndex(b, -1)
	for i, m := range starts {
		end := len(b)
		if i+1 < len(starts) {
			end = starts[i+1][0]
		}
		key := string(b[m[4]:m[5]])
		if m[2] >= 0 {
			key = strings.TrimSpace(string(b[m[2]:m[3]]))
		}
		chunks[key] = b[m[0]:end]
	}
	return chunks
}

func TestMergeUsage(t *testing.T) {
	out := filepath.Join(t.TempDir(), "out.ged")
	tests := []struct {
		name string
		args []string
		code int
		msg  string
	}{
		{"same xref", []string{"merge-people", mergeFixture, "I1", "@I1@", "-o", out}, ExitUsage, "keep and drop are both @I1@"},
		{"missing xref", []string{"merge-people", mergeFixture, "I1", "I99", "-o", out}, ExitError, "no such record: @I99@"},
		{"family to merge-people", []string{"merge-people", mergeFixture, "I1", "F1", "-o", out}, ExitError, "@F1@ is a FAM record; want an individual"},
		{"person to merge-families", []string{"merge-families", mergeFixture, "I1", "F1", "-o", out}, ExitError, "@I1@ is a INDI record; want a family"},
		{"missing family", []string{"merge-families", mergeFixture, "F9", "F1", "-o", out}, ExitError, "no such record: @F9@"},
		{"no output", []string{"merge-people", mergeFixture, "I1", "I2"}, ExitUsage, "--output (-o) is required"},
		{"output is input", []string{"merge-people", mergeFixture, "I1", "I2", "-o", mergeFixture}, ExitUsage, "is the input file"},
		{"too few arguments", []string{"merge-families", mergeFixture, "F1", "-o", out}, ExitUsage, "accepts 3 arg(s)"},
		{"missing file", []string{"merge-people", "testdata/does-not-exist.ged", "I1", "I2", "-o", out}, ExitError, "does-not-exist.ged"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code, stdout, stderr := run(t, tt.args...)
			if code != tt.code || !strings.Contains(stderr, tt.msg) {
				t.Errorf("exit = %d, want %d; stderr = %q, want %q", code, tt.code, stderr, tt.msg)
			}
			if stdout != "" {
				t.Errorf("stdout = %q, want nothing", stdout)
			}
			if _, err := os.Stat(out); err == nil {
				t.Error("an output file was written")
			}
		})
	}
}

func TestMergeHelp(t *testing.T) {
	for _, cmd := range []string{"merge-people", "merge-families"} {
		code, stdout, _ := run(t, cmd, "--help")
		if code != ExitOK || !strings.Contains(stdout, "gedcom "+cmd+" family.ged @") {
			t.Errorf("%s --help: exit = %d, lacks an example:\n%s", cmd, code, stdout)
		}
	}
}

// TestMergeToStdout checks that -o - writes the merged file to stdout and the
// report to stderr, as the other write commands do.
func TestMergeToStdout(t *testing.T) {
	code, stdout, stderr := run(t, "merge-people", mergeFixture, "I1", "I2", "-o", "-")
	if code != ExitOK {
		t.Fatalf("exit = %d, stderr = %q", code, stderr)
	}
	if !strings.HasPrefix(stdout, "0 HEAD") || !strings.Contains(stdout, "0 TRLR") {
		t.Errorf("stdout is not a GEDCOM file:\n%s", stdout)
	}
	if strings.Contains(stdout, "0 @I2@ INDI") {
		t.Errorf("the dropped record is still in the output")
	}
	if stderr == "" {
		t.Errorf("the report is not on stderr")
	}
}
