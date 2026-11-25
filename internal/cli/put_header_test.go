package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// headerFixture has a header mixed from two exports, as an old combine left
// it, and records with their own layout: an indented line and a blank line.
const headerFixture = "testdata/header-mixed.ged"

// mixedHeader is headerFixture's header, as get prints it and as the file
// has it.
const mixedHeader = "0 HEAD\n" +
	"1 SOUR Example Family Site\n2 VERS 2019.1\n2 NAME Example Family Site Trees\n" +
	"2 CORP Example Genealogy Inc.\n3 ADDR 1 Example Plaza\n4 CITY Sampletown\n" +
	"2 _TREE Sample Family Tree\n3 RIN 12345\n" +
	"1 DEST Other Example App\n1 DATE 2 JAN 2019\n2 TIME 10:20:30\n" +
	"1 FILE /home/someone/old/sample.ged\n1 COPR Copyright 2019 Example\n" +
	"1 GEDC\n2 VERS 5.5.1\n2 FORM LINEAGE-LINKED\n1 CHAR UTF-8\n1 LANG English\n" +
	"1 NOTE Written by Example Merger 3.0\n"

// ownHeader is the tree's own header for headerFixture: DEST, FILE, COPR and
// NOTE removed, SOUR and DATE changed, SUBM added.
const ownHeader = "0 HEAD\n" +
	"1 SOUR gedcom-cli-test\n2 VERS 1.0\n2 NAME Sample Tree\n" +
	"1 DATE 24 NOV 2026\n" +
	"1 GEDC\n2 VERS 5.5.1\n2 FORM LINEAGE-LINKED\n1 CHAR UTF-8\n1 LANG English\n" +
	"1 SUBM @U1@\n"

func TestPutHeaderUnchangedCopyIsByteIdentical(t *testing.T) {
	out := filepath.Join(t.TempDir(), "out.ged")
	head := getRecord(t, headerFixture, "HEAD")
	if head != mixedHeader {
		t.Fatalf("get HEAD = %q, want %q", head, mixedHeader)
	}
	code, stdout, stderr := run(t, "put-header", headerFixture, writeFragment(t, head), "-o", out)
	if code != ExitOK || stderr != "" {
		t.Fatalf("exit = %d, stderr = %q", code, stderr)
	}
	if want := "wrote " + out + ": header unchanged\n"; stdout != want {
		t.Errorf("stdout = %q, want %q", stdout, want)
	}
	assertFileIs(t, out, readFile(t, headerFixture))
}

// Lines are removed, changed and added in one run; get prints the header
// given, diff reports the header as the only change, and every other line is
// written as it was read.
func TestPutHeaderReplacesOnlyTheHeader(t *testing.T) {
	out := filepath.Join(t.TempDir(), "out.ged")
	code, stdout, stderr := run(t, "put-header", headerFixture, writeFragment(t, ownHeader), "-o", out)
	if code != ExitOK || stderr != "" {
		t.Fatalf("exit = %d, stderr = %q", code, stderr)
	}
	if want := "wrote " + out + ": header replaced\n"; stdout != want {
		t.Errorf("stdout = %q, want %q", stdout, want)
	}

	input := readFile(t, headerFixture)
	if !strings.HasPrefix(input, mixedHeader) {
		t.Fatal("the fixture does not start with mixedHeader")
	}
	assertFileIs(t, out, ownHeader+strings.TrimPrefix(input, mixedHeader))

	if got := getRecord(t, out, "HEAD"); got != ownHeader {
		t.Errorf("get HEAD = %q, want %q", got, ownHeader)
	}

	code, stdout, stderr = run(t, "diff", "--format", "json", "--include-header", headerFixture, out)
	if code != ExitOK {
		t.Fatalf("diff: exit = %d, stderr = %q", code, stderr)
	}
	var diff diffReport
	if err := json.Unmarshal([]byte(stdout), &diff); err != nil {
		t.Fatalf("diff stdout is not JSON: %v\n%s", err, stdout)
	}
	if diff.Header == nil || len(diff.Added)+len(diff.Removed)+len(diff.Changed) != 0 {
		t.Errorf("diff = %s, want the header as the only change", stdout)
	}
	_, stdout, _ = run(t, "diff", headerFixture, out)
	if want := headerFixture + " and " + out + ": no differences\n"; stdout != want {
		t.Errorf("diff without the header = %q, want no differences", stdout)
	}
}

func TestPutHeaderJSON(t *testing.T) {
	out := filepath.Join(t.TempDir(), "out.ged")
	code, stdout, stderr := run(t, "put-header", "--format", "json", headerFixture, writeFragment(t, ownHeader), "-o", out)
	if code != ExitOK || stderr != "" {
		t.Fatalf("exit = %d, stderr = %q", code, stderr)
	}
	var got putHeaderReport
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("stdout is not JSON: %v\n%s", err, stdout)
	}
	if !got.Written || !got.Replaced || got.Output != out || got.Input != headerFixture || len(got.Findings) != 0 {
		t.Errorf("report = %+v", got)
	}
	for _, key := range []string{`"replaced": true`, `"written": true`, `"findings": []`} {
		if !strings.Contains(stdout, key) {
			t.Errorf("stdout lacks %s:\n%s", key, stdout)
		}
	}
}

// The edit loop from the help, with the header on stdin and the output on
// stdout.
func TestPutHeaderStdinToStdout(t *testing.T) {
	edited := strings.Replace(getRecord(t, headerFixture, "HEAD"), "1 DEST Other Example App\n", "", 1)
	var out, errOut strings.Builder
	root := newRootCmd()
	root.SetArgs([]string{"put-header", headerFixture, "-", "-o", "-"})
	root.SetIn(strings.NewReader(edited))
	root.SetOut(&out)
	root.SetErr(&errOut)
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	if want := strings.Replace(readFile(t, headerFixture), "1 DEST Other Example App\n", "", 1); out.String() != want {
		t.Errorf("stdout = %q, want %q", out.String(), want)
	}
	if want := "wrote stdout: header replaced\n"; errOut.String() != want {
		t.Errorf("stderr = %q, want the report %q", errOut.String(), want)
	}
}

// A header that would change how the rest of the file is read is refused.
func TestPutHeaderRefusesVersionOrCharset(t *testing.T) {
	for _, tc := range []struct{ name, old, new, want string }{
		{"version changed", "2 VERS 5.5.1\n", "2 VERS 5.5\n", `GEDC.VERS "5.5" differs from the file's "5.5.1"`},
		{"version removed", "2 VERS 5.5.1\n", "", `GEDC.VERS absent differs from the file's "5.5.1"`},
		{"charset changed", "1 CHAR UTF-8\n", "1 CHAR ANSEL\n", `CHAR "ANSEL" differs from the file's "UTF-8"`},
		{"charset removed", "1 CHAR UTF-8\n", "", `CHAR absent differs from the file's "UTF-8"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out := filepath.Join(t.TempDir(), "out.ged")
			header := strings.Replace(ownHeader, tc.old, tc.new, 1)
			code, stdout, stderr := run(t, "put-header", headerFixture, writeFragment(t, header), "-o", out)
			if code != ExitError || stdout != "" {
				t.Fatalf("exit = %d, stdout = %q; want %d and no report", code, stdout, ExitError)
			}
			if !strings.Contains(stderr, tc.want) || !strings.Contains(stderr, "gedcom convert") {
				t.Errorf("stderr = %q, want %q and a pointer to convert", stderr, tc.want)
			}
			if _, err := os.Stat(out); !os.IsNotExist(err) {
				t.Errorf("output written despite the refusal (stat err %v)", err)
			}
		})
	}
}

// Decode problems in the header are reported as put reports them, and an
// error stops the write.
func TestPutHeaderErrorsWriteNothing(t *testing.T) {
	out := filepath.Join(t.TempDir(), "out.ged")
	header := writeFragment(t, strings.Replace(ownHeader, "1 DATE", "not a GEDCOM line\n1 DATE", 1))
	code, stdout, stderr := run(t, "put-header", headerFixture, header, "-o", out)
	if code != ExitError {
		t.Fatalf("exit = %d, want %d (stderr %q)", code, ExitError, stderr)
	}
	want := out + " not written\n" +
		"error (1)\n" +
		"  line 5\tINVALID_LEVEL\tinvalid level number (context: \"not a GEDCOM line\")\tdecode\n"
	if stdout != want {
		t.Errorf("stdout = %q, want %q", stdout, want)
	}
	if !strings.Contains(stderr, "1 error in "+header) {
		t.Errorf("stderr = %q, want the error count", stderr)
	}
	if _, err := os.Stat(out); !os.IsNotExist(err) {
		t.Errorf("output written despite the error (stat err %v)", err)
	}

	code, stdout, _ = run(t, "put-header", "--format", "json", headerFixture, header, "-o", out)
	if code != ExitError {
		t.Fatalf("json: exit = %d, want %d", code, ExitError)
	}
	var got putHeaderReport
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("stdout is not JSON: %v\n%s", err, stdout)
	}
	if got.Written || got.Replaced || got.Counts.Error != 1 || got.Findings[0].Line != 5 {
		t.Errorf("report = %+v, want one error at line 5, nothing written", got)
	}
}

// A warning is reported, and the header is still written.
func TestPutHeaderWarningsWrite(t *testing.T) {
	out := filepath.Join(t.TempDir(), "out.ged")
	header := strings.Replace(ownHeader, "1 LANG English\n", "1 LANG English\n\n", 1)
	code, stdout, stderr := run(t, "put-header", headerFixture, writeFragment(t, header), "-o", out)
	if code != ExitOK || stderr != "" {
		t.Fatalf("exit = %d, stderr = %q", code, stderr)
	}
	want := "wrote " + out + ": header replaced\n" +
		"warning (1)\n" +
		"  line 11\tEMPTY_LINE\tempty line\tdecode\n"
	if stdout != want {
		t.Errorf("stdout = %q, want %q", stdout, want)
	}
	if got := getRecord(t, out, "HEAD"); got != ownHeader {
		t.Errorf("get HEAD = %q, want %q", got, ownHeader)
	}
}

func TestPutHeaderExitCodes(t *testing.T) {
	out := filepath.Join(t.TempDir(), "out.ged")
	header := writeFragment(t, ownHeader)
	tests := []struct {
		name string
		args []string
		want int
	}{
		{"missing -o", []string{"put-header", headerFixture, header}, ExitUsage},
		{"-o is the input", []string{"put-header", headerFixture, header, "-o", headerFixture}, ExitUsage},
		{"-o is the header file", []string{"put-header", headerFixture, header, "-o", header}, ExitUsage},
		{"missing header file", []string{"put-header", headerFixture, "-o", out}, ExitUsage},
		{"header file not found", []string{"put-header", headerFixture, "testdata/does-not-exist.ged", "-o", out}, ExitError},
		{"input not found", []string{"put-header", "testdata/does-not-exist.ged", header, "-o", out}, ExitError},
		{"a whole file", []string{"put-header", headerFixture, headerFixture, "-o", out}, ExitError},
		{"header and a record", []string{"put-header", headerFixture, writeFragment(t, ownHeader+"0 @I9@ INDI\n"), "-o", out}, ExitError},
		{"header and a trailer", []string{"put-header", headerFixture, writeFragment(t, ownHeader+"0 TRLR\n"), "-o", out}, ExitError},
		{"a record", []string{"put-header", headerFixture, writeFragment(t, getRecord(t, headerFixture, "@I1@")), "-o", out}, ExitError},
		{"lines before HEAD", []string{"put-header", headerFixture, writeFragment(t, "1 DEST x\n"+ownHeader), "-o", out}, ExitError},
		{"empty", []string{"put-header", headerFixture, writeFragment(t, ""), "-o", out}, ExitError},
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
		t.Errorf("a failed put-header wrote %s (stat err %v)", out, err)
	}
}

// get and put name put-header for the header.
func TestPutHeaderNamedInHelp(t *testing.T) {
	for _, cmd := range []string{"get", "put"} {
		_, stdout, _ := run(t, cmd, "--help")
		if !strings.Contains(stdout, "'gedcom put-header'") {
			t.Errorf("%s --help does not name put-header:\n%s", cmd, stdout)
		}
	}
	_, stdout, _ := run(t, "put-header", "--help")
	for _, want := range []string{"gedcom get family.ged HEAD > head.ged", "GEDC.VERS", "'gedcom convert'"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("help missing %q:\n%s", want, stdout)
		}
	}
}
