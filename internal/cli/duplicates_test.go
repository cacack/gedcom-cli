package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

// The duplicates fixtures reuse xrefs for different people: @I1@ is Cora in A
// and Ansel in B, @I2@ the other way round. B's @I4@ is a Cora vetoed by sex.
const (
	duplicatesA = "testdata/duplicates-a.ged"
	duplicatesB = "testdata/duplicates-b.ged"
)

func TestDuplicatesText(t *testing.T) {
	code, stdout, stderr := run(t, "duplicates", duplicatesA, duplicatesB)
	if code != ExitOK || stderr != "" {
		t.Fatalf("exit = %d, stderr = %q", code, stderr)
	}
	want := "0.90\t@I1@\tCora /Sample/\t3 MAR 1901\t14 NOV 1978\t@I2@\tCora /Sample/\t1901\t\n" +
		"0.90\t@I2@\tAnsel /Example/\t1880\t\t@I1@\tAnsel /Example/\tABT 1880\t1951\n" +
		"0.70\t@I3@\tDell /Sample/\t21 JUL 1899\t\t@I3@\tDell /Sample/\t\t\n"
	if stdout != want {
		t.Errorf("stdout =\n%s\nwant\n%s", stdout, want)
	}
}

func TestDuplicatesJSON(t *testing.T) {
	code, stdout, stderr := run(t, "duplicates", "--format", "json", duplicatesA, duplicatesB)
	if code != ExitOK || stderr != "" {
		t.Fatalf("exit = %d, stderr = %q", code, stderr)
	}
	var got []map[string]any
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("stdout is not JSON: %v\n%s", err, stdout)
	}
	person := func(xref, name, birth, death string) map[string]any {
		return map[string]any{"xref": xref, "name": name, "birth": birth, "death": death}
	}
	want := []map[string]any{
		{"score": 0.9, "a": person("@I1@", "Cora /Sample/", "3 MAR 1901", "14 NOV 1978"), "b": person("@I2@", "Cora /Sample/", "1901", "")},
		{"score": 0.9, "a": person("@I2@", "Ansel /Example/", "1880", ""), "b": person("@I1@", "Ansel /Example/", "ABT 1880", "1951")},
		{"score": 0.7, "a": person("@I3@", "Dell /Sample/", "21 JUL 1899", ""), "b": person("@I3@", "Dell /Sample/", "", "")},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v\nwant %v", got, want)
	}
}

func TestDuplicatesMinScore(t *testing.T) {
	tests := []struct {
		minScore string
		want     []string // "score a-xref b-xref" per pair, in output order
	}{
		{"0", []string{"0.9 @I1@ @I2@", "0.9 @I2@ @I1@", "0.7 @I3@ @I3@"}}, // the library's ZeroThreshold path
		{"0.8", []string{"0.9 @I1@ @I2@", "0.9 @I2@ @I1@"}},
		{"0.95", []string{}},
	}
	for _, tt := range tests {
		t.Run(tt.minScore, func(t *testing.T) {
			code, stdout, stderr := run(t, "duplicates", "--format", "json", "--min-score", tt.minScore, duplicatesA, duplicatesB)
			if code != ExitOK || stderr != "" {
				t.Fatalf("exit = %d, stderr = %q", code, stderr)
			}
			var got []duplicatePair
			if err := json.Unmarshal([]byte(stdout), &got); err != nil {
				t.Fatalf("stdout is not JSON: %v\n%s", err, stdout)
			}
			if got == nil {
				t.Fatalf("stdout = %s, want an array", stdout)
			}
			summary := []string{}
			for _, p := range got {
				summary = append(summary, strconv.FormatFloat(p.Score, 'g', -1, 64)+" "+p.A.XRef+" "+p.B.XRef)
			}
			if !reflect.DeepEqual(summary, tt.want) {
				t.Errorf("pairs = %q, want %q", summary, tt.want)
			}
		})
	}
}

// TestDuplicatesMinScoreRounded checks that --min-score applies to the printed
// score. Exact names, the same sex and birth years one apart sum to a raw
// score just under 0.8 that prints as 0.80.
func TestDuplicatesMinScoreRounded(t *testing.T) {
	dir := t.TempDir()
	a, b := filepath.Join(dir, "a.ged"), filepath.Join(dir, "b.ged")
	for path, year := range map[string]string{a: "1900", b: "1901"} {
		ged := "0 HEAD\n1 GEDC\n2 VERS 5.5.1\n1 CHAR UTF-8\n0 @I1@ INDI\n1 NAME Ezra /Example/\n1 SEX M\n1 BIRT\n2 DATE " + year + "\n0 TRLR\n"
		if err := os.WriteFile(path, []byte(ged), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	code, stdout, stderr := run(t, "duplicates", "--min-score", "0.8", a, b)
	if code != ExitOK || stderr != "" {
		t.Fatalf("exit = %d, stderr = %q", code, stderr)
	}
	if !strings.HasPrefix(stdout, "0.80\t@I1@\t") {
		t.Errorf("stdout = %q, want the 0.80 pair", stdout)
	}
}

func TestDuplicatesNoPairsText(t *testing.T) {
	code, stdout, stderr := run(t, "duplicates", "--min-score", "1", duplicatesA, duplicatesB)
	if code != ExitOK || stdout != "" || stderr != "" {
		t.Errorf("exit = %d, stdout = %q, stderr = %q", code, stdout, stderr)
	}
}

// TestDuplicatesLimitWarning checks that the library's notice of shortened
// comparisons reaches stderr while the pairs still reach stdout.
func TestDuplicatesLimitWarning(t *testing.T) {
	dir := t.TempDir()
	// 90 runes is past validator.DefaultMaxNameRunes, so the comparison is shortened.
	long := strings.Repeat("Longgiven", 10)
	ged := "0 HEAD\n1 GEDC\n2 VERS 5.5.1\n1 CHAR UTF-8\n0 @I1@ INDI\n1 NAME " + long + " /Example/\n0 TRLR\n"
	a, b := filepath.Join(dir, "a.ged"), filepath.Join(dir, "b.ged")
	for _, path := range []string{a, b} {
		if err := os.WriteFile(path, []byte(ged), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	code, stdout, stderr := run(t, "duplicates", "--min-score", "0.5", a, b)
	if code != ExitOK {
		t.Fatalf("exit = %d, stderr = %q", code, stderr)
	}
	if !strings.HasPrefix(stdout, "0.60\t@I1@\t") {
		t.Errorf("stdout = %q, want the pair", stdout)
	}
	if !strings.Contains(stderr, "gedcom: warning: ") || !strings.Contains(stderr, "DUPLICATE_DETECTION_LIMITED") {
		t.Errorf("stderr = %q, want the limit warning", stderr)
	}
}

func TestDuplicatesLeavesInputs(t *testing.T) {
	dir := t.TempDir()
	before := map[string][]byte{}
	for _, src := range []string{duplicatesA, duplicatesB} {
		data, err := os.ReadFile(src)
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(dir, filepath.Base(src))
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatal(err)
		}
		before[path] = data
	}
	paths := []string{filepath.Join(dir, filepath.Base(duplicatesA)), filepath.Join(dir, filepath.Base(duplicatesB))}
	if code, _, stderr := run(t, "duplicates", paths[0], paths[1]); code != ExitOK {
		t.Fatalf("exit = %d, stderr = %q", code, stderr)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Errorf("directory holds %d files after the run, want the 2 inputs", len(entries))
	}
	for path, data := range before {
		after, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(after, data) {
			t.Errorf("%s changed", path)
		}
	}
}

func TestDuplicatesExitCodes(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want int
	}{
		{"one file", []string{"duplicates", duplicatesA}, ExitUsage},
		{"score above 1", []string{"duplicates", "--min-score", "1.5", duplicatesA, duplicatesB}, ExitUsage},
		{"negative score", []string{"duplicates", "--min-score", "-0.1", duplicatesA, duplicatesB}, ExitUsage},
		{"NaN score", []string{"duplicates", "--min-score", "NaN", duplicatesA, duplicatesB}, ExitUsage},
		{"missing file", []string{"duplicates", duplicatesA, "testdata/does-not-exist.ged"}, ExitError},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code, stdout, stderr := run(t, tt.args...)
			if code != tt.want || stdout != "" || stderr == "" {
				t.Errorf("exit = %d (want %d), stdout = %q, stderr = %q", code, tt.want, stdout, stderr)
			}
		})
	}
}
