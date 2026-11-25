package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
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
		{"too few arguments", []string{"merge-families", mergeFixture, "F1", "-o", out}, ExitUsage, "requires at least 3 arg(s)"},
		{"missing file", []string{"merge-people", "testdata/does-not-exist.ged", "I1", "I2", "-o", out}, ExitError, "does-not-exist.ged"},
		{"same source", []string{"merge-sources", sourcesFixture, "S1", "@S1@", "-o", out}, ExitUsage, "keep and drop are both @S1@"},
		{"missing source", []string{"merge-sources", sourcesFixture, "S1", "S99", "-o", out}, ExitError, "no such record: @S99@"},
		{"repository to merge-sources", []string{"merge-sources", sourcesFixture, "S1", "R1", "-o", out}, ExitError, "@R1@ is a REPO record; want a source"},
		{"same repository", []string{"merge-repositories", sourcesFixture, "R1", "R1", "-o", out}, ExitUsage, "keep and drop are both @R1@"},
		{"missing repository", []string{"merge-repositories", sourcesFixture, "R9", "R1", "-o", out}, ExitError, "no such record: @R9@"},
		{"source to merge-repositories", []string{"merge-repositories", sourcesFixture, "R1", "S1", "-o", out}, ExitError, "@S1@ is a SOUR record; want a repository"},
		{"fold-events on merge-sources", []string{"merge-sources", "--fold-events", sourcesFixture, "S1", "S2", "-o", out}, ExitUsage, "unknown flag: --fold-events"},
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
	for _, cmd := range []string{"merge-people", "merge-families", "merge-sources", "merge-repositories"} {
		code, stdout, _ := run(t, cmd, "--help")
		if code != ExitOK || !strings.Contains(stdout, "gedcom "+cmd+" family.ged @") {
			t.Errorf("%s --help: exit = %d, lacks an example:\n%s", cmd, code, stdout)
		}
	}
	// merge-sources says what it carries and what it reports.
	_, stdout, _ := run(t, "merge-sources", "--help")
	for _, want := range []string{"is carried", "not carried", "dropped       tag, lines, kept", "conflict      tag, lines", "repointed     xref, path"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("merge-sources --help lacks %q", want)
		}
	}
	_, stdout, _ = run(t, "merge-repositories", "--help")
	for _, want := range []string{"is carried", "reported as dropped", "collapsed     xref, path"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("merge-repositories --help lacks %q", want)
		}
	}
}

// sourcesFixture holds one parish register as @S1@ and @S2@, cited by Ada
// (@I1@), Bert (@I2@) and a note, and as @S3@ under a differing title and
// _APID, cited by @F1@. @S1@ and @S3@ name the archive @R1@, @S2@ names
// @R2@, which is the same archive; @R3@ is another one.
const sourcesFixture = "testdata/merge-sources.ged"

// citationCount is the number of source citations, at any level, in the
// GEDCOM file path.
func citationCount(t *testing.T, path string) int {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return len(regexp.MustCompile(`(?m)^[1-9]\d* SOUR @`).FindAll(b, -1))
}

func TestMergeSources(t *testing.T) {
	input, err := os.ReadFile(sourcesFixture)
	if err != nil {
		t.Fatal(err)
	}
	out, stdout := runMerge(t, "merge-sources", sourcesFixture, "@S1@", "S2")
	want := "moved\tPUBL\t1 PUBL Example Press\n" +
		"moved\tREPO\t1 REPO @R2@\n" +
		"moved\tNOTE\t1 NOTE Indexed copy\n" +
		"deduplicated\tTITL\t1 TITL Example parish register\n" +
		"deduplicated\tAUTH\t1 AUTH Example Parish\n" +
		"deduplicated\t_APID\t1 _APID 1,1234::0\n" +
		"repointed\t@I1@\tINDI.BIRT.SOUR\n" +
		"repointed\t@I2@\tINDI.SOUR\n" +
		"repointed\t@N1@\tNOTE.SOUR\n"
	if stdout != want {
		t.Errorf("stdout:\n%s\nwant:\n%s", stdout, want)
	}
	if after, _ := os.ReadFile(sourcesFixture); !bytes.Equal(after, input) {
		t.Error("the input file changed")
	}
	assertValid(t, out)

	// The dropped source is gone, and nothing points at it.
	if code, _, stderr := run(t, "get", out, "@S2@"); code != ExitError || !strings.Contains(stderr, "no such record: @S2@") {
		t.Errorf("get @S2@: exit = %d, stderr = %q", code, stderr)
	}
	doc, err := decodeFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if pointedXRefs(doc)["@S2@"] {
		t.Error("a record still points at @S2@")
	}

	// As many citations as before, their lines kept, now citing @S1@.
	if before, after := citationCount(t, sourcesFixture), citationCount(t, out); before != after {
		t.Errorf("%d citations before, %d after", before, after)
	}
	code, stdout, _ := run(t, "get", out, "@I1@", "@I2@", "@S1@")
	for _, want := range []string{
		"2 SOUR @S1@\n3 PAGE Entry 12\n1 SOUR @S1@\n2 PAGE Folio 3\n",
		"1 SOUR @S1@\n2 PAGE Entry 40\n2 _APID 1,1234::40\n",
		"0 @S1@ SOUR\n1 TITL Example parish register\n1 AUTH Example Parish\n1 _APID 1,1234::0\n1 REPO @R1@\n1 REPO @R2@\n1 PUBL Example Press\n1 NOTE Indexed copy\n",
	} {
		if code != ExitOK || !strings.Contains(stdout, want) {
			t.Errorf("merged records lack %q:\n%s", want, stdout)
		}
	}
}

// TestMergeSourcesConflicts merges two sources that differ: drop's TITL is
// not carried and is reported as dropped, its _APID is carried beside keep's
// and reported as a conflict.
func TestMergeSourcesConflicts(t *testing.T) {
	out, stdout := runMerge(t, "merge-sources", sourcesFixture, "S1", "S3")
	want := "conflict\t_APID\t1 _APID 1,5678::0\n" +
		"dropped\tTITL\t1 TITL Example parish register, 1800-1850\t1 TITL Example parish register\n" +
		"deduplicated\tREPO\t1 REPO @R1@\n" +
		"repointed\t@F1@\tFAM.SOUR\n"
	if stdout != want {
		t.Errorf("stdout:\n%s\nwant:\n%s", stdout, want)
	}
	assertValid(t, out)
	if before, after := citationCount(t, sourcesFixture), citationCount(t, out); before != after {
		t.Errorf("%d citations before, %d after", before, after)
	}
	code, stdout, _ := run(t, "get", out, "@S1@")
	if want := "0 @S1@ SOUR\n1 TITL Example parish register\n1 AUTH Example Parish\n1 _APID 1,1234::0\n1 _APID 1,5678::0\n1 REPO @R1@\n"; code != ExitOK || stdout != want {
		t.Errorf("get @S1@ = %q, want %q", stdout, want)
	}

	_, stdout = runMerge(t, "merge-sources", sourcesFixture, "--format", "json", "S1", "S3")
	var got mergeReport
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("stdout is not JSON: %v\n%s", err, stdout)
	}
	titl := mergedConflict{Tag: "TITL", Lines: []string{"1 TITL Example parish register, 1800-1850"}, Kept: []string{"1 TITL Example parish register"}}
	if got.Dropped == nil || !reflect.DeepEqual(*got.Dropped, []mergedConflict{titl}) {
		t.Errorf("dropped = %+v, want [%+v]", got.Dropped, titl)
	}
	apid := mergedPart{Tag: "_APID", Lines: []string{"1 _APID 1,5678::0"}, Conflict: true}
	if !reflect.DeepEqual(got.Moved, []mergedPart{apid}) || got.Candidates != nil {
		t.Errorf("report = %+v, want moved = [%+v] and no candidates", got, apid)
	}

	// With nothing dropped the list is empty, not missing; merge-people has
	// none.
	_, stdout = runMerge(t, "merge-sources", sourcesFixture, "--format", "json", "S1", "S2")
	if !strings.Contains(stdout, "\"dropped\": []") {
		t.Errorf("merge-sources JSON lacks an empty dropped list:\n%s", stdout)
	}
	_, stdout = runMerge(t, "merge-people", mergeFixture, "--format", "json", "I1", "I2")
	if strings.Contains(stdout, "\"dropped\"") {
		t.Errorf("merge-people JSON has a dropped list:\n%s", stdout)
	}
}

func TestMergeRepositories(t *testing.T) {
	// Once @S2@ is merged, @S1@ names both @R1@ and @R2@: merging them
	// leaves one REPO line.
	sources, _ := runMerge(t, "merge-sources", sourcesFixture, "S1", "S2")
	out, stdout := runMerge(t, "merge-repositories", sources, "R1", "R2")
	want := "moved\tPHON\t1 PHON 555-0100\n" +
		"deduplicated\tNAME\t1 NAME Example Archive\n" +
		"collapsed\t@S1@\tSOUR.REPO\n"
	if stdout != want {
		t.Errorf("stdout:\n%s\nwant:\n%s", stdout, want)
	}
	assertValid(t, out)
	doc, err := decodeFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if doc.GetRecord("@R2@") != nil || pointedXRefs(doc)["@R2@"] {
		t.Error("@R2@ is still in the output, or a record points at it")
	}
	code, stdout, _ := run(t, "get", out, "@S1@", "@R1@")
	if code != ExitOK || strings.Count(stdout, "1 REPO @R1@\n") != 1 || !strings.Contains(stdout, "1 ADDR Archive Street\n1 PHON 555-0100\n") {
		t.Errorf("merged records:\n%s", stdout)
	}

	// A different name is dropped, and reported with keep's.
	_, stdout = runMerge(t, "merge-repositories", sourcesFixture, "--format", "json", "R1", "R3")
	var got mergeReport
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("stdout is not JSON: %v\n%s", err, stdout)
	}
	name := mergedConflict{Tag: "NAME", Lines: []string{"1 NAME Example Library"}, Kept: []string{"1 NAME Example Archive"}}
	if got.Dropped == nil || !reflect.DeepEqual(*got.Dropped, []mergedConflict{name}) {
		t.Errorf("dropped = %+v, want [%+v]", got.Dropped, name)
	}
}

// TestMergeSourcesKeepsUninvolvedRecords settles C2 for merge-sources and
// merge-repositories: every record the merge does not touch is written byte
// for byte.
func TestMergeSourcesKeepsUninvolvedRecords(t *testing.T) {
	input, err := os.ReadFile(sourcesFixture)
	if err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		cmd      string
		args     []string
		involved []string // keep, drop and the records repointed
	}{
		{"merge-sources", []string{"S1", "S2"}, []string{"@S1@", "@S2@", "@I1@", "@I2@", "@N1@"}},
		{"merge-repositories", []string{"R1", "R2"}, []string{"@R1@", "@R2@", "@S2@"}},
	} {
		t.Run(tt.cmd, func(t *testing.T) {
			out, _ := runMerge(t, tt.cmd, sourcesFixture, tt.args...)
			output, err := os.ReadFile(out)
			if err != nil {
				t.Fatal(err)
			}
			before, after := recordChunks(input), recordChunks(output)
			for xref, chunk := range before {
				if slices.Contains(tt.involved, xref) {
					continue
				}
				if !bytes.Equal(after[xref], chunk) {
					t.Errorf("%s changed:\n got %q\nwant %q", xref, after[xref], chunk)
				}
			}
		})
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

// mergeOneByOne merges each pair of in by one run of cmd, each on the last
// run's output, and returns the last output's path with every run's report,
// each line starting with its pair, as a run merging them all prints it.
func mergeOneByOne(t *testing.T, cmd, in string, pairs ...[2]string) (out, report string) {
	t.Helper()
	out = in
	for _, pair := range pairs {
		var stdout string
		out, stdout = runMerge(t, cmd, out, pair[0], pair[1])
		for line := range strings.Lines(stdout) {
			report += pair[0] + "\t" + pair[1] + "\t" + line
		}
	}
	return out, report
}

// assertSameFile fails unless the files at got and want hold the same bytes.
func assertSameFile(t *testing.T, got, want string) {
	t.Helper()
	g, err := os.ReadFile(got)
	if err != nil {
		t.Fatal(err)
	}
	w, err := os.ReadFile(want)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(g, w) {
		t.Errorf("output differs from the runs one pair at a time:\n got %q\nwant %q", g, w)
	}
}

// TestMergeSeveralDrops merges two sources into one in one run: the file and
// the report are those of the two runs one pair at a time.
func TestMergeSeveralDrops(t *testing.T) {
	want, wantReport := mergeOneByOne(t, "merge-sources", sourcesFixture, [2]string{"@S1@", "@S2@"}, [2]string{"@S1@", "@S3@"})
	out, stdout := runMerge(t, "merge-sources", sourcesFixture, "S1", "S2", "S3")
	assertSameFile(t, out, want)
	assertValid(t, out)
	if stdout != wantReport {
		t.Errorf("stdout:\n%s\nwant:\n%s", stdout, wantReport)
	}
	// Each pair's lines, labeled: carried, deduplicated, dropped, conflicting.
	for _, line := range []string{
		"@S1@\t@S2@\tmoved\tPUBL\t1 PUBL Example Press\n",
		"@S1@\t@S2@\tdeduplicated\tTITL\t1 TITL Example parish register\n",
		"@S1@\t@S3@\tconflict\t_APID\t1 _APID 1,5678::0\n",
		"@S1@\t@S3@\tdropped\tTITL\t1 TITL Example parish register, 1800-1850\t1 TITL Example parish register\n",
		"@S1@\t@S3@\trepointed\t@F1@\tFAM.SOUR\n",
	} {
		if !strings.Contains(stdout, line) {
			t.Errorf("stdout lacks %q:\n%s", line, stdout)
		}
	}

	_, stdout = runMerge(t, "merge-sources", sourcesFixture, "--format", "json", "S1", "S2", "S3")
	var got []mergeReport
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("stdout is not a JSON array: %v\n%s", err, stdout)
	}
	if len(got) != 2 || got[0].Keep != "@S1@" || got[0].Drop != "@S2@" || got[1].Keep != "@S1@" || got[1].Drop != "@S3@" {
		t.Fatalf("reports = %+v, want @S2@ then @S3@ into @S1@", got)
	}
	if got[1].Dropped == nil || len(*got[1].Dropped) != 1 || len(got[0].Deduplicated) != 3 || len(got[1].Moved) != 1 || !got[1].Moved[0].Conflict {
		t.Errorf("reports = %+v", got)
	}
}

// TestMergeBatch merges the pairs of a --batch file, comments and blank lines
// skipped: the file and the report are those of the runs one pair at a time.
// The second row drops the first row's keep.
func TestMergeBatch(t *testing.T) {
	pairs := writeTemp(t, "pairs.tsv", "# keep\tdrop\nS1\tS2\n\n@S3@\t@S1@\r\n")
	want, wantReport := mergeOneByOne(t, "merge-sources", sourcesFixture, [2]string{"@S1@", "@S2@"}, [2]string{"@S3@", "@S1@"})
	out, stdout := runMerge(t, "merge-sources", sourcesFixture, "--batch", pairs)
	assertSameFile(t, out, want)
	assertValid(t, out)
	if stdout != wantReport {
		t.Errorf("stdout:\n%s\nwant:\n%s", stdout, wantReport)
	}
	if !strings.Contains(stdout, "@S3@\t@S1@\tdropped\tTITL\t1 TITL Example parish register\t1 TITL Example parish register, 1800-1850\n") {
		t.Errorf("stdout lacks the second pair's dropped line:\n%s", stdout)
	}

	_, stdout = runMerge(t, "merge-sources", sourcesFixture, "--format", "json", "--batch", pairs)
	var got []mergeReport
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("stdout is not a JSON array: %v\n%s", err, stdout)
	}
	if len(got) != 2 || got[0].Drop != "@S2@" || got[1].Keep != "@S3@" || got[1].Drop != "@S1@" {
		t.Errorf("reports = %+v", got)
	}

	// A batch of one row is still labeled, and an array.
	one := writeTemp(t, "one.tsv", "S1\tS2\n")
	_, stdout = runMerge(t, "merge-sources", sourcesFixture, "--format", "json", "--batch", one)
	if !strings.HasPrefix(stdout, "[") {
		t.Errorf("stdout is not an array:\n%s", stdout)
	}
}

// TestMergeRepositoriesSeveral takes merge-sources' forms to
// merge-repositories.
func TestMergeRepositoriesSeveral(t *testing.T) {
	want, wantReport := mergeOneByOne(t, "merge-repositories", sourcesFixture, [2]string{"@R1@", "@R2@"}, [2]string{"@R1@", "@R3@"})
	out, stdout := runMerge(t, "merge-repositories", sourcesFixture, "R1", "R2", "R3")
	assertSameFile(t, out, want)
	if stdout != wantReport || !strings.Contains(stdout, "@R1@\t@R3@\tdropped\tNAME\t1 NAME Example Library\t1 NAME Example Archive\n") {
		t.Errorf("stdout:\n%s\nwant:\n%s", stdout, wantReport)
	}
	pairs := writeTemp(t, "pairs.tsv", "R1\tR2\nR1\tR3\n")
	out, stdout = runMerge(t, "merge-repositories", sourcesFixture, "--batch", pairs)
	assertSameFile(t, out, want)
	if stdout != wantReport {
		t.Errorf("stdout:\n%s\nwant:\n%s", stdout, wantReport)
	}
}

// TestMergePeopleAndFamiliesSeveral checks that merge-people and
// merge-families take the same forms.
func TestMergePeopleAndFamiliesSeveral(t *testing.T) {
	people := writeTemp(t, "people.tsv", "I1\tI2\n")
	want, wantReport := mergeOneByOne(t, "merge-people", mergeFixture, [2]string{"@I1@", "@I2@"})
	out, stdout := runMerge(t, "merge-people", mergeFixture, "--batch", people)
	assertSameFile(t, out, want)
	if stdout != wantReport {
		t.Errorf("stdout:\n%s\nwant:\n%s", stdout, wantReport)
	}

	families := writeTemp(t, "families.tsv", "F1\tF2\n")
	want, wantReport = mergeOneByOne(t, "merge-families", out, [2]string{"@F1@", "@F2@"})
	got, stdout := runMerge(t, "merge-families", out, "--batch", families)
	assertSameFile(t, got, want)
	if stdout != wantReport {
		t.Errorf("stdout:\n%s\nwant:\n%s", stdout, wantReport)
	}
}

// TestMergeManyInvalid checks that an invalid pair is reported by argument or
// by line, and that nothing is written.
func TestMergeManyInvalid(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "out.ged")
	missing := writeTemp(t, "missing.tsv", "S1\tS2\nS1\tS99\n")
	self := writeTemp(t, "self.tsv", "S1\tS2\n\nS3\t@S3@\n")
	twice := writeTemp(t, "twice.tsv", "S1\tS2\nS3\tS2\n")
	removed := writeTemp(t, "removed.tsv", "S1\tS2\nS2\tS3\n")
	malformed := writeTemp(t, "malformed.tsv", "S1\tS2\nS3\n")
	tests := []struct {
		name string
		args []string
		code int
		msg  string
	}{
		{"missing drop argument", []string{"S1", "S2", "S99"}, ExitError, "argument 4 (S99): no such record: @S99@"},
		{"drop named twice", []string{"S1", "S2", "@S2@"}, ExitUsage, "argument 4 (@S2@): @S2@ was merged into @S1@ by argument 3 (S2)"},
		{"keep among the drops", []string{"S1", "S2", "S1"}, ExitUsage, "argument 4 (S1): keep and drop are both @S1@"},
		{"wrong type", []string{"S1", "S2", "R1"}, ExitError, "argument 4 (R1): @R1@ is a REPO record; want a source"},
		{"missing row", []string{"--batch", missing}, ExitError, missing + " line 2: no such record: @S99@"},
		{"self row", []string{"--batch", self}, ExitUsage, self + " line 3: keep and drop are both @S3@"},
		{"drop twice", []string{"--batch", twice}, ExitUsage, twice + " line 2: @S2@ was merged into @S1@ by " + twice + " line 1"},
		{"keep already dropped", []string{"--batch", removed}, ExitUsage, removed + " line 2: @S2@ was merged into @S1@ by " + removed + " line 1"},
		{"malformed row", []string{"--batch", malformed}, ExitError, malformed + " line 2: want keep and drop, tab-separated"},
		{"batch and pairs", []string{"--batch", missing, "S1", "S2"}, ExitUsage, "give <file> alone"},
		{"missing batch", []string{"--batch", filepath.Join(dir, "none.tsv")}, ExitError, "none.tsv"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args := append([]string{"merge-sources", sourcesFixture}, tt.args...)
			code, stdout, stderr := run(t, append(args, "-o", out)...)
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

	// Every invalid pair is reported, not only the first.
	code, _, stderr := run(t, "merge-sources", sourcesFixture, "S1", "S99", "S1", "-o", out)
	if code != ExitError || !strings.Contains(stderr, "argument 3 (S99)") || !strings.Contains(stderr, "argument 4 (S1)") {
		t.Errorf("exit = %d, stderr = %q", code, stderr)
	}
}
