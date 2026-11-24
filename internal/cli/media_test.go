package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// mediaGED has one reference of each status: @O1@ resolves, @O2@ does not,
// and @I1@'s inline links are a URL, an absolute path and a backslash path.
const mediaGED = `0 HEAD
1 SOUR gedcom-cli-test
1 GEDC
2 VERS 5.5.1
2 FORM LINEAGE-LINKED
1 CHAR UTF-8
0 @I1@ INDI
1 NAME Ada /Example/
1 OBJE
2 FILE https://example.com/ada.jpg
2 FORM jpg
1 OBJE
2 FILE C:\scans\ada.jpg
2 FORM jpg
1 OBJE
2 FILE photos\ben.jpg
2 FORM jpg
0 @I2@ INDI
1 NAME Ben /Example/
1 SEX M
0 @F1@ FAM
1 HUSB @I2@
0 @S1@ SOUR
1 TITL Example Parish Register
0 @R1@ REPO
1 NAME Example Archive
0 @O1@ OBJE
1 FILE photos/ada.jpg
2 FORM jpg
0 @O2@ OBJE
1 FILE photos/gone.jpg
2 FORM jpg
0 TRLR
`

// media70GED is a 7.0 file with no media.
const media70GED = `0 HEAD
1 GEDC
2 VERS 7.0
0 @I1@ INDI
1 NAME Ada /Example/
0 TRLR
`

// mediaFixture writes ged and a media folder holding the given files to a
// temporary directory, returning the GEDCOM's path and the folder's.
func mediaFixture(t *testing.T, ged string, files ...string) (file, root string) {
	t.Helper()
	dir := t.TempDir()
	file = filepath.Join(dir, "family.ged")
	if err := os.WriteFile(file, []byte(ged), 0o644); err != nil {
		t.Fatal(err)
	}
	root = filepath.Join(dir, "media")
	if err := os.Mkdir(root, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range files {
		p := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return file, root
}

func TestMediaCheckText(t *testing.T) {
	file, root := mediaFixture(t, mediaGED, "photos/ada.jpg", "photos/ben.jpg", "photos/extra.jpg", ".hidden/x.jpg", "docs/.DS_Store")
	code, stdout, stderr := run(t, "media", "check", "--root", root, "--unreferenced", file)
	if code != ExitError {
		t.Fatalf("exit = %d, want %d (stderr %q)", code, ExitError, stderr)
	}
	if stderr != "gedcom: 1 missing media file in "+file+"\n" {
		t.Errorf("stderr = %q", stderr)
	}
	want := "url\t@I1@\tline 10\thttps://example.com/ada.jpg\n" +
		"absolute\t@I1@\tline 13\tC:\\scans\\ada.jpg\n" +
		"ok\t@I1@\tline 16\tphotos\\ben.jpg\n" +
		"ok\t@O1@\tline 28\tphotos/ada.jpg\n" +
		"missing\t@O2@\tline 31\tphotos/gone.jpg\n" +
		"unreferenced\t\t\tphotos/extra.jpg\n"
	if stdout != want {
		t.Errorf("stdout =\n%s\nwant\n%s", stdout, want)
	}
}

func TestMediaCheckJSON(t *testing.T) {
	file, root := mediaFixture(t, mediaGED, "photos/ada.jpg", "photos/gone.jpg", "photos/ben.jpg")
	for _, unreferenced := range []bool{false, true} {
		args := []string{"media", "check", "--format", "json", "--root", root, file}
		if unreferenced {
			args = append(args, "--unreferenced")
		}
		code, stdout, stderr := run(t, args...)
		if code != ExitOK || stderr != "" {
			t.Fatalf("exit = %d, stderr = %q", code, stderr)
		}
		var report mediaCheckReport
		dec := json.NewDecoder(strings.NewReader(stdout))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&report); err != nil {
			t.Fatalf("stdout is not a media check report: %v\n%s", err, stdout)
		}
		var statuses []string
		for _, ref := range report.References {
			statuses = append(statuses, ref.Status)
		}
		if got := strings.Join(statuses, ","); got != "url,absolute,ok,ok,ok" {
			t.Errorf("statuses = %s", got)
		}
		if report.References[3] != (mediaRef{XRef: "@O1@", Line: 28, File: "photos/ada.jpg", Status: "ok"}) {
			t.Errorf("reference = %+v", report.References[3])
		}
		if unreferenced != strings.Contains(stdout, `"unreferenced"`) {
			t.Errorf("unreferenced key present = %v with --unreferenced %v", !unreferenced, unreferenced)
		}
		if unreferenced && (report.Unreferenced == nil || len(*report.Unreferenced) != 0) {
			t.Errorf("unreferenced = %v, want []", report.Unreferenced)
		}
	}
}

// mediaEmptyGED is a media object whose FILE has no value, as some exports
// write for media that is not a local file.
const mediaEmptyGED = `0 HEAD
1 GEDC
2 VERS 5.5.1
2 FORM LINEAGE-LINKED
1 CHAR UTF-8
0 @O1@ OBJE
1 FILE
2 FORM jpg
2 TITL Online image
0 TRLR
`

func TestMediaCheckEmptyFile(t *testing.T) {
	file, root := mediaFixture(t, mediaEmptyGED)
	code, stdout, stderr := run(t, "media", "check", "--root", root, file)
	if code != ExitOK || stderr != "" {
		t.Fatalf("exit = %d, stderr = %q", code, stderr)
	}
	if want := "empty\t@O1@\tline 7\t\n"; stdout != want {
		t.Errorf("stdout = %q, want %q", stdout, want)
	}

	code, stdout, stderr = run(t, "media", "check", "--format", "json", "--root", root, file)
	if code != ExitOK || stderr != "" {
		t.Fatalf("json: exit = %d, stderr = %q", code, stderr)
	}
	var report mediaCheckReport
	if err := json.Unmarshal([]byte(stdout), &report); err != nil {
		t.Fatalf("stdout is not a media check report: %v\n%s", err, stdout)
	}
	if len(report.References) != 1 || report.References[0] != (mediaRef{XRef: "@O1@", Line: 7, Status: "empty"}) {
		t.Errorf("references = %+v", report.References)
	}
}

// TestMediaCheckWhitespaceFile checks that a FILE of only whitespace is empty
// too, not a path to resolve.
func TestMediaCheckWhitespaceFile(t *testing.T) {
	file, root := mediaFixture(t, strings.Replace(mediaEmptyGED, "1 FILE\n", "1 FILE \t\n", 1))
	code, stdout, stderr := run(t, "media", "check", "--root", root, file)
	if code != ExitOK || stderr != "" {
		t.Fatalf("exit = %d, stderr = %q", code, stderr)
	}
	if !strings.HasPrefix(stdout, "empty\t@O1@\tline 7\t") {
		t.Errorf("stdout = %q, want an empty reference", stdout)
	}
}

func TestMediaCheckEmptyAndMissing(t *testing.T) {
	ged := strings.Replace(mediaEmptyGED, "0 TRLR\n", "0 @O2@ OBJE\n1 FILE photos/gone.jpg\n2 FORM jpg\n0 TRLR\n", 1)
	file, root := mediaFixture(t, ged)
	code, stdout, stderr := run(t, "media", "check", "--root", root, file)
	if code != ExitError {
		t.Fatalf("exit = %d, want %d (stderr %q)", code, ExitError, stderr)
	}
	if stderr != "gedcom: 1 missing media file in "+file+"\n" {
		t.Errorf("stderr = %q", stderr)
	}
	if want := "empty\t@O1@\tline 7\t\nmissing\t@O2@\tline 11\tphotos/gone.jpg\n"; stdout != want {
		t.Errorf("stdout = %q, want %q", stdout, want)
	}

	code, stdout, _ = run(t, "media", "check", "--format", "json", "--root", root, file)
	if code != ExitError {
		t.Errorf("json: exit = %d, want %d", code, ExitError)
	}
	var report mediaCheckReport
	if err := json.Unmarshal([]byte(stdout), &report); err != nil {
		t.Fatalf("stdout is not a media check report: %v\n%s", err, stdout)
	}
	if len(report.References) != 2 || report.References[0].Status != "empty" || report.References[1].Status != "missing" {
		t.Errorf("references = %+v", report.References)
	}
}

func TestMediaCheckHelpListsEmpty(t *testing.T) {
	code, stdout, _ := run(t, "media", "check", "--help")
	if code != ExitOK || !strings.Contains(stdout, "  empty     no value") {
		t.Errorf("exit = %d, help does not list the empty status:\n%s", code, stdout)
	}
}

func TestMediaCheckUsage(t *testing.T) {
	file, root := mediaFixture(t, mediaGED)
	if code, _, _ := run(t, "media", "check", file); code != ExitUsage {
		t.Errorf("no --root: exit = %d, want %d", code, ExitUsage)
	}
	if code, _, _ := run(t, "media", "check", "--root", filepath.Join(root, "nope"), file); code != ExitError {
		t.Errorf("missing --root: exit = %d, want %d", code, ExitError)
	}
	if code, _, _ := run(t, "media", "nope"); code != ExitUsage {
		t.Errorf("unknown subcommand: exit = %d, want %d", code, ExitUsage)
	}
}

// addedLines returns the lines of got that are not in want, failing unless
// got is want with only those lines inserted: every other line, and so every
// record not involved, comes back exactly as it was.
func addedLines(t *testing.T, got, want string) []string {
	t.Helper()
	g, w := strings.SplitAfter(got, "\n"), strings.SplitAfter(want, "\n")
	var added []string
	i := 0
	for _, line := range g {
		if i < len(w) && line == w[i] {
			i++
			continue
		}
		added = append(added, strings.TrimSuffix(line, "\n"))
	}
	if i != len(w) {
		t.Fatalf("output lost input line %d %q:\n%s", i+1, w[i], got)
	}
	return added
}

func readFile(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestMediaAddCreates551(t *testing.T) {
	file, root := mediaFixture(t, mediaGED, "photos/cara.jpg")
	out := filepath.Join(t.TempDir(), "out.ged")
	code, stdout, stderr := run(t, "media", "add", file, "--root", root, "--to", "I2", "--path", "photos/cara.jpg", "--title", "Cara, 1920", "-o", out)
	if code != ExitOK || stderr != "" {
		t.Fatalf("exit = %d, stderr = %q", code, stderr)
	}
	if want := "@I2@\t@O3@\tcreated\tphotos/cara.jpg\n"; stdout != want {
		t.Errorf("stdout = %q, want %q", stdout, want)
	}
	got := readFile(t, out)
	added := addedLines(t, got, mediaGED)
	want := []string{"1 OBJE @O3@", "0 @O3@ OBJE", "1 FILE photos/cara.jpg", "2 FORM jpg", "2 TITL Cara, 1920"}
	if strings.Join(added, "\n") != strings.Join(want, "\n") {
		t.Errorf("added lines = %q, want %q", added, want)
	}
	if !strings.Contains(got, "1 SEX M\n1 OBJE @O3@\n0 @F1@ FAM") {
		t.Errorf("link is not under @I2@:\n%s", got)
	}

	// Again, on the output: nothing changes.
	again := filepath.Join(t.TempDir(), "again.ged")
	code, stdout, stderr = run(t, "media", "add", out, "--root", root, "--to", "I2", "--path", "photos/cara.jpg", "--title", "Cara, 1920", "-o", again)
	if code != ExitOK || stderr != "" {
		t.Fatalf("second run: exit = %d, stderr = %q", code, stderr)
	}
	if want := "@I2@\t@O3@\tunchanged\tphotos/cara.jpg\n"; stdout != want {
		t.Errorf("second run stdout = %q, want %q", stdout, want)
	}
	if readFile(t, again) != got {
		t.Errorf("second run changed the file:\n%s", readFile(t, again))
	}
}

func TestMediaAddCreates70(t *testing.T) {
	file, _ := mediaFixture(t, media70GED)
	out := filepath.Join(t.TempDir(), "out.ged")
	code, _, stderr := run(t, "media", "add", file, "--to", "@I1@", "--path", `docs\register.pdf`, "--title", "Register", "-o", out)
	if code != ExitOK || stderr != "" {
		t.Fatalf("exit = %d, stderr = %q", code, stderr)
	}
	added := addedLines(t, readFile(t, out), media70GED)
	want := []string{"1 OBJE @O1@", "0 @O1@ OBJE", "1 FILE docs/register.pdf", "2 FORM application/pdf", "2 TITL Register"}
	if strings.Join(added, "\n") != strings.Join(want, "\n") {
		t.Errorf("added lines = %q, want %q", added, want)
	}
	if code, stdout, _ := run(t, "validate", out); code != ExitOK {
		t.Errorf("output does not validate:\n%s", stdout)
	}
}

func TestMediaAddReusesObject(t *testing.T) {
	file, _ := mediaFixture(t, mediaGED)
	out := filepath.Join(t.TempDir(), "out.ged")
	code, stdout, stderr := run(t, "media", "add", "--format", "json", file, "--to", "F1", "--path", "photos/ada.jpg", "--form", "png", "-o", out)
	if code != ExitOK || stderr != "" {
		t.Fatalf("exit = %d, stderr = %q", code, stderr)
	}
	var got []mediaAdded
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("stdout is not JSON: %v\n%s", err, stdout)
	}
	if len(got) != 1 || got[0] != (mediaAdded{To: "@F1@", Object: "@O1@", Path: "photos/ada.jpg", Action: "linked"}) {
		t.Errorf("result = %+v", got)
	}
	if added := addedLines(t, readFile(t, out), mediaGED); len(added) != 1 || added[0] != "1 OBJE @O1@" {
		t.Errorf("added lines = %q, want only the link", added)
	}
}

func TestMediaAddBatch(t *testing.T) {
	file, root := mediaFixture(t, mediaGED, "photos/cara.jpg", "docs/register.pdf")
	batch := filepath.Join(t.TempDir(), "links.tsv")
	rows := "# xref\tpath\ttitle\n" +
		"I2\tphotos/cara.jpg\tCara\n" +
		"\n" +
		"@S1@\tdocs/register.pdf\n" +
		"F1\tphotos/cara.jpg\n" +
		"I2\tphotos/cara.jpg\n"
	if err := os.WriteFile(batch, []byte(rows), 0o644); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(t.TempDir(), "out.ged")
	code, stdout, stderr := run(t, "media", "add", file, "--root", root, "--batch", batch, "-o", out)
	if code != ExitOK || stderr != "" {
		t.Fatalf("exit = %d, stderr = %q", code, stderr)
	}
	want := "@I2@\t@O3@\tcreated\tphotos/cara.jpg\n" +
		"@S1@\t@O4@\tcreated\tdocs/register.pdf\n" +
		"@F1@\t@O3@\tlinked\tphotos/cara.jpg\n" +
		"@I2@\t@O3@\tunchanged\tphotos/cara.jpg\n"
	if stdout != want {
		t.Errorf("stdout =\n%s\nwant\n%s", stdout, want)
	}
	added := addedLines(t, readFile(t, out), mediaGED)
	wantAdded := []string{"1 OBJE @O3@", "1 OBJE @O3@", "1 OBJE @O4@",
		"0 @O3@ OBJE", "1 FILE photos/cara.jpg", "2 FORM jpg", "2 TITL Cara",
		"0 @O4@ OBJE", "1 FILE docs/register.pdf", "2 FORM pdf"}
	if strings.Join(added, "\n") != strings.Join(wantAdded, "\n") {
		t.Errorf("added lines = %q, want %q", added, wantAdded)
	}
}

func TestMediaAddBatchAllOrNone(t *testing.T) {
	file, root := mediaFixture(t, mediaGED, "photos/cara.jpg")
	batch := filepath.Join(t.TempDir(), "links.tsv")
	rows := "I2\tphotos/cara.jpg\n" +
		"I9\tphotos/cara.jpg\n" +
		"I2\t../outside.jpg\n" +
		"R1\tphotos/cara.jpg\n" +
		"I2\tphotos/none.jpg\n"
	if err := os.WriteFile(batch, []byte(rows), 0o644); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(t.TempDir(), "out.ged")
	code, stdout, stderr := run(t, "media", "add", file, "--root", root, "--batch", batch, "-o", out)
	if code != ExitError || stdout != "" {
		t.Fatalf("exit = %d, stdout = %q", code, stdout)
	}
	for _, want := range []string{"line 2: no such record: @I9@", "line 3: path ../outside.jpg is outside --root", "line 4: @R1@ is a REPO record", "line 5: no file photos/none.jpg"} {
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

func TestMediaAddUsage(t *testing.T) {
	file, _ := mediaFixture(t, mediaGED)
	out := filepath.Join(t.TempDir(), "out.ged")
	tests := []struct {
		name string
		args []string
		want int
	}{
		{"no output", []string{file, "--to", "I1", "--path", "a.jpg"}, ExitUsage},
		{"output is input", []string{file, "--to", "I1", "--path", "a.jpg", "-o", file}, ExitUsage},
		{"no path", []string{file, "--to", "I1", "-o", out}, ExitUsage},
		{"batch and to", []string{file, "--batch", "x.tsv", "--to", "I1", "-o", out}, ExitUsage},
		{"no extension", []string{file, "--to", "I1", "--path", "photos/scan", "-o", out}, ExitError},
		{"missing batch", []string{file, "--batch", filepath.Join(t.TempDir(), "nope.tsv"), "-o", out}, ExitError},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code, _, stderr := run(t, append([]string{"media", "add"}, tt.args...)...)
			if code != tt.want {
				t.Errorf("exit = %d, want %d (stderr %q)", code, tt.want, stderr)
			}
		})
	}
	if readFile(t, file) != mediaGED {
		t.Error("input file changed")
	}
}

func TestMediaCheckSymlinkRoot(t *testing.T) {
	file, root := mediaFixture(t, mediaGED, "photos/ada.jpg", "photos/gone.jpg", "photos/ben.jpg", "photos/extra.jpg")
	link := filepath.Join(t.TempDir(), "media")
	if err := os.Symlink(root, link); err != nil {
		t.Skipf("cannot make a symlink: %v", err)
	}
	code, stdout, stderr := run(t, "media", "check", "--root", link, "--unreferenced", file)
	if code != ExitOK || stderr != "" {
		t.Fatalf("exit = %d, stderr = %q", code, stderr)
	}
	if !strings.HasSuffix(stdout, "ok\t@O2@\tline 31\tphotos/gone.jpg\nunreferenced\t\t\tphotos/extra.jpg\n") {
		t.Errorf("stdout =\n%s", stdout)
	}
}

func TestMediaCheckOutside(t *testing.T) {
	ged := strings.Replace(mediaGED, "1 FILE photos/gone.jpg", "1 FILE photos/../../outside.jpg", 1)
	file, root := mediaFixture(t, ged, "photos/ada.jpg", "photos/ben.jpg")
	if err := os.WriteFile(filepath.Join(filepath.Dir(root), "outside.jpg"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	code, stdout, stderr := run(t, "media", "check", "--root", root, "--unreferenced", file)
	if code != ExitOK || stderr != "" {
		t.Fatalf("exit = %d, stderr = %q", code, stderr)
	}
	if !strings.HasSuffix(stdout, "ok\t@O1@\tline 28\tphotos/ada.jpg\noutside\t@O2@\tline 31\tphotos/../../outside.jpg\n") {
		t.Errorf("stdout =\n%s", stdout)
	}
}

func TestMediaCheckOtherSpelling(t *testing.T) {
	file, root := mediaFixture(t, mediaGED, "photos/ADA.jpg", "photos/gone.jpg", "photos/ben.jpg")
	if _, err := os.Stat(filepath.Join(root, "photos", "ada.jpg")); err != nil {
		t.Skip("the filesystem is case-sensitive")
	}
	code, stdout, stderr := run(t, "media", "check", "--root", root, "--unreferenced", file)
	if code != ExitOK || stderr != "" {
		t.Fatalf("exit = %d, stderr = %q", code, stderr)
	}
	if strings.Contains(stdout, "unreferenced") {
		t.Errorf("a referenced file is listed as unreferenced:\n%s", stdout)
	}
}

func TestMediaAddSkipsDanglingXRef(t *testing.T) {
	ged := strings.Replace(mediaGED, "1 SEX M\n", "1 SEX M\n1 OBJE @O3@\n", 1)
	file, _ := mediaFixture(t, ged)
	out := filepath.Join(t.TempDir(), "out.ged")
	code, stdout, stderr := run(t, "media", "add", file, "--to", "F1", "--path", "photos/cara.jpg", "-o", out)
	if code != ExitOK || stderr != "" {
		t.Fatalf("exit = %d, stderr = %q", code, stderr)
	}
	if want := "@F1@\t@O4@\tcreated\tphotos/cara.jpg\n"; stdout != want {
		t.Errorf("stdout = %q, want %q", stdout, want)
	}
}

// -o - puts the GEDCOM on stdout and the report on stderr.
func TestMediaAddToStdout(t *testing.T) {
	file, root := mediaFixture(t, mediaGED, "photos/cara.jpg")
	out := filepath.Join(t.TempDir(), "out.ged")
	args := []string{"media", "add", file, "--root", root, "--to", "I2", "--path", "photos/cara.jpg", "-o"}
	if code, _, stderr := run(t, append(args, out)...); code != ExitOK {
		t.Fatalf("exit = %d, stderr = %q", code, stderr)
	}
	code, stdout, stderr := run(t, append(args, "-")...)
	if code != ExitOK {
		t.Fatalf("exit = %d, stderr = %q", code, stderr)
	}
	if stdout != readFile(t, out) {
		t.Errorf("stdout = %q, want the file -o writes", stdout)
	}
	if want := "@I2@\t@O3@\tcreated\tphotos/cara.jpg\n"; stderr != want {
		t.Errorf("stderr = %q, want %q", stderr, want)
	}
}

// An existing output is replaced by renaming a temporary file over it, never
// written in place: a hard link to the old file keeps the old bytes. The new
// file keeps the old one's permissions, and nothing else is left behind.
func TestMediaAddReplacesExistingOutput(t *testing.T) {
	file, _ := mediaFixture(t, mediaGED)
	dir := t.TempDir()
	out := filepath.Join(dir, "out.ged")
	if err := os.WriteFile(out, []byte("old\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(t.TempDir(), "link.ged")
	if err := os.Link(out, link); err != nil {
		t.Fatal(err)
	}
	code, _, stderr := run(t, "media", "add", file, "--to", "I2", "--path", "photos/cara.jpg", "-o", out)
	if code != ExitOK || stderr != "" {
		t.Fatalf("exit = %d, stderr = %q", code, stderr)
	}
	if got := readFile(t, out); !strings.Contains(got, "1 FILE photos/cara.jpg") {
		t.Errorf("output not replaced:\n%s", got)
	}
	if got := readFile(t, link); got != "old\n" {
		t.Errorf("the old file was written in place: %q", got)
	}
	if info, err := os.Stat(out); err != nil || info.Mode().Perm() != 0o600 {
		t.Errorf("replaced file mode = %v, %v; want 0600", info.Mode().Perm(), err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Errorf("want only out.ged in the directory, got %v", entries)
	}
}
