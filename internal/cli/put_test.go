package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeFragment writes text to a records file in a temporary directory.
func writeFragment(t *testing.T, text string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "records.ged")
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func getRecord(t *testing.T, path, xref string) string {
	t.Helper()
	code, stdout, stderr := run(t, "get", path, xref)
	if code != ExitOK {
		t.Fatalf("get: exit = %d, stderr = %q", code, stderr)
	}
	return stdout
}

func TestPutUnchangedCopyIsByteIdentical(t *testing.T) {
	out := filepath.Join(t.TempDir(), "out.ged")
	fragment := writeFragment(t, getRecord(t, recordsFixture, "@I1@"))
	code, stdout, stderr := run(t, "put", recordsFixture, fragment, "-o", out)
	if code != ExitOK || stderr != "" {
		t.Fatalf("exit = %d, stderr = %q", code, stderr)
	}
	want := "wrote " + out + ": 0 replaced, 0 added, 1 unchanged\n  unchanged\t@I1@\n"
	if stdout != want {
		t.Errorf("stdout = %q, want %q", stdout, want)
	}
	assertFileIs(t, out, readFile(t, recordsFixture))
}

// After put of an edited record, the output differs from the input only in
// that record's lines.
func TestPutEditedRecordChangesOnlyItsLines(t *testing.T) {
	out := filepath.Join(t.TempDir(), "out.ged")
	original := getRecord(t, recordsFixture, "@I1@")
	edited := strings.Replace(original, "1 NAME Cora /Sample/\n", "1 NAME Cora /Sample/\n2 NICK Corrie\n", 1)
	edited = strings.Replace(edited, "2 DATE 14 NOV 1978\n", "2 DATE 15 NOV 1978\n", 1)
	code, stdout, stderr := run(t, "put", recordsFixture, writeFragment(t, edited), "-o", out)
	if code != ExitOK || stderr != "" {
		t.Fatalf("exit = %d, stderr = %q", code, stderr)
	}
	if want := "wrote " + out + ": 1 replaced, 0 added, 0 unchanged\n  replaced\t@I1@\n"; stdout != want {
		t.Errorf("stdout = %q, want %q", stdout, want)
	}
	input := readFile(t, recordsFixture)
	if !strings.Contains(input, original) {
		t.Fatal("get output is not the fixture's lines for @I1@")
	}
	assertFileIs(t, out, strings.Replace(input, original, edited, 1))
}

// A replaced record is written as its new lines, without the blank line
// before it; every other record keeps its layout.
func TestPutKeepsOtherRecordsLayout(t *testing.T) {
	in := "testdata/roundtrip/blank-lines.ged"
	out := filepath.Join(t.TempDir(), "out.ged")
	code, _, stderr := run(t, "put", in, writeFragment(t, "0 @I2@ INDI\n1 NAME Bea /Example/\n"), "-o", out)
	if code != ExitOK {
		t.Fatalf("exit = %d, stderr = %q", code, stderr)
	}
	want := strings.Replace(readFile(t, in), "\n\n0 @I2@ INDI\n1 NAME Ben /Example/\n", "\n0 @I2@ INDI\n1 NAME Bea /Example/\n", 1)
	assertFileIs(t, out, want)
}

func TestPutAddsNewRecordsJSON(t *testing.T) {
	out := filepath.Join(t.TempDir(), "out.ged")
	fragment := "0 @I9@ INDI\n1 NAME Hal /Example/\n0 @I1@ INDI\n1 NAME Cora /Changed/\n"
	code, stdout, stderr := run(t, "put", "--format", "json", recordsFixture, writeFragment(t, fragment), "-o", out)
	if code != ExitOK || stderr != "" {
		t.Fatalf("exit = %d, stderr = %q", code, stderr)
	}
	var got putReport
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("stdout is not JSON: %v\n%s", err, stdout)
	}
	if !got.Written || strings.Join(got.Added, " ") != "@I9@" || strings.Join(got.Replaced, " ") != "@I1@" ||
		len(got.Unchanged) != 0 || len(got.Findings) != 0 || got.Output != out {
		t.Errorf("report = %+v", got)
	}
	written := readFile(t, out)
	if !strings.HasSuffix(written, "0 @I9@ INDI\n1 NAME Hal /Example/\n0 TRLR\n") {
		t.Errorf("new record not appended before the trailer:\n%s", written)
	}
	if !strings.Contains(written, "0 @I1@ INDI\n1 NAME Cora /Changed/\n0 @I2@ INDI\n") {
		t.Errorf("@I1@ not replaced in place:\n%s", written)
	}
}

// The edit loop from put's help, with the fragment on stdin and the output
// on stdout.
func TestPutStdinToStdout(t *testing.T) {
	edited := strings.Replace(getRecord(t, recordsFixture, "@I3@"), "Edie", "Edith", 1)
	var out, errOut strings.Builder
	root := newRootCmd()
	root.SetArgs([]string{"put", recordsFixture, "-", "-o", "-"})
	root.SetIn(strings.NewReader(edited))
	root.SetOut(&out)
	root.SetErr(&errOut)
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	if want := strings.Replace(readFile(t, recordsFixture), "Edie", "Edith", 1); out.String() != want {
		t.Errorf("stdout = %q, want %q", out.String(), want)
	}
	if want := "wrote stdout: 1 replaced, 0 added, 0 unchanged\n  replaced\t@I3@\n"; errOut.String() != want {
		t.Errorf("stderr = %q, want the report %q", errOut.String(), want)
	}
}

// Decode problems in the fragment are reported as validate reports them, and
// an error stops the write.
func TestPutFragmentErrorsWriteNothing(t *testing.T) {
	out := filepath.Join(t.TempDir(), "out.ged")
	fragment := writeFragment(t, "0 @I1@ INDI\nnot a GEDCOM line\n1 NAME Cora /Changed/\n")
	code, stdout, stderr := run(t, "put", recordsFixture, fragment, "-o", out)
	if code != ExitError {
		t.Fatalf("exit = %d, want %d (stderr %q)", code, ExitError, stderr)
	}
	want := out + " not written\n" +
		"error (1)\n" +
		"  line 2\tINVALID_LEVEL\tinvalid level number (context: \"not a GEDCOM line\")\tdecode\n"
	if stdout != want {
		t.Errorf("stdout = %q, want %q", stdout, want)
	}
	if !strings.Contains(stderr, "1 error in "+fragment) {
		t.Errorf("stderr = %q, want the error count", stderr)
	}
	if _, err := os.Stat(out); !os.IsNotExist(err) {
		t.Errorf("output written despite the error (stat err %v)", err)
	}
}

func TestPutFragmentErrorsJSON(t *testing.T) {
	out := filepath.Join(t.TempDir(), "out.ged")
	fragment := writeFragment(t, "0 @I9@ INDI\n1 NAME A\n0 @I9@ INDI\n1 NAME B\n")
	code, stdout, _ := run(t, "put", "--format", "json", recordsFixture, fragment, "-o", out)
	if code != ExitError {
		t.Fatalf("exit = %d, want %d", code, ExitError)
	}
	var got putReport
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("stdout is not JSON: %v\n%s", err, stdout)
	}
	if got.Written || got.Counts.Error != 1 || len(got.Findings) != 1 ||
		got.Findings[0].Code != "DUPLICATE_XREF" || got.Findings[0].Source != sourceDecode || got.Findings[0].Line != 3 {
		t.Errorf("report = %+v, want one DUPLICATE_XREF decode error at line 3", got)
	}
}

func TestPutExitCodes(t *testing.T) {
	out := filepath.Join(t.TempDir(), "out.ged")
	fragment := writeFragment(t, "0 @I9@ INDI\n1 NAME Hal /Example/\n")
	tests := []struct {
		name string
		args []string
		want int
	}{
		{"missing -o", []string{"put", recordsFixture, fragment}, ExitUsage},
		{"-o is the input", []string{"put", recordsFixture, fragment, "-o", recordsFixture}, ExitUsage},
		{"-o is the records file", []string{"put", recordsFixture, fragment, "-o", fragment}, ExitUsage},
		{"missing records file", []string{"put", recordsFixture, "-o", out}, ExitUsage},
		{"records file not found", []string{"put", recordsFixture, "testdata/does-not-exist.ged", "-o", out}, ExitError},
		{"input not found", []string{"put", "testdata/does-not-exist.ged", fragment, "-o", out}, ExitError},
		{"not a fragment", []string{"put", recordsFixture, recordsFixture, "-o", out}, ExitError},
		{"record without xref", []string{"put", recordsFixture, writeFragment(t, "0 NOTE loose\n"), "-o", out}, ExitError},
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
	if _, err := os.Stat(out); !os.IsNotExist(err) {
		t.Errorf("a failed put wrote %s (stat err %v)", out, err)
	}
}

func TestPutHelpShowsEditLoop(t *testing.T) {
	code, stdout, _ := run(t, "put", "--help")
	if code != ExitOK {
		t.Fatalf("exit = %d", code)
	}
	for _, want := range []string{"gedcom get family.ged @I1@ > i1.ged", "gedcom put family.ged i1.ged -o"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("help missing %q:\n%s", want, stdout)
		}
	}
}
