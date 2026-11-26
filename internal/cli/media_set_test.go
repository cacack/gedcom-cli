package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// mediaSetGED has two OBJE records with an empty FILE (@O1@, @O2@), one with
// a path (@O3@) and one with two files (@O4@).
const mediaSetGED = `0 HEAD
1 SOUR gedcom-cli-test
1 GEDC
2 VERS 5.5.1
2 FORM LINEAGE-LINKED
1 CHAR UTF-8
0 @I1@ INDI
1 NAME Ada /Example/
1 OBJE @O1@
0 @I2@ INDI
1 NAME Ben /Example/
1 OBJE @O2@
0 @O1@ OBJE
1 FILE
2 FORM image/jpeg
2 TITL Ada, 1920
0 @O2@ OBJE
1 FILE
2 FORM jpg
0 @O3@ OBJE
1 FILE photos/cara.jpg
2 FORM jpg
0 @O4@ OBJE
1 FILE photos/a.jpg
2 FORM jpg
1 FILE photos/b.jpg
2 FORM jpg
0 TRLR
`

func TestMediaSetEmptyFile(t *testing.T) {
	file, root := mediaFixture(t, mediaSetGED, "photos/ada.jpg")
	out := filepath.Join(t.TempDir(), "out.ged")
	code, stdout, stderr := run(t, "media", "set", file, "--root", root, "--obje", "@O1@", "--path", "photos/ada.jpg", "-o", out)
	if code != ExitOK || stderr != "" {
		t.Fatalf("exit = %d, stderr = %q", code, stderr)
	}
	if want := "@O1@\tset\tphotos/ada.jpg\n"; stdout != want {
		t.Errorf("stdout = %q, want %q", stdout, want)
	}
	got := readFile(t, out)
	want := replaceOnce(t, mediaSetGED, "0 @O1@ OBJE\n1 FILE\n", "0 @O1@ OBJE\n1 FILE photos/ada.jpg\n")
	if got != want {
		t.Errorf("output =\n%s\nwant\n%s", got, want)
	}

	// Again, on the output: nothing changes.
	again := filepath.Join(t.TempDir(), "again.ged")
	code, stdout, stderr = run(t, "media", "set", "--format", "json", out, "--root", root, "--obje", "O1", "--path", "photos/ada.jpg", "-o", again)
	if code != ExitOK || stderr != "" {
		t.Fatalf("second run: exit = %d, stderr = %q", code, stderr)
	}
	var results []mediaSetResult
	if err := json.Unmarshal([]byte(stdout), &results); err != nil {
		t.Fatalf("stdout is not JSON: %v\n%s", err, stdout)
	}
	if len(results) != 1 || results[0] != (mediaSetResult{Object: "@O1@", Path: "photos/ada.jpg", Action: "unchanged"}) {
		t.Errorf("second run result = %+v", results)
	}
	if readFile(t, again) != got {
		t.Errorf("second run changed the file:\n%s", readFile(t, again))
	}
}

func TestMediaSetReplace(t *testing.T) {
	file, _ := mediaFixture(t, mediaSetGED)
	out := filepath.Join(t.TempDir(), "out.ged")
	code, stdout, stderr := run(t, "media", "set", file, "--obje", "@O3@", "--path", `scans\cara.png`, "--form", "png", "--replace", "-o", out)
	if code != ExitOK || stderr != "" {
		t.Fatalf("exit = %d, stderr = %q", code, stderr)
	}
	if want := "@O3@\treplaced\tscans/cara.png\n"; stdout != want {
		t.Errorf("stdout = %q, want %q", stdout, want)
	}
	want := replaceOnce(t, mediaSetGED, "1 FILE photos/cara.jpg\n2 FORM jpg\n", "1 FILE scans/cara.png\n2 FORM png\n")
	if got := readFile(t, out); got != want {
		t.Errorf("output =\n%s\nwant\n%s", got, want)
	}
}

func TestMediaSetRefuses(t *testing.T) {
	file, root := mediaFixture(t, mediaSetGED, "photos/ada.jpg")
	tests := []struct {
		name string
		args []string
		want string
	}{
		{"not OBJE", []string{"--obje", "@I1@", "--path", "photos/ada.jpg"}, "@I1@ is a INDI record; want an OBJE record"},
		{"unknown", []string{"--obje", "@O9@", "--path", "photos/ada.jpg"}, "no such record: @O9@"},
		{"other path", []string{"--obje", "@O3@", "--path", "photos/ada.jpg"}, "@O3@ already has FILE photos/cara.jpg; pass --replace"},
		{"several files", []string{"--obje", "@O4@", "--path", "photos/ada.jpg", "--replace"}, "@O4@ has 2 FILE lines; want exactly one"},
		{"absolute", []string{"--root", root, "--obje", "@O1@", "--path", "/photos/ada.jpg"}, "is outside --root"},
		{"climbs out", []string{"--root", root, "--obje", "@O1@", "--path", "../ada.jpg"}, "path ../ada.jpg is outside --root"},
		{"no such file", []string{"--root", root, "--obje", "@O1@", "--path", "photos/none.jpg"}, "no file photos/none.jpg under --root"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out := filepath.Join(t.TempDir(), "out.ged")
			code, stdout, stderr := run(t, append([]string{"media", "set", file, "-o", out}, tt.args...)...)
			if code != ExitError || stdout != "" {
				t.Fatalf("exit = %d, stdout = %q", code, stdout)
			}
			if !strings.Contains(stderr, tt.want) {
				t.Errorf("stderr = %q, want %q", stderr, tt.want)
			}
			if _, err := os.Stat(out); !os.IsNotExist(err) {
				t.Errorf("output written: %v", err)
			}
		})
	}
}

func TestMediaSetBatch(t *testing.T) {
	file, root := mediaFixture(t, mediaSetGED, "photos/ada.jpg", "photos/cara.jpg")
	batch := filepath.Join(t.TempDir(), "files.tsv")
	rows := "# xref\tpath\tform\n" +
		"O1\tphotos/ada.jpg\n" +
		"\n" +
		"@O2@\tphotos/ada.jpg\timage/jpeg\n" +
		"O3\tphotos/cara.jpg\n"
	if err := os.WriteFile(batch, []byte(rows), 0o644); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(t.TempDir(), "out.ged")
	code, stdout, stderr := run(t, "media", "set", file, "--root", root, "--batch", batch, "-o", out)
	if code != ExitOK || stderr != "" {
		t.Fatalf("exit = %d, stderr = %q", code, stderr)
	}
	wantOut := "@O1@\tset\tphotos/ada.jpg\n" +
		"@O2@\tset\tphotos/ada.jpg\n" +
		"@O3@\tunchanged\tphotos/cara.jpg\n"
	if stdout != wantOut {
		t.Errorf("stdout =\n%s\nwant\n%s", stdout, wantOut)
	}
	want := replaceOnce(t, mediaSetGED, "0 @O1@ OBJE\n1 FILE\n", "0 @O1@ OBJE\n1 FILE photos/ada.jpg\n")
	want = replaceOnce(t, want, "0 @O2@ OBJE\n1 FILE\n2 FORM jpg\n", "0 @O2@ OBJE\n1 FILE photos/ada.jpg\n2 FORM image/jpeg\n")
	if got := readFile(t, out); got != want {
		t.Errorf("output =\n%s\nwant\n%s", got, want)
	}
}

func TestMediaSetBatchAllOrNone(t *testing.T) {
	file, root := mediaFixture(t, mediaSetGED, "photos/ada.jpg")
	batch := filepath.Join(t.TempDir(), "files.tsv")
	rows := "O1\tphotos/ada.jpg\n" +
		"I1\tphotos/ada.jpg\n" +
		"O2\t../outside.jpg\n" +
		"O3\tphotos/ada.jpg\n" +
		"O1\tphotos/ada.jpg\n"
	if err := os.WriteFile(batch, []byte(rows), 0o644); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(t.TempDir(), "out.ged")
	code, stdout, stderr := run(t, "media", "set", file, "--root", root, "--batch", batch, "-o", out)
	if code != ExitError || stdout != "" {
		t.Fatalf("exit = %d, stdout = %q", code, stdout)
	}
	for _, want := range []string{"line 2: @I1@ is a INDI record", "line 3: path ../outside.jpg is outside --root", "line 4: @O3@ already has FILE", "line 5: @O1@ is already set by line 1"} {
		if !strings.Contains(stderr, want) {
			t.Errorf("stderr missing %q:\n%s", want, stderr)
		}
	}
	if strings.Contains(stderr, "line 1:") {
		t.Errorf("stderr reports the good row:\n%s", stderr)
	}
	if _, err := os.Stat(out); !os.IsNotExist(err) {
		t.Errorf("output written despite failed rows: %v", err)
	}
}

func TestMediaSetUsage(t *testing.T) {
	file, _ := mediaFixture(t, mediaSetGED)
	out := filepath.Join(t.TempDir(), "out.ged")
	tests := []struct {
		name string
		args []string
		want int
	}{
		{"no output", []string{file, "--obje", "O1", "--path", "a.jpg"}, ExitUsage},
		{"output is input", []string{file, "--obje", "O1", "--path", "a.jpg", "-o", file}, ExitUsage},
		{"no path", []string{file, "--obje", "O1", "-o", out}, ExitUsage},
		{"batch and obje", []string{file, "--batch", "x.tsv", "--obje", "O1", "-o", out}, ExitUsage},
		{"missing batch", []string{file, "--batch", filepath.Join(t.TempDir(), "nope.tsv"), "-o", out}, ExitError},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code, _, stderr := run(t, append([]string{"media", "set"}, tt.args...)...)
			if code != tt.want {
				t.Errorf("exit = %d, want %d (stderr %q)", code, tt.want, stderr)
			}
		})
	}
	if readFile(t, file) != mediaSetGED {
		t.Error("input file changed")
	}
}

func TestMediaSetFormOnly(t *testing.T) {
	ged := replaceOnce(t, mediaSetGED, "1 FILE photos/cara.jpg\n", `1 FILE photos\cara.jpg`+"\n")
	file, _ := mediaFixture(t, ged)
	out := filepath.Join(t.TempDir(), "out.ged")
	code, stdout, stderr := run(t, "media", "set", file, "--obje", "@O3@", "--path", "photos/cara.jpg", "--form", "image/jpeg", "-o", out)
	if code != ExitOK || stderr != "" {
		t.Fatalf("exit = %d, stderr = %q", code, stderr)
	}
	// The FILE keeps its spelling, and the report shows what it holds.
	if want := "@O3@\tset\tphotos\\cara.jpg\n"; stdout != want {
		t.Errorf("stdout = %q, want %q", stdout, want)
	}
	want := replaceOnce(t, ged, "1 FILE photos\\cara.jpg\n2 FORM jpg\n", "1 FILE photos\\cara.jpg\n2 FORM image/jpeg\n")
	if got := readFile(t, out); got != want {
		t.Errorf("output =\n%s\nwant\n%s", got, want)
	}
}

// mediaSet55GED has an OBJE in the GEDCOM 5.5 layout: FORM beside FILE.
const mediaSet55GED = `0 HEAD
1 SOUR gedcom-cli-test
1 GEDC
2 VERS 5.5
2 FORM LINEAGE-LINKED
1 CHAR UTF-8
0 @O1@ OBJE
1 FORM jpg
1 FILE
1 TITL Ada, 1920
0 TRLR
`

func TestMediaSetForm55(t *testing.T) {
	file, _ := mediaFixture(t, mediaSet55GED)
	out := filepath.Join(t.TempDir(), "out.ged")
	code, stdout, stderr := run(t, "media", "set", file, "--obje", "@O1@", "--path", "photos/ada.png", "--form", "png", "-o", out)
	if code != ExitOK || stderr != "" {
		t.Fatalf("exit = %d, stderr = %q", code, stderr)
	}
	if want := "@O1@\tset\tphotos/ada.png\n"; stdout != want {
		t.Errorf("stdout = %q, want %q", stdout, want)
	}
	want := replaceOnce(t, mediaSet55GED, "1 FORM jpg\n1 FILE\n", "1 FORM png\n1 FILE photos/ada.png\n")
	if got := readFile(t, out); got != want {
		t.Errorf("output =\n%s\nwant\n%s", got, want)
	}
}
