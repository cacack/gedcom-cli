package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"
)

// anselFixture is a GEDCOM 5.5 file in ANSEL. ANSEL writes a combining
// diacritic before its base letter: 0xE2 is the acute accent, 0xE8 the
// diaeresis and 0xE4 the tilde, so "Ren\xe2ee" reads as "Renee" with an
// acute accent on the first e.
const anselFixture = "0 HEAD\n" +
	"1 SOUR gedcom-cli-test\n" +
	"1 GEDC\n" +
	"2 VERS 5.5\n" +
	"2 FORM LINEAGE-LINKED\n" +
	"1 CHAR ANSEL\n" +
	"0 @I1@ INDI\n" +
	"1 NAME Ren\xe2ee /Exampl\xe8e/\n" +
	"1 BIRT\n" +
	"2 DATE 3 MAR 1901\n" +
	"2 PLAC Espa\xe4na\n" +
	"1 DEAT\n" +
	"2 DATE 14 NOV 1978\n" +
	"1 FAMS @F1@\n" +
	"0 @I2@ INDI\n" +
	"1 NAME Jos\xe2e /Example/\n" +
	"1 BIRT\n" +
	"2 DATE ABT 1899\n" +
	"1 FAMS @F1@\n" +
	"0 @I3@ INDI\n" +
	"1 NAME Zo\xe8e /Example/\n" +
	"1 FAMC @F1@\n" +
	"0 @F1@ FAM\n" +
	"1 HUSB @I2@\n" +
	"1 WIFE @I1@\n" +
	"1 CHIL @I3@\n" +
	"1 MARR\n" +
	"2 DATE 12 JUN 1923\n" +
	"0 TRLR\n"

// lossy is a GEDCOM 7.0 file whose NO, EXID and SCHMA structures GEDCOM 5.5.1
// cannot hold.
const lossy = "testdata/convert-lossy.ged"

func writeTemp(t *testing.T, name, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestConvertANSELTo551(t *testing.T) {
	in := writeTemp(t, "ansel.ged", anselFixture)
	out := filepath.Join(t.TempDir(), "out.ged")

	code, stdout, stderr := run(t, "convert", in, "--to", "5.5.1", "-o", out)
	if code != ExitOK || stderr != "" {
		t.Fatalf("exit = %d, stderr = %q", code, stderr)
	}
	if want := in + ": 5.5 -> 5.5.1, wrote " + out + "\n"; !strings.HasPrefix(stdout, want) {
		t.Errorf("stdout does not start with %q:\n%s", want, stdout)
	}

	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if !utf8.Valid(data) {
		t.Fatalf("output is not UTF-8:\n%q", data)
	}
	// The library decodes ANSEL to a base letter followed by a combining mark
	// (Unicode NFD): "e\u0301" is e with an acute accent.
	for _, want := range []string{
		"2 VERS 5.5.1\n",
		"1 CHAR UTF-8\n",
		"1 NAME Rene\u0301e /Example\u0308/\n",
		"2 PLAC Espan\u0303a\n",
		"1 NAME Zoe\u0308 /Example/\n",
	} {
		if !strings.Contains(string(data), want) {
			t.Errorf("output missing %q:\n%s", want, data)
		}
	}

	// The converted file lists the same people, families and events.
	for _, kind := range []string{"indi", "fam"} {
		_, before, _ := run(t, "list", in, kind)
		code, after, stderr := run(t, "list", out, kind)
		if code != ExitOK || stderr != "" {
			t.Fatalf("list %s: exit = %d, stderr = %q", kind, code, stderr)
		}
		if after != before || before == "" {
			t.Errorf("list %s differs:\nbefore\n%s\nafter\n%s", kind, before, after)
		}
	}
}

func TestConvertReportsDataLossText(t *testing.T) {
	out := filepath.Join(t.TempDir(), "out.ged")
	code, stdout, stderr := run(t, "convert", lossy, "--to", "5.5.1", "-o", out)
	if code != ExitOK || stderr != "" {
		t.Fatalf("exit = %d, stderr = %q", code, stderr)
	}
	for _, want := range []string{
		"data loss (3)\n",
		"  NO tags\tTag not supported in GEDCOM 5.5.1\t@I1@\n",
		"  EXID tags\tTag not supported in GEDCOM 5.5.1\t@I1@\n",
		"  SCHMA schema definitions\t",
		"dropped (2)\n",
		"  Individual @I1@ > NO\tNO\t\tTag not supported in GEDCOM 5.5.1\n",
		"  Individual @I1@ > EXID\tEXID\t\tTag not supported in GEDCOM 5.5.1\n",
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("stdout missing %q:\n%s", want, stdout)
		}
	}
	if _, err := os.Stat(out); err != nil {
		t.Errorf("without --fail-on-loss the output is written: %v", err)
	}
}

func TestConvertReportsDataLossJSON(t *testing.T) {
	out := filepath.Join(t.TempDir(), "out.ged")
	code, stdout, stderr := run(t, "convert", "--format", "json", lossy, "--to", "5.5.1", "-o", out)
	if code != ExitOK || stderr != "" {
		t.Fatalf("exit = %d, stderr = %q", code, stderr)
	}
	var got convertReport
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("stdout is not JSON: %v\n%s", err, stdout)
	}
	if !got.Written || got.SourceVersion != "7.0" || got.TargetVersion != "5.5.1" || got.Output != out {
		t.Errorf("unexpected report header: %+v", got)
	}
	features := map[string]bool{}
	for _, d := range got.DataLoss {
		features[d.Feature] = true
	}
	for _, want := range []string{"NO tags", "EXID tags", "SCHMA schema definitions"} {
		if !features[want] {
			t.Errorf("dataLoss missing %q: %+v", want, got.DataLoss)
		}
	}
	if len(got.Dropped) != 2 || got.Dropped[0].Path != "Individual @I1@ > NO" {
		t.Errorf("dropped = %+v", got.Dropped)
	}
}

func TestConvertFailOnLoss(t *testing.T) {
	for _, format := range []string{formatText, formatJSON} {
		t.Run(format, func(t *testing.T) {
			out := filepath.Join(t.TempDir(), "out.ged")
			code, stdout, stderr := run(t, "convert", "--fail-on-loss", "--format", format, lossy, "--to", "5.5.1", "-o", out)
			if code != ExitError {
				t.Fatalf("exit = %d, want %d (stderr %q)", code, ExitError, stderr)
			}
			if !strings.Contains(stdout, "EXID tags") {
				t.Errorf("the report is still printed; stdout:\n%s", stdout)
			}
			if !strings.Contains(stderr, "would lose data (3 items)") || strings.Count(stderr, "\n") != 1 {
				t.Errorf("stderr = %q, want one line naming the loss", stderr)
			}
			if _, err := os.Stat(out); !os.IsNotExist(err) {
				t.Errorf("output was written despite --fail-on-loss (stat err %v)", err)
			}
		})
	}
}

func TestConvertFailOnLossWithoutLoss(t *testing.T) {
	in := writeTemp(t, "ansel.ged", anselFixture)
	out := filepath.Join(t.TempDir(), "out.ged")
	code, _, stderr := run(t, "convert", "--fail-on-loss", in, "--to", "7.0", "-o", out)
	if code != ExitOK || stderr != "" {
		t.Fatalf("exit = %d, stderr = %q", code, stderr)
	}
	if _, err := os.Stat(out); err != nil {
		t.Errorf("output not written: %v", err)
	}
}

func TestConvertSameVersionByteIdentical(t *testing.T) {
	input, err := os.ReadFile(records)
	if err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(t.TempDir(), "out.ged")
	code, stdout, stderr := run(t, "convert", records, "--to", "5.5.1", "-o", out)
	if code != ExitOK || stderr != "" {
		t.Fatalf("exit = %d, stderr = %q", code, stderr)
	}
	if strings.Contains(stdout, "data loss") {
		t.Errorf("same-version conversion reported loss:\n%s", stdout)
	}
	got, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	assertSameBytes(t, got, input)
}

func TestConvertExitCodes(t *testing.T) {
	out := filepath.Join(t.TempDir(), "out.ged")
	tests := []struct {
		name string
		args []string
		want int
	}{
		{"missing --to", []string{"convert", records, "-o", out}, ExitUsage},
		{"unsupported --to", []string{"convert", records, "--to", "5.5", "-o", out}, ExitUsage},
		{"missing -o", []string{"convert", records, "--to", "7.0"}, ExitUsage},
		{"output is the input", []string{"convert", records, "--to", "7.0", "-o", "./" + records}, ExitUsage},
		{"no file", []string{"convert", "--to", "7.0", "-o", out}, ExitUsage},
		{"missing file", []string{"convert", "testdata/does-not-exist.ged", "--to", "7.0", "-o", out}, ExitError},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code, stdout, stderr := run(t, tt.args...)
			if code != tt.want {
				t.Errorf("exit = %d, want %d (stderr %q)", code, tt.want, stderr)
			}
			if stdout != "" || strings.Count(stderr, "\n") != 1 {
				t.Errorf("stdout = %q, stderr = %q: want no result and one diagnostic line", stdout, stderr)
			}
		})
	}
	if _, err := os.Stat(out); !os.IsNotExist(err) {
		t.Errorf("a failed invocation wrote %s", out)
	}
}

// malformed is a GEDCOM 5.5.1 file with a line the decoder cannot parse.
const malformed = "0 HEAD\n" +
	"1 GEDC\n" +
	"2 VERS 5.5.1\n" +
	"2 FORM LINEAGE-LINKED\n" +
	"1 CHAR UTF-8\n" +
	"0 @I1@ INDI\n" +
	"1 NAME Ada /Example/\n" +
	"not a line\n" +
	"0 TRLR\n"

func TestConvertReportsDecodeDiagnostics(t *testing.T) {
	in := writeTemp(t, "malformed.ged", malformed)
	out := filepath.Join(t.TempDir(), "out.ged")

	code, stdout, stderr := run(t, "convert", in, "--to", "7.0", "-o", out)
	if code != ExitOK || stderr != "" {
		t.Fatalf("exit = %d, stderr = %q", code, stderr)
	}
	if !strings.Contains(stdout, "decode (") || !strings.Contains(stdout, "\tline 8\t") {
		t.Errorf("stdout has no decode section naming line 8:\n%s", stdout)
	}

	code, stdout, stderr = run(t, "convert", "--format", "json", in, "--to", "7.0", "-o", out)
	if code != ExitOK || stderr != "" {
		t.Fatalf("json: exit = %d, stderr = %q", code, stderr)
	}
	var got convertReport
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("stdout is not JSON: %v\n%s", err, stdout)
	}
	found := false
	for _, f := range got.Decode {
		found = found || (f.Source == sourceDecode && f.Line == 8)
	}
	if !found {
		t.Errorf("decode = %+v, want a finding on line 8", got.Decode)
	}
}

func TestConvertCleanInputHasEmptyDecode(t *testing.T) {
	out := filepath.Join(t.TempDir(), "out.ged")
	_, stdout, _ := run(t, "convert", "--format", "json", records, "--to", "5.5.1", "-o", out)
	if !strings.Contains(stdout, `"decode": []`) {
		t.Errorf("want an empty decode list:\n%s", stdout)
	}
}

func TestConvertReplacesExistingOutput(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "out.ged")
	if err := os.WriteFile(out, []byte("old\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	code, _, stderr := run(t, "convert", records, "--to", "5.5.1", "-o", out)
	if code != ExitOK || stderr != "" {
		t.Fatalf("exit = %d, stderr = %q", code, stderr)
	}
	want, err := os.ReadFile(records)
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	assertSameBytes(t, got, want)
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Errorf("want only out.ged in the directory, got %v", entries)
	}
}

func TestConvertFailOnLossKeepsExistingOutput(t *testing.T) {
	out := filepath.Join(t.TempDir(), "out.ged")
	if err := os.WriteFile(out, []byte("old\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if code, _, _ := run(t, "convert", "--fail-on-loss", lossy, "--to", "5.5.1", "-o", out); code != ExitError {
		t.Fatalf("exit = %d, want %d", code, ExitError)
	}
	if got, _ := os.ReadFile(out); string(got) != "old\n" {
		t.Errorf("existing output changed: %q", got)
	}
}
