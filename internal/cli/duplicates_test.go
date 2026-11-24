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
	code, stdout, stderr := run(t, "duplicates", "--reference-year", referenceYear, duplicatesA, duplicatesB)
	if code != ExitOK || stderr != "" {
		t.Fatalf("exit = %d, stderr = %q", code, stderr)
	}
	want := "0.90\t@I1@\tCora /Sample/\t3 MAR 1901\t14 NOV 1978\tfalse\t@I2@\tCora /Sample/\t1901\t\tfalse\n" +
		"0.90\t@I2@\tAnsel /Example/\t1880\t\tfalse\t@I1@\tAnsel /Example/\tABT 1880\t1951\tfalse\n" +
		"0.70\t@I3@\tDell /Sample/\t21 JUL 1899\t\tfalse\t@I3@\tDell /Sample/\t\t\ttrue\n"
	if stdout != want {
		t.Errorf("stdout =\n%s\nwant\n%s", stdout, want)
	}
}

// The groups fixtures hold Ansel once on each side and Cora twice on each
// side, so the five pairs form two groups.
const (
	duplicateGroupsA = "testdata/duplicates-groups-a.ged"
	duplicateGroupsB = "testdata/duplicates-groups-b.ged"
)

func TestDuplicatesGroupsText(t *testing.T) {
	code, stdout, stderr := run(t, "duplicates", "--groups", duplicateGroupsA, duplicateGroupsB)
	if code != ExitOK || stderr != "" {
		t.Fatalf("exit = %d, stderr = %q", code, stderr)
	}
	ansel := "Ansel /Example/\t1880\t\tfalse"
	cora := "Cora /Sample/\t1901\t\tfalse"
	want := "group 1: a @I1@; b @I2@\n" +
		"  0.90\t@I1@\t" + ansel + "\t@I2@\t" + ansel + "\n" +
		"group 2: a @I2@ @I3@; b @I1@ @I3@\n" +
		"  0.90\t@I2@\t" + cora + "\t@I1@\t" + cora + "\n" +
		"  0.90\t@I2@\t" + cora + "\t@I3@\t" + cora + "\n" +
		"  0.90\t@I3@\t" + cora + "\t@I1@\t" + cora + "\n" +
		"  0.90\t@I3@\t" + cora + "\t@I3@\t" + cora + "\n"
	if stdout != want {
		t.Errorf("stdout =\n%s\nwant\n%s", stdout, want)
	}
	// Without --groups the same pairs print as pairs.
	_, pairs, _ := run(t, "duplicates", duplicateGroupsA, duplicateGroupsB)
	var lines []string
	for _, line := range strings.Split(want, "\n") {
		if strings.HasPrefix(line, "  ") {
			lines = append(lines, strings.TrimPrefix(line, "  "))
		}
	}
	if pairs != strings.Join(lines, "\n")+"\n" {
		t.Errorf("pairs output =\n%s\nwant the grouped pairs unindented", pairs)
	}
}

func TestDuplicatesGroupsJSON(t *testing.T) {
	code, stdout, stderr := run(t, "duplicates", "--groups", "--format", "json", duplicateGroupsA, duplicateGroupsB)
	if code != ExitOK || stderr != "" {
		t.Fatalf("exit = %d, stderr = %q", code, stderr)
	}
	var got []struct {
		A     []string        `json:"a"`
		B     []string        `json:"b"`
		Pairs []duplicatePair `json:"pairs"`
	}
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("stdout is not JSON: %v\n%s", err, stdout)
	}
	type summary struct {
		A, B, Pairs []string
	}
	var sums []summary
	for _, g := range got {
		s := summary{A: g.A, B: g.B}
		for _, p := range g.Pairs {
			s.Pairs = append(s.Pairs, strconv.FormatFloat(p.Score, 'g', -1, 64)+" "+p.A.XRef+" "+p.B.XRef)
		}
		sums = append(sums, s)
	}
	want := []summary{
		{A: []string{"@I1@"}, B: []string{"@I2@"}, Pairs: []string{"0.9 @I1@ @I2@"}},
		{A: []string{"@I2@", "@I3@"}, B: []string{"@I1@", "@I3@"}, Pairs: []string{"0.9 @I2@ @I1@", "0.9 @I2@ @I3@", "0.9 @I3@ @I1@", "0.9 @I3@ @I3@"}},
	}
	if !reflect.DeepEqual(sums, want) {
		t.Errorf("groups = %+v\nwant %+v", sums, want)
	}
	if !strings.Contains(stdout, `"name": "Cora /Sample/"`) {
		t.Errorf("pairs in a group lack list indi's keys:\n%s", stdout)
	}
}

// TestDuplicatesGroupsMinScore checks that groups are formed from the pairs
// reported at the chosen score, and that no pairs is no groups.
func TestDuplicatesGroupsMinScore(t *testing.T) {
	tests := []struct {
		minScore string
		want     string
	}{
		{"0", "group 1: a @I1@; b @I2@\ngroup 2: a @I2@; b @I1@\ngroup 3: a @I3@; b @I3@\n"},
		{"0.8", "group 1: a @I1@; b @I2@\ngroup 2: a @I2@; b @I1@\n"},
		{"1", ""},
	}
	for _, tt := range tests {
		t.Run(tt.minScore, func(t *testing.T) {
			code, stdout, stderr := run(t, "duplicates", "--groups", "--min-score", tt.minScore, "--reference-year", referenceYear, duplicatesA, duplicatesB)
			if code != ExitOK || stderr != "" {
				t.Fatalf("exit = %d, stderr = %q", code, stderr)
			}
			var headers []string
			for _, line := range strings.SplitAfter(stdout, "\n") {
				if strings.HasPrefix(line, "group ") {
					headers = append(headers, line)
				}
			}
			if got := strings.Join(headers, ""); got != tt.want {
				t.Errorf("group lines = %q, want %q", got, tt.want)
			}
		})
	}
	code, stdout, stderr := run(t, "duplicates", "--groups", "--format", "json", "--min-score", "1", duplicatesA, duplicatesB)
	if code != ExitOK || stderr != "" || strings.TrimSpace(stdout) != "[]" {
		t.Errorf("json: exit = %d, stderr = %q, stdout = %q", code, stderr, stdout)
	}
}

func TestDuplicatesJSON(t *testing.T) {
	code, stdout, stderr := run(t, "duplicates", "--format", "json", "--reference-year", referenceYear, duplicatesA, duplicatesB)
	if code != ExitOK || stderr != "" {
		t.Fatalf("exit = %d, stderr = %q", code, stderr)
	}
	var got []map[string]any
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("stdout is not JSON: %v\n%s", err, stdout)
	}
	person := func(xref, name, birth, death string, living bool) map[string]any {
		return map[string]any{"xref": xref, "name": name, "birth": birth, "death": death, "living": living}
	}
	want := []map[string]any{
		{"score": 0.9, "a": person("@I1@", "Cora /Sample/", "3 MAR 1901", "14 NOV 1978", false), "b": person("@I2@", "Cora /Sample/", "1901", "", false)},
		{"score": 0.9, "a": person("@I2@", "Ansel /Example/", "1880", "", false), "b": person("@I1@", "Ansel /Example/", "ABT 1880", "1951", false)},
		{"score": 0.7, "a": person("@I3@", "Dell /Sample/", "21 JUL 1899", "", false), "b": person("@I3@", "Dell /Sample/", "", "", true)},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v\nwant %v", got, want)
	}
}

// TestDuplicatesRedactLiving checks that detection still sees a presumed-living
// person, and that --redact-living hides their name in the pair printed.
func TestDuplicatesRedactLiving(t *testing.T) {
	code, stdout, stderr := run(t, "duplicates", "--redact-living", "--reference-year", referenceYear, duplicatesA, duplicatesB)
	if code != ExitOK || stderr != "" {
		t.Fatalf("exit = %d, stderr = %q", code, stderr)
	}
	want := "0.70\t@I3@\tDell /Sample/\t21 JUL 1899\t\tfalse\t@I3@\tLiving\t\t\ttrue\n"
	if !strings.HasSuffix(stdout, want) {
		t.Errorf("stdout =\n%s\nwant it to end with\n%s", stdout, want)
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
