package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const recordsFixture = "testdata/records.ged"

// TestDeleteRemovesOnlyTheRecord holds delete to CONSTITUTION.md C2: on every
// round-trip fixture, the output is the input without the deleted record's
// lines (and the blank lines before it), byte for byte.
func TestDeleteRemovesOnlyTheRecord(t *testing.T) {
	paths, err := filepath.Glob("testdata/roundtrip/*.ged")
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range paths {
		t.Run(filepath.Base(path), func(t *testing.T) {
			input, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			out := filepath.Join(t.TempDir(), "out.ged")
			code, _, stderr := run(t, "delete", path, "@I1@", "-o", out)
			if code != ExitOK {
				t.Fatalf("exit = %d, stderr = %q", code, stderr)
			}
			got, err := os.ReadFile(out)
			if err != nil {
				t.Fatal(err)
			}
			assertSameBytes(t, got, []byte(withoutRecord(t, string(input), "@I1@")))
		})
	}
}

// withoutRecord is src with the lines of the record xref cut out, along with
// the blank lines before it, which belong to its first line.
func withoutRecord(t *testing.T, src, xref string) string {
	t.Helper()
	lines := strings.SplitAfter(src, "\n")
	isLevel0 := func(i int) bool { return strings.HasPrefix(strings.TrimLeft(lines[i], " \uFEFF"), "0 ") }
	isBlank := func(i int) bool { return strings.TrimRight(lines[i], "\r\n") == "" }
	start := -1
	for i := range lines {
		if strings.HasPrefix(strings.TrimLeft(lines[i], " "), "0 "+xref+" ") {
			start = i
			break
		}
	}
	if start < 0 {
		t.Fatalf("no record %s in fixture", xref)
	}
	end := start + 1
	for end < len(lines) && !isLevel0(end) {
		end++
	}
	for end > start+1 && isBlank(end-1) {
		end--
	}
	for start > 0 && isBlank(start-1) {
		start--
	}
	return strings.Join(lines[:start], "") + strings.Join(lines[end:], "")
}

func TestDeleteText(t *testing.T) {
	out := filepath.Join(t.TempDir(), "out.ged")
	code, stdout, stderr := run(t, "delete", recordsFixture, "O1", "-o", out)
	if code != ExitOK || stderr != "" {
		t.Fatalf("exit = %d, stderr = %q", code, stderr)
	}
	if want := "wrote " + out + ": deleted @O1@\n"; stdout != want {
		t.Errorf("stdout = %q, want %q", stdout, want)
	}
	assertFileIs(t, out, withoutRecord(t, readFile(t, recordsFixture), "@O1@"))
}

func TestDeleteRefusesWhileReferenced(t *testing.T) {
	out := filepath.Join(t.TempDir(), "out.ged")
	code, stdout, stderr := run(t, "delete", recordsFixture, "@I2@", "-o", out)
	if code != ExitError {
		t.Fatalf("exit = %d, want %d (stderr %q)", code, ExitError, stderr)
	}
	want := out + " not written: pointers left to @I2@\n" +
		"references (1)\n" +
		"  @F1@\tFAM.HUSB\t@I2@\n"
	if stdout != want {
		t.Errorf("stdout = %q, want %q", stdout, want)
	}
	if !strings.Contains(stderr, "1 pointer left") || !strings.Contains(stderr, "--force") {
		t.Errorf("stderr = %q, want the pointer count and --force", stderr)
	}
	if _, err := os.Stat(out); !os.IsNotExist(err) {
		t.Errorf("output written despite the refusal (stat err %v)", err)
	}
}

func TestDeleteForceReportsDangling(t *testing.T) {
	out := filepath.Join(t.TempDir(), "out.ged")
	code, stdout, stderr := run(t, "delete", "--force", "--format", "json", recordsFixture, "I2", "U1", "-o", out)
	if code != ExitOK || stderr != "" {
		t.Fatalf("exit = %d, stderr = %q", code, stderr)
	}
	var got deleteReport
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("stdout is not JSON: %v\n%s", err, stdout)
	}
	want := []pointer{
		{From: "@F1@", Path: "FAM.HUSB", To: "@I2@"},
		{From: "", Path: "HEAD.SUBM", To: "@U1@"},
	}
	if !got.Written || strings.Join(got.Deleted, " ") != "@I2@ @U1@" || len(got.References) != len(want) {
		t.Fatalf("report = %+v", got)
	}
	for i := range want {
		if got.References[i] != want[i] {
			t.Errorf("references[%d] = %+v, want %+v", i, got.References[i], want[i])
		}
	}
	expected := withoutRecord(t, withoutRecord(t, readFile(t, recordsFixture), "@I2@"), "@U1@")
	assertFileIs(t, out, expected)
}

func TestDeleteForceText(t *testing.T) {
	out := filepath.Join(t.TempDir(), "out.ged")
	code, stdout, stderr := run(t, "delete", "--force", recordsFixture, "U1", "-o", out)
	if code != ExitOK || stderr != "" {
		t.Fatalf("exit = %d, stderr = %q", code, stderr)
	}
	want := "wrote " + out + ": deleted @U1@\n" +
		"dangling (1)\n" +
		"  -\tHEAD.SUBM\t@U1@\n"
	if stdout != want {
		t.Errorf("stdout = %q, want %q", stdout, want)
	}
}

// Pointers held by records deleted in the same command do not count.
func TestDeleteTogetherIgnoresPointersBetweenDeleted(t *testing.T) {
	out := filepath.Join(t.TempDir(), "out.ged")
	code, stdout, stderr := run(t, "delete", "--format", "json", recordsFixture, "I3", "F1", "I1", "I2", "I1", "-o", out)
	if code != ExitOK || stderr != "" {
		t.Fatalf("exit = %d, stderr = %q", code, stderr)
	}
	var got deleteReport
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("stdout is not JSON: %v\n%s", err, stdout)
	}
	if strings.Join(got.Deleted, " ") != "@I3@ @F1@ @I1@ @I2@" || len(got.References) != 0 {
		t.Errorf("report = %+v, want each record once and no references", got)
	}
}

func TestDeleteToStdout(t *testing.T) {
	code, stdout, stderr := run(t, "delete", recordsFixture, "O1", "-o", "-")
	if code != ExitOK {
		t.Fatalf("exit = %d, stderr = %q", code, stderr)
	}
	if want := withoutRecord(t, readFile(t, recordsFixture), "@O1@"); stdout != want {
		t.Errorf("stdout = %q, want %q", stdout, want)
	}
	if stderr != "wrote stdout: deleted @O1@\n" {
		t.Errorf("stderr = %q, want the report", stderr)
	}
}

func TestDeleteExitCodes(t *testing.T) {
	out := filepath.Join(t.TempDir(), "out.ged")
	tests := []struct {
		name string
		args []string
		want int
	}{
		{"missing -o", []string{"delete", recordsFixture, "O1"}, ExitUsage},
		{"-o is the input", []string{"delete", recordsFixture, "O1", "-o", recordsFixture}, ExitUsage},
		{"-o is the input by another path", []string{"delete", recordsFixture, "O1", "-o", "./testdata/../testdata/records.ged"}, ExitUsage},
		{"no xref", []string{"delete", recordsFixture, "-o", out}, ExitUsage},
		{"no such record", []string{"delete", recordsFixture, "O9", "-o", out}, ExitError},
		{"missing file", []string{"delete", "testdata/does-not-exist.ged", "O1", "-o", out}, ExitError},
		{"referenced", []string{"delete", recordsFixture, "R1", "-o", out}, ExitError},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code, _, stderr := run(t, tt.args...)
			if code != tt.want {
				t.Errorf("exit = %d, want %d (stderr %q)", code, tt.want, stderr)
			}
			if stderr == "" {
				t.Error("stderr is empty on failure")
			}
		})
	}
}

// A file defining an xref twice is refused: removing one copy leaves the
// other under the same xref, so the report would be wrong.
func TestDeleteRefusesDuplicateXRef(t *testing.T) {
	dir := t.TempDir()
	in := filepath.Join(dir, "dup.ged")
	text := "0 HEAD\n1 GEDC\n2 VERS 5.5.1\n1 CHAR UTF-8\n" +
		"0 @I1@ INDI\n1 NAME Ada /Example/\n0 @I1@ INDI\n1 NAME Ben /Example/\n0 TRLR\n"
	if err := os.WriteFile(in, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "out.ged")
	code, stdout, stderr := run(t, "delete", "--format", "json", in, "I1", "-o", out)
	if code != ExitError {
		t.Fatalf("exit = %d, want %d (stderr %q)", code, ExitError, stderr)
	}
	if stdout != "" || !strings.Contains(stderr, "more than once") {
		t.Errorf("stdout = %q, stderr = %q; want only the error", stdout, stderr)
	}
	if _, err := os.Stat(out); !os.IsNotExist(err) {
		t.Errorf("a refused delete wrote %s (stat err %v)", out, err)
	}
}

func assertFileIs(t *testing.T, path, want string) {
	t.Helper()
	assertSameBytes(t, []byte(readFile(t, path)), []byte(want))
}
