package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// repeatedNamesGED holds an individual (@I1@) whose first name is written
// again in other case, spacing and punctuation, with no name pieces, one
// citation the first has line for line and one it lacks, and the SEX line
// between them; one (@I2@) with two equal values whose TYPEs differ, and a
// third name that differs; and one (@I3@) with three copies of one name,
// the second carrying a note.
const repeatedNamesGED = "testdata/repeated-names.ged"

const (
	repeatedJohn = "repeated\t@I1@\tNAME\t1 NAME John /Sample/ Jr. | 2 GIVN John | 2 SURN Sample | 2 NSFX Jr. | 2 SOUR @S1@ | 3 PAGE 1\t" +
		"1 NAME john  sample, jr | 2 SOUR @S1@ | 3 PAGE 1 | 2 SOUR @S1@ | 3 PAGE 2\n"
	repeatedCleo = "repeated\t@I3@\tNAME\t1 NAME Cleo /Sample/\t1 NAME Cleo /Sample/ | 2 NOTE Copied from the register\t1 NAME CLEO /SAMPLE/\n"
)

// TestRepeatedNamesReport checks that the report lists each person's
// repeated names and leaves out names whose TYPEs differ and names that
// differ.
func TestRepeatedNamesReport(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{"every individual", nil, repeatedJohn + repeatedCleo},
		{"individuals named", []string{"I3"}, repeatedCleo},
		{"nothing repeated", []string{"@I2@"}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code, stdout, stderr := run(t, append([]string{"repeated-names", repeatedNamesGED}, tt.args...)...)
			if code != ExitOK || stderr != "" {
				t.Fatalf("exit = %d, stderr = %q", code, stderr)
			}
			if stdout != tt.want {
				t.Errorf("stdout:\n%s\nwant:\n%s", stdout, tt.want)
			}
		})
	}

	code, stdout, stderr := run(t, "repeated-names", "--format", "json", repeatedNamesGED, "@I3@")
	if code != ExitOK || stderr != "" {
		t.Fatalf("json: exit = %d, stderr = %q", code, stderr)
	}
	var got []repeatedGroup
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("stdout is not JSON: %v\n%s", err, stdout)
	}
	want := []repeatedGroup{{XRef: "@I3@", Tag: "NAME", Members: [][]string{
		{"1 NAME Cleo /Sample/"},
		{"1 NAME Cleo /Sample/", "2 NOTE Copied from the register"},
		{"1 NAME CLEO /SAMPLE/"},
	}}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("json = %+v, want %+v", got, want)
	}

	// A file with no repeated name reports nothing, and exits 0.
	for _, format := range []string{"text", "json"} {
		code, stdout, stderr = run(t, "repeated-names", "--format", format, records)
		want := ""
		if format == "json" {
			want = "[]\n"
		}
		if code != ExitOK || stderr != "" || stdout != want {
			t.Errorf("%s, none: exit = %d, stderr = %q, stdout = %q, want %q", format, code, stderr, stdout, want)
		}
	}
}

// TestRepeatedNamesFold checks the whole file --fold writes: each group is
// its first NAME, with every distinct citation and note once, and every
// other line of the file is as read. The input is not changed.
func TestRepeatedNamesFold(t *testing.T) {
	input := readFixture(t, repeatedNamesGED)
	want := replaceOnce(t, input, "3 PAGE 1\n1 SEX M\n1 NAME john  sample, jr\n2 SOUR @S1@\n3 PAGE 1\n2 SOUR @S1@\n3 PAGE 2\n",
		"3 PAGE 1\n2 SOUR @S1@\n3 PAGE 2\n1 SEX M\n")
	want = replaceOnce(t, want, "1 NAME Cleo /Sample/\n1 NAME Cleo /Sample/\n2 NOTE Copied from the register\n1 NAME CLEO /SAMPLE/\n",
		"1 NAME Cleo /Sample/\n2 NOTE Copied from the register\n")

	out := filepath.Join(t.TempDir(), "out.ged")
	code, stdout, stderr := run(t, "repeated-names", "--fold", repeatedNamesGED, "-o", out)
	if code != ExitOK || stderr != "" {
		t.Fatalf("exit = %d, stderr = %q", code, stderr)
	}
	got := readFixture(t, out)
	if got != want {
		t.Errorf("output:\n%s\nwant:\n%s", got, want)
	}
	if after := readFixture(t, repeatedNamesGED); after != input {
		t.Error("the input file changed")
	}
	wantReport := "folded\t@I1@\tNAME\t1 NAME john  sample, jr | 2 SOUR @S1@ | 3 PAGE 1 | 2 SOUR @S1@ | 3 PAGE 2\t" +
		"1 NAME John /Sample/ Jr. | 2 GIVN John | 2 SURN Sample | 2 NSFX Jr. | 2 SOUR @S1@ | 3 PAGE 1 | 2 SOUR @S1@ | 3 PAGE 2\n" +
		"folded\t@I3@\tNAME\t1 NAME Cleo /Sample/ | 2 NOTE Copied from the register\t1 NAME Cleo /Sample/ | 2 NOTE Copied from the register\n" +
		"folded\t@I3@\tNAME\t1 NAME CLEO /SAMPLE/\t1 NAME Cleo /Sample/ | 2 NOTE Copied from the register\n"
	if stdout != wantReport {
		t.Errorf("report:\n%s\nwant:\n%s", stdout, wantReport)
	}

	// Folding the output again folds nothing, and the report lists nothing.
	again := filepath.Join(t.TempDir(), "again.ged")
	if code, stdout, _ = run(t, "repeated-names", "--fold", out, "-o", again); code != ExitOK || stdout != "" || readFixture(t, again) != got {
		t.Errorf("second fold: exit = %d, report %q, or the file changed", code, stdout)
	}
}

func TestRepeatedNamesFoldJSON(t *testing.T) {
	code, stdout, stderr := run(t, "repeated-names", "--fold", "--format", "json", repeatedNamesGED, "@I1@", "-o", filepath.Join(t.TempDir(), "out.ged"))
	if code != ExitOK || stderr != "" {
		t.Fatalf("exit = %d, stderr = %q", code, stderr)
	}
	var got repeatedFoldReport
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("stdout is not JSON: %v\n%s", err, stdout)
	}
	want := repeatedFoldReport{Folded: []repeatedFold{{XRef: "@I1@", mergedFold: mergedFold{
		Tag:   "NAME",
		Lines: []string{"1 NAME john  sample, jr", "2 SOUR @S1@", "3 PAGE 1", "2 SOUR @S1@", "3 PAGE 2"},
		Into:  []string{"1 NAME John /Sample/ Jr.", "2 GIVN John", "2 SURN Sample", "2 NSFX Jr.", "2 SOUR @S1@", "3 PAGE 1", "2 SOUR @S1@", "3 PAGE 2"},
	}}}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("json = %+v, want %+v", got, want)
	}
}

func TestRepeatedNamesErrors(t *testing.T) {
	out := filepath.Join(t.TempDir(), "out.ged")
	tests := []struct {
		name string
		args []string
		code int
		want string
	}{
		{"output without fold", []string{repeatedNamesGED, "-o", out}, ExitUsage, "add --fold"},
		{"fold without output", []string{"--fold", repeatedNamesGED}, ExitUsage, "--output (-o) is required"},
		{"output is the input", []string{"--fold", repeatedNamesGED, "-o", repeatedNamesGED}, ExitUsage, "is the input file"},
		{"no such record", []string{repeatedNamesGED, "@I9@"}, ExitError, "no such record: @I9@"},
		{"not an individual", []string{repeatedNamesGED, "@S1@"}, ExitError, "@S1@ is a SOUR record; want an individual"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code, stdout, stderr := run(t, append([]string{"repeated-names"}, tt.args...)...)
			if code != tt.code || stdout != "" || !strings.Contains(stderr, tt.want) {
				t.Errorf("exit = %d, stdout = %q, stderr = %q; want exit %d and %q", code, stdout, stderr, tt.code, tt.want)
			}
		})
	}
	if _, err := os.Stat(out); !os.IsNotExist(err) {
		t.Errorf("an error wrote %s", out)
	}
}

func TestRepeatedNamesHelp(t *testing.T) {
	code, stdout, _ := run(t, "repeated-names", "--help")
	if code != ExitOK {
		t.Fatalf("exit = %d", code)
	}
	for _, want := range []string{"repeated  xref, tag, lines...", "folded    xref, tag, lines, into",
		"words in the same order, case ignored", "NAME_PART_MISMATCH", "list --repeated"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("help lacks %q", want)
		}
	}
}
