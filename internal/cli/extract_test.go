package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// extractFixture is two families around Ada (@I1@): her parents' (@F1@) and
// her own (@F2@, with Dan and their child Eve). Ada cites a source with a
// repository, a note and a media object; her mother cites a source of her own.
const extractFixture = "testdata/extract.ged"

// extractWant is the bounded subset of Ada and Eve: the parents and Dan are
// cut, and so is the source only the mother cites.
const extractWant = `0 HEAD
1 SOUR gedcom-cli-test
1 GEDC
2 VERS 5.5.1
2 FORM LINEAGE-LINKED
1 CHAR UTF-8
1 SUBM @U1@
0 @U1@ SUBM
1 NAME Test Submitter
1 ADDR 1 Example Street
1 PHON 555-0100
1 EMAIL submitter@example.com
0 @I1@ INDI
1 NAME Ada /Example/
1 SEX F
1 SOUR @S1@
2 PAGE entry 4
1 NOTE @N1@
1 OBJE @O1@
1 FAMC @F1@
1 FAMS @F2@
0 @I5@ INDI
1 NAME Eve /Example/
1 SEX F
1 FAMC @F2@
0 @F1@ FAM
1 CHIL @I1@
0 @F2@ FAM
1 WIFE @I1@
1 CHIL @I5@
0 @S1@ SOUR
1 TITL Example Parish Register
1 REPO @R1@
0 @R1@ REPO
1 NAME Example Archive
0 @N1@ NOTE Research note about Ada
0 @O1@ OBJE
1 FILE photos/ada.jpg
2 FORM jpg
0 TRLR
`

const extractWarnings = `gedcom: warning: dropped pointer @F1@ HUSB @I2@ (restorable)
gedcom: warning: dropped pointer @F1@ WIFE @I3@ (restorable)
gedcom: warning: dropped pointer @F2@ HUSB @I4@ (restorable)
`

func TestExtractText(t *testing.T) {
	out := filepath.Join(t.TempDir(), "branch.ged")
	code, stdout, stderr := run(t, "extract", extractFixture, "@I1@", "I5", "-o", out)
	if code != ExitOK {
		t.Fatalf("exit = %d, stderr = %q", code, stderr)
	}
	if want := "wrote " + out + ": 9 records, 2 individuals, 2 families, 3 pointers dropped\n"; stdout != want {
		t.Errorf("stdout = %q, want %q", stdout, want)
	}
	if stderr != extractWarnings {
		t.Errorf("stderr = %q, want %q", stderr, extractWarnings)
	}
	if got := fileText(t, out); got != extractWant {
		t.Errorf("output:\n%s\nwant:\n%s", got, extractWant)
	}

	// No dangling pointers: the output validates clean.
	code, stdout, stderr = run(t, "validate", out)
	if code != ExitOK || stderr != "" || !strings.HasSuffix(stdout, ": no findings\n") {
		t.Errorf("validate: exit = %d, stdout = %q, stderr = %q", code, stdout, stderr)
	}
}

func TestExtractJSON(t *testing.T) {
	out := filepath.Join(t.TempDir(), "branch.ged")
	code, stdout, stderr := run(t, "extract", "--format", "json", extractFixture, "I1", "I5", "-o", out)
	if code != ExitOK {
		t.Fatalf("exit = %d, stderr = %q", code, stderr)
	}
	if stderr != extractWarnings {
		t.Errorf("stderr = %q, want %q", stderr, extractWarnings)
	}
	var got extractReport
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("stdout is not JSON: %v\n%s", err, stdout)
	}
	want := extractReport{
		Output: out, Records: 9, Individuals: 2, Families: 2,
		Dropped: []droppedPointer{
			{XRef: "@F1@", Tag: "HUSB", Target: "@I2@", Restorable: true},
			{XRef: "@F1@", Tag: "WIFE", Target: "@I3@", Restorable: true},
			{XRef: "@F2@", Tag: "HUSB", Target: "@I4@", Restorable: true},
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("report = %+v, want %+v", got, want)
	}
	for _, key := range []string{`"output"`, `"records"`, `"individuals"`, `"families"`, `"dropped"`, `"xref"`, `"tag"`, `"target"`, `"restorable"`} {
		if !strings.Contains(stdout, key) {
			t.Errorf("JSON lacks key %s:\n%s", key, stdout)
		}
	}
}

// The composition from extract's help: descendants' xrefs on stdin, with
// "-o -" putting the GEDCOM on stdout and the report on stderr.
func TestExtractStdin(t *testing.T) {
	code, xrefs, errOut := run(t, "descendants", "--format", "json", extractFixture, "I1")
	if code != ExitOK {
		t.Fatalf("descendants: exit = %d, stderr = %q", code, errOut)
	}
	var rows []struct{ XRef string }
	if err := json.Unmarshal([]byte(xrefs), &rows); err != nil {
		t.Fatal(err)
	}
	var lines strings.Builder
	for _, r := range rows {
		lines.WriteString(r.XRef + "\n")
	}

	code, stdout, stderr := runStdin(t, lines.String(), "extract", extractFixture, "I1", "-", "-o", "-")
	if code != ExitOK {
		t.Fatalf("exit = %d, stderr = %q", code, stderr)
	}
	if stdout != extractWant {
		t.Errorf("stdout:\n%s\nwant:\n%s", stdout, extractWant)
	}
	if want := extractWarnings + "wrote stdout: 9 records, 2 individuals, 2 families, 3 pointers dropped\n"; stderr != want {
		t.Errorf("stderr = %q, want %q", stderr, want)
	}
}

// With "-o -" and --format json, stderr holds only the JSON report: its
// dropped list stands in for the warning lines.
func TestExtractStdoutJSON(t *testing.T) {
	code, stdout, stderr := run(t, "extract", "--format", "json", extractFixture, "I1", "I5", "-o", "-")
	if code != ExitOK {
		t.Fatalf("exit = %d, stderr = %q", code, stderr)
	}
	if stdout != extractWant {
		t.Errorf("stdout:\n%s\nwant:\n%s", stdout, extractWant)
	}
	var got extractReport
	if err := json.Unmarshal([]byte(stderr), &got); err != nil {
		t.Fatalf("stderr is not one JSON document: %v\n%s", err, stderr)
	}
	if got.Output != "stdout" || len(got.Dropped) != 3 {
		t.Errorf("report = %+v", got)
	}
}

// An output file replaced keeps its permissions; a new one is not left 0600.
func TestExtractOutputMode(t *testing.T) {
	dir := t.TempDir()
	kept := filepath.Join(dir, "kept.ged")
	if err := os.WriteFile(kept, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(kept, 0o600); err != nil {
		t.Fatal(err)
	}
	fresh := filepath.Join(dir, "fresh.ged")
	for _, out := range []string{kept, fresh} {
		if code, _, stderr := run(t, "extract", extractFixture, "I1", "-o", out); code != ExitOK {
			t.Fatalf("%s: exit = %d, stderr = %q", out, code, stderr)
		}
	}
	if info, err := os.Stat(kept); err != nil || info.Mode().Perm() != 0o600 {
		t.Errorf("replaced file mode = %v, %v; want 0600", info.Mode().Perm(), err)
	}
	probe := filepath.Join(dir, "probe")
	f, err := os.Create(probe)
	if err != nil {
		t.Fatal(err)
	}
	_ = f.Close()
	want, _ := os.Stat(probe)
	if info, err := os.Stat(fresh); err != nil || info.Mode().Perm() != want.Mode().Perm() {
		t.Errorf("new file mode = %v, %v; want %v as os.Create gives", info.Mode().Perm(), err, want.Mode().Perm())
	}
}

func TestExtractStripSubmitter(t *testing.T) {
	contact := []string{"Test Submitter", "Example Street", "555-0100", "submitter@example.com"}
	for _, strip := range []bool{false, true} {
		args := []string{"extract", extractFixture, "I5", "-o", "-"}
		if strip {
			args = append(args, "--strip-submitter")
		}
		code, stdout, stderr := run(t, args...)
		if code != ExitOK {
			t.Fatalf("strip %v: exit = %d, stderr = %q", strip, code, stderr)
		}
		for _, detail := range contact {
			if strings.Contains(stdout, detail) == strip {
				t.Errorf("strip %v: output contains %q = %v:\n%s", strip, detail, !strip, stdout)
			}
		}
		if strip && !strings.Contains(stdout, "1 SUBM @U1@\n0 @U1@ SUBM\n1 NAME "+submitterValue+"\n") {
			t.Errorf("stripped submitter is not kept as a named record:\n%s", stdout)
		}
	}
}

// Redaction happens before the cut: a note only a living person cites is not
// carried into the output.
func TestExtractRedactLiving(t *testing.T) {
	code, stdout, stderr := run(t, "--redact-living", "--reference-year", referenceYear,
		"extract", living, "I3", "-o", "-")
	if code != ExitOK {
		t.Fatalf("exit = %d, stderr = %q", code, stderr)
	}
	if strings.Contains(stdout, "@N1@") {
		t.Errorf("output keeps the living person's note record:\n%s", stdout)
	}
	out := filepath.Join(t.TempDir(), "share.ged")
	if err := os.WriteFile(out, []byte(stdout), 0o644); err != nil {
		t.Fatal(err)
	}
	if code, stdout, stderr := run(t, "validate", out); code != ExitOK || !strings.HasSuffix(stdout, ": no findings\n") {
		t.Errorf("validate: exit = %d, stdout = %q, stderr = %q", code, stdout, stderr)
	}
}

func TestExtractErrors(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "out.ged")
	input := filepath.Join(dir, "in.ged")
	if err := os.WriteFile(input, []byte(fileText(t, extractFixture)), 0o644); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name string
		args []string
		want int
	}{
		{"no output", []string{"extract", extractFixture, "I1"}, ExitUsage},
		{"no xref", []string{"extract", extractFixture, "-o", out}, ExitUsage},
		{"output is input", []string{"extract", input, "I1", "-o", input}, ExitUsage},
		{"stdin twice", []string{"extract", extractFixture, "-", "-", "-o", out}, ExitUsage},
		{"unknown xref", []string{"extract", extractFixture, "I1", "I99", "-o", out}, ExitError},
		{"not an individual", []string{"extract", extractFixture, "F1", "-o", out}, ExitError},
		{"empty stdin", []string{"extract", extractFixture, "-", "-o", out}, ExitError},
		{"missing file", []string{"extract", "testdata/does-not-exist.ged", "I1", "-o", out}, ExitError},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code, stdout, stderr := runStdin(t, "", tt.args...)
			if code != tt.want {
				t.Errorf("exit = %d, want %d (stderr %q)", code, tt.want, stderr)
			}
			if stdout != "" || stderr == "" {
				t.Errorf("stdout = %q, stderr = %q; want only an error", stdout, stderr)
			}
			if _, err := os.Stat(out); !os.IsNotExist(err) {
				t.Errorf("output was written: %v", err)
			}
			if got := fileText(t, input); got != fileText(t, extractFixture) {
				t.Error("input file was changed")
			}
		})
	}
}

func TestExtractHelp(t *testing.T) {
	_, stdout, _ := run(t, "extract", "--help")
	for _, want := range []string{
		"gedcom descendants --format json tree.ged I1 | jq -r '.[].xref' | gedcom extract tree.ged - -o branch.ged",
		"--strip-submitter",
		"carries the submitter's name, address, phone and email",
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("help lacks %q:\n%s", want, stdout)
		}
	}
}

// runStdin is run with stdin as the command's standard input.
func runStdin(t *testing.T, stdin string, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	var out, errOut strings.Builder
	code = runWithStdin(args, strings.NewReader(stdin), &out, &errOut)
	return code, out.String(), errOut.String()
}

// fileText is the content of the file at path.
func fileText(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// extractEveryone is extract's run with no change requested: every individual
// in the file, cut into a file of its own.
func extractEveryone(_ *testing.T, in, out string) []string {
	args := []string{"extract", in}
	res, err := decodeForWrite(in)
	if err != nil {
		panic(fmt.Sprintf("extractEveryone: %v", err))
	}
	for _, ind := range res.Document.Individuals() {
		args = append(args, ind.XRef)
	}
	return append(args, "-o", out)
}
