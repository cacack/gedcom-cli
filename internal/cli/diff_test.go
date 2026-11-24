package cli

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

// The diff fixtures are two exports of one tree. Between them: the header's
// DATE and TIME and the vendor tokens (_HPID, _USER) changed; @I1@'s SEX and
// NAME swapped places and its birth date changed; @I2@'s NOTE is split
// differently and moved after FAMS; @I3@ was removed, @I4@ added and made a
// child of @F1@.
const (
	diffOld = "testdata/diff-old.ged"
	diffNew = "testdata/diff-new.ged"
)

func TestDiffText(t *testing.T) {
	code, stdout, stderr := run(t, "diff", diffOld, diffNew)
	if code != ExitOK || stderr != "" {
		t.Fatalf("exit = %d, stderr = %q", code, stderr)
	}
	want := "added\t@I4@\tINDI\n" +
		"removed\t@I3@\tINDI\n" +
		"changed\t@I1@\tINDI\n" +
		"  in INDI > BIRT\n" +
		"  - line 13: 2 DATE 3 MAR 1901\n" +
		"  + line 13: 2 DATE 4 MAR 1901\n" +
		"  in INDI\n" +
		"  - line 15: 1 _HPID token-a\n" +
		"  + line 15: 1 _HPID token-b\n" +
		"changed\t@F1@\tFAM\n" +
		"  in FAM\n" +
		"  - line 29: 1 _USER token-1\n" +
		"  + line 27: 1 _USER token-2\n" +
		"  in FAM\n" +
		"  + line 26: 1 CHIL @I4@\n"
	if stdout != want {
		t.Errorf("stdout =\n%s\nwant\n%s", stdout, want)
	}
}

func TestDiffIgnoreTagsAndHeader(t *testing.T) {
	code, stdout, stderr := run(t, "diff", "--include-header", "--ignore-tag", "_USER", "--ignore-tag", "_HPID", diffOld, diffNew)
	if code != ExitOK || stderr != "" {
		t.Fatalf("exit = %d, stderr = %q", code, stderr)
	}
	want := "added\t@I4@\tINDI\n" +
		"removed\t@I3@\tINDI\n" +
		"changed\t@I1@\tINDI\n" +
		"  in INDI > BIRT\n" +
		"  - line 13: 2 DATE 3 MAR 1901\n" +
		"  + line 13: 2 DATE 4 MAR 1901\n" +
		"changed\t@F1@\tFAM\n" +
		"  in FAM\n" +
		"  + line 26: 1 CHIL @I4@\n" +
		"changed\t\tHEAD\n" +
		"  in HEAD\n" +
		"  - line 3: 1 DATE 1 JAN 2026\n" +
		"  + line 3: 1 DATE 2 FEB 2026\n" +
		"  in HEAD > DATE\n" +
		"  - line 4: 2 TIME 10:00:00\n" +
		"  + line 4: 2 TIME 11:30:00\n"
	if stdout != want {
		t.Errorf("stdout =\n%s\nwant\n%s", stdout, want)
	}
}

// TestDiffMeaningPreserving checks that what the help calls meaning-preserving
// is not reported: @I2@ differs only in line order and CONC split, and @I1@
// only in order once its date and token are ignored.
func TestDiffMeaningPreserving(t *testing.T) {
	_, stdout, _ := run(t, "diff", "--format", "json", "--ignore-tag", "DATE", "--ignore-tag", "_HPID", diffOld, diffNew)
	var got diffReport
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("stdout is not JSON: %v\n%s", err, stdout)
	}
	for _, c := range got.Changed {
		if c.XRef == "@I1@" || c.XRef == "@I2@" {
			t.Errorf("%s reported as changed: %+v", c.XRef, c)
		}
	}

	_, help, _ := run(t, "diff", "--help")
	for _, want := range []string{"Only changes in meaning are reported", "CONT/CONC", "same tag"} {
		if !strings.Contains(help, want) {
			t.Errorf("help lacks %q", want)
		}
	}
}

func TestDiffJSON(t *testing.T) {
	code, stdout, stderr := run(t, "diff", "--format", "json", "--include-header", "--ignore-tag", "_USER", "--ignore-tag", "_HPID", diffOld, diffNew)
	if code != ExitOK || stderr != "" {
		t.Fatalf("exit = %d, stderr = %q", code, stderr)
	}
	var got diffReport
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("stdout is not JSON: %v\n%s", err, stdout)
	}
	line := func(n int, text string) []diffLine { return []diffLine{{Line: n, Text: text}} }
	want := diffReport{
		Old:     diffOld,
		New:     diffNew,
		Added:   []diffRecord{{XRef: "@I4@", Type: "INDI"}},
		Removed: []diffRecord{{XRef: "@I3@", Type: "INDI"}},
		Changed: []diffChanged{
			{diffRecord: diffRecord{XRef: "@I1@", Type: "INDI"}, Changes: []diffHunk{
				{Path: []string{"BIRT"}, Removed: line(13, "2 DATE 3 MAR 1901"), Added: line(13, "2 DATE 4 MAR 1901")},
			}},
			{diffRecord: diffRecord{XRef: "@F1@", Type: "FAM"}, Changes: []diffHunk{
				{Path: []string{}, Removed: []diffLine{}, Added: line(26, "1 CHIL @I4@")},
			}},
		},
		Header: &diffChanged{diffRecord: diffRecord{Type: "HEAD"}, Changes: []diffHunk{
			{Path: []string{}, Removed: line(3, "1 DATE 1 JAN 2026"), Added: line(3, "1 DATE 2 FEB 2026")},
			{Path: []string{"DATE"}, Removed: line(4, "2 TIME 10:00:00"), Added: line(4, "2 TIME 11:30:00")},
		}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got  %+v\nwant %+v", got, want)
	}
}

func TestDiffNoDifferences(t *testing.T) {
	code, stdout, stderr := run(t, "diff", diffOld, diffOld)
	if code != ExitOK || stderr != "" {
		t.Fatalf("exit = %d, stderr = %q", code, stderr)
	}
	if want := diffOld + " and " + diffOld + ": no differences\n"; stdout != want {
		t.Errorf("stdout = %q, want %q", stdout, want)
	}

	_, stdout, _ = run(t, "diff", "--format", "json", diffOld, diffOld)
	for _, want := range []string{`"added": []`, `"removed": []`, `"changed": []`, `"header": null`} {
		if !strings.Contains(stdout, want) {
			t.Errorf("json lacks %s:\n%s", want, stdout)
		}
	}
}

func TestDiffExitCodes(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want int
	}{
		{"differences are not a failure", []string{"diff", diffOld, diffNew}, ExitOK},
		{"missing file", []string{"diff", diffOld, "testdata/does-not-exist.ged"}, ExitError},
		{"one file", []string{"diff", diffOld}, ExitUsage},
		{"empty ignore-tag", []string{"diff", "--ignore-tag", "", diffOld, diffNew}, ExitUsage},
		{"ignore-tag with a space", []string{"diff", "--ignore-tag", "_USER _HPID", diffOld, diffNew}, ExitUsage},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code, _, stderr := run(t, tt.args...)
			if code != tt.want {
				t.Errorf("exit = %d, want %d (stderr %q)", code, tt.want, stderr)
			}
			if (code == ExitOK) != (stderr == "") {
				t.Errorf("exit %d with stderr %q: stderr must be set exactly on failure", code, stderr)
			}
		})
	}
}
