package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// listCases is what each record type lists from the records fixture, in file
// order, with the living test made in referenceYear.
var listCases = []struct {
	kind string
	text string
	json []map[string]any
}{
	{
		kind: "indi",
		text: "@I1@\tCora /Sample/\t3 MAR 1901\t14 NOV 1978\tfalse\n" +
			"@I2@\tDell /Sample/\t21 JUL 1899\t2 FEB 1960\tfalse\n" +
			"@I3@\tEdie /Sample/\t9 SEP 1925\t\ttrue\n",
		json: []map[string]any{
			{"xref": "@I1@", "name": "Cora /Sample/", "birth": "3 MAR 1901", "death": "14 NOV 1978", "living": false},
			{"xref": "@I2@", "name": "Dell /Sample/", "birth": "21 JUL 1899", "death": "2 FEB 1960", "living": false},
			{"xref": "@I3@", "name": "Edie /Sample/", "birth": "9 SEP 1925", "death": "", "living": true},
		},
	},
	{
		kind: "fam",
		text: "@F1@\t@I2@\t@I1@\t12 JUN 1923\t1\n",
		json: []map[string]any{
			{"xref": "@F1@", "husband": "@I2@", "wife": "@I1@", "marriage": "12 JUN 1923", "children": float64(1)},
		},
	},
	{
		kind: "sour",
		text: "@S1@\tSample Family Bible\tInvented Author\n",
		json: []map[string]any{
			{"xref": "@S1@", "title": "Sample Family Bible", "author": "Invented Author"},
		},
	},
	{
		kind: "repo",
		text: "@R1@\tExample County Archive\n",
		json: []map[string]any{
			{"xref": "@R1@", "name": "Example County Archive"},
		},
	},
	{
		kind: "obje",
		text: "@O1@\tPortrait of Cora Sample\tphotos/cora.jpg\tjpg\n",
		json: []map[string]any{
			{"xref": "@O1@", "title": "Portrait of Cora Sample", "file": "photos/cora.jpg", "format": "jpg"},
		},
	},
}

func TestListText(t *testing.T) {
	for _, tt := range listCases {
		t.Run(tt.kind, func(t *testing.T) {
			code, stdout, stderr := run(t, "list", "--reference-year", referenceYear, records, tt.kind)
			if code != ExitOK || stderr != "" {
				t.Fatalf("exit = %d, stderr = %q", code, stderr)
			}
			if stdout != tt.text {
				t.Errorf("stdout =\n%s\nwant\n%s", stdout, tt.text)
			}
		})
	}
}

func TestListJSON(t *testing.T) {
	for _, tt := range listCases {
		t.Run(tt.kind, func(t *testing.T) {
			code, stdout, stderr := run(t, "list", "--format", "json", "--reference-year", referenceYear, records, tt.kind)
			if code != ExitOK || stderr != "" {
				t.Fatalf("exit = %d, stderr = %q", code, stderr)
			}
			var got []map[string]any
			if err := json.Unmarshal([]byte(stdout), &got); err != nil {
				t.Fatalf("stdout is not a JSON array of objects: %v\n%s", err, stdout)
			}
			if !reflect.DeepEqual(got, tt.json) {
				t.Errorf("got  %v\nwant %v", got, tt.json)
			}
		})
	}
}

func TestListTypeIsCaseInsensitive(t *testing.T) {
	_, want, _ := run(t, "list", records, "indi")
	for _, kind := range []string{"INDI", "Indi"} {
		code, stdout, stderr := run(t, "list", records, kind)
		if code != ExitOK || stdout != want {
			t.Errorf("%s: exit = %d, stderr = %q, stdout =\n%s", kind, code, stderr, stdout)
		}
	}
}

func TestListLimit(t *testing.T) {
	all := listCases[0].text
	tests := []struct {
		limit string
		want  int // lines
	}{
		{"0", 3},
		{"1", 1},
		{"2", 2},
		{"99", 3},
	}
	for _, tt := range tests {
		t.Run(tt.limit, func(t *testing.T) {
			code, stdout, stderr := run(t, "list", "--limit", tt.limit, "--reference-year", referenceYear, records, "indi")
			if code != ExitOK || stderr != "" {
				t.Fatalf("exit = %d, stderr = %q", code, stderr)
			}
			want := strings.Join(strings.SplitAfter(all, "\n")[:tt.want], "")
			if stdout != want {
				t.Errorf("stdout =\n%s\nwant the first %d records:\n%s", stdout, tt.want, want)
			}
		})
	}

	code, stdout, stderr := run(t, "list", "--limit", "2", "--format", "json", records, "indi")
	if code != ExitOK {
		t.Fatalf("exit = %d, stderr = %q", code, stderr)
	}
	var got []map[string]any
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("stdout is not a JSON array: %v\n%s", err, stdout)
	}
	if len(got) != 2 || got[0]["xref"] != "@I1@" || got[1]["xref"] != "@I2@" {
		t.Errorf("--limit 2 gave:\n%s", stdout)
	}
}

// TestListEmpty lists types the minimal fixture does not contain.
func TestListEmpty(t *testing.T) {
	for _, kind := range []string{"sour", "repo", "obje"} {
		code, stdout, stderr := run(t, "list", sample, kind)
		if code != ExitOK || stdout != "" || stderr != "" {
			t.Errorf("%s text: exit = %d, stdout = %q, stderr = %q", kind, code, stdout, stderr)
		}
		code, stdout, stderr = run(t, "list", "--format", "json", sample, kind)
		if code != ExitOK || stdout != "[]\n" || stderr != "" {
			t.Errorf("%s json: exit = %d, stdout = %q, stderr = %q; want []", kind, code, stdout, stderr)
		}
	}
}

// TestListSparseRecords covers absent values, a date the library cannot
// parse, and a multi-line value.
func TestListSparseRecords(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sparse.ged")
	const data = "0 HEAD\n1 GEDC\n2 VERS 5.5.1\n1 CHAR UTF-8\n" +
		"0 @I1@ INDI\n1 BIRT\n2 DATE sometime in spring\n" +
		"0 @F1@ FAM\n" +
		"0 @S1@ SOUR\n1 TITL Line one\n2 CONT line two\n" +
		"0 @O1@ OBJE\n" +
		"0 TRLR\n"
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	text := map[string]string{
		"indi": "@I1@\t\tsometime in spring\t\ttrue\n", // no date the library can place
		"fam":  "@F1@\t\t\t\t0\n",
		"sour": "@S1@\tLine one line two\t\n",
		"obje": "@O1@\t\t\t\n",
	}
	for kind, want := range text {
		code, stdout, stderr := run(t, "list", path, kind)
		if code != ExitOK || stdout != want {
			t.Errorf("%s: exit = %d, stderr = %q, stdout = %q, want %q", kind, code, stderr, stdout, want)
		}
	}

	// JSON keeps every key and the value as the library gives it.
	wantJSON := map[string]map[string]any{
		"fam":  {"xref": "@F1@", "husband": "", "wife": "", "marriage": "", "children": float64(0)},
		"sour": {"xref": "@S1@", "title": "Line one\nline two", "author": ""},
		"obje": {"xref": "@O1@", "title": "", "file": "", "format": ""},
	}
	for kind, want := range wantJSON {
		code, stdout, stderr := run(t, "list", "--format", "json", path, kind)
		if code != ExitOK {
			t.Fatalf("%s: exit = %d, stderr = %q", kind, code, stderr)
		}
		var got []map[string]any
		if err := json.Unmarshal([]byte(stdout), &got); err != nil {
			t.Fatalf("%s: stdout is not a JSON array: %v\n%s", kind, err, stdout)
		}
		if len(got) != 1 || !reflect.DeepEqual(got[0], want) {
			t.Errorf("%s: got %v, want [%v]", kind, got, want)
		}
	}
}

// TestListTextHasNoControlCharacters checks that an escape sequence in a file
// does not reach a terminal through text output.
func TestListTextHasNoControlCharacters(t *testing.T) {
	if got, want := oneLine("a\x1b[2Jb\r\nc\td"), "a [2Jb  c d"; got != want {
		t.Errorf("oneLine = %q, want %q", got, want)
	}

	path := filepath.Join(t.TempDir(), "escape.ged")
	const data = "0 HEAD\n1 GEDC\n2 VERS 5.5.1\n1 CHAR UTF-8\n" +
		"0 @S1@ SOUR\n1 TITL Before\x1b[2JAfter\n" +
		"0 TRLR\n"
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	code, stdout, stderr := run(t, "list", path, "sour")
	if code != ExitOK || !strings.Contains(stdout, "Before") || strings.ContainsRune(stdout, '\x1b') {
		t.Errorf("exit = %d, stderr = %q, stdout = %q", code, stderr, stdout)
	}
}

// places is a file whose PLAC values repeat, vary in spacing and case, carry
// coordinates on one use, and are absent on one line.
const places = "0 HEAD\n1 GEDC\n2 VERS 5.5.1\n1 CHAR UTF-8\n" +
	"0 @I1@ INDI\n1 NAME Cora /Sample/\n" +
	"1 BIRT\n2 PLAC Springfield, Example County, Ohio\n" +
	"1 DEAT\n2 PLAC Springfield,  Example,   Ohio\n" +
	"1 BURI\n2 PLAC\n" +
	"0 @I2@ INDI\n1 NAME Dell /Sample/\n" +
	"1 BIRT\n2 PLAC Springfield, Example County, Ohio\n3 MAP\n4 LATI N39.9\n4 LONG W83.8\n" +
	"1 DEAT\n2 PLAC springfield, example county, ohio\n" +
	"1 BURI\n2 PLAC Ohio SPRINGFIELD\n3 NOTE no coordinates\n4 LATI N1\n" +
	"0 @F1@ FAM\n1 MARR\n2 PLAC Springfield, Example County, Ohio\n" +
	"0 TRLR\n"

func writePlaces(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "places.ged")
	if err := os.WriteFile(path, []byte(places), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestListPlac(t *testing.T) {
	path := writePlaces(t)
	// Count descending, then byte order: uppercase sorts before lowercase.
	const want = "3\tSpringfield, Example County, Ohio\n" +
		"1\tOhio SPRINGFIELD\n" +
		"1\tSpringfield,  Example,   Ohio\n" +
		"1\tspringfield, example county, ohio\n"
	for _, kind := range []string{"plac", "PLAC"} {
		code, stdout, stderr := run(t, "list", path, kind)
		if code != ExitOK || stderr != "" {
			t.Fatalf("%s: exit = %d, stderr = %q", kind, code, stderr)
		}
		if stdout != want {
			t.Errorf("%s: stdout =\n%s\nwant\n%s", kind, stdout, want)
		}
	}

	code, stdout, stderr := run(t, "list", "--limit", "2", path, "plac")
	if code != ExitOK || stderr != "" {
		t.Fatalf("limit: exit = %d, stderr = %q", code, stderr)
	}
	if first := strings.Join(strings.SplitAfter(want, "\n")[:2], ""); stdout != first {
		t.Errorf("--limit 2: stdout =\n%s\nwant the two most used:\n%s", stdout, first)
	}
}

func TestListPlacJSON(t *testing.T) {
	code, stdout, stderr := run(t, "list", "--format", "json", writePlaces(t), "plac")
	if code != ExitOK || stderr != "" {
		t.Fatalf("exit = %d, stderr = %q", code, stderr)
	}
	var got []map[string]any
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("stdout is not a JSON array of objects: %v\n%s", err, stdout)
	}
	// Only the place with a MAP under a use of it has coordinates.
	want := []map[string]any{
		{"place": "Springfield, Example County, Ohio", "count": float64(3), "latitude": "N39.9", "longitude": "W83.8"},
		{"place": "Ohio SPRINGFIELD", "count": float64(1)},
		{"place": "Springfield,  Example,   Ohio", "count": float64(1)},
		{"place": "springfield, example county, ohio", "count": float64(1)},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got  %v\nwant %v", got, want)
	}
}

// TestListPlacKeepsValue checks that a place is listed as written: trailing
// space kept, and a control character a space in text only.
func TestListPlacKeepsValue(t *testing.T) {
	path := filepath.Join(t.TempDir(), "raw.ged")
	const data = "0 HEAD\n1 GEDC\n2 VERS 5.5.1\n1 CHAR UTF-8\n" +
		"0 @I1@ INDI\n1 BIRT\n2 PLAC Oldtown \n1 DEAT\n2 PLAC Oldtown\n1 BURI\n2 PLAC Old\x1b[2Jtown\n" +
		"0 TRLR\n"
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	code, stdout, stderr := run(t, "list", path, "plac")
	if want := "1\tOld [2Jtown\n1\tOldtown\n1\tOldtown \n"; code != ExitOK || stdout != want {
		t.Errorf("exit = %d, stderr = %q, stdout = %q, want %q", code, stderr, stdout, want)
	}
	code, stdout, stderr = run(t, "list", "--format", "json", path, "plac")
	if code != ExitOK || !strings.Contains(stdout, `"Old\u001b[2Jtown"`) || !strings.Contains(stdout, `"Oldtown "`) {
		t.Errorf("json: exit = %d, stderr = %q, stdout = %s", code, stderr, stdout)
	}
}

// TestListPlacCoordinates checks that coordinates come from the first use
// that gives both: a MAP with only a latitude is passed over.
func TestListPlacCoordinates(t *testing.T) {
	path := filepath.Join(t.TempDir(), "map.ged")
	const data = "0 HEAD\n1 GEDC\n2 VERS 5.5.1\n1 CHAR UTF-8\n" +
		"0 @I1@ INDI\n1 BIRT\n2 PLAC Oldtown\n3 MAP\n4 LATI N1\n" +
		"1 DEAT\n2 PLAC Oldtown\n3 MAP\n4 LATI N2\n4 LONG W3\n" +
		"1 BURI\n2 PLAC Oldtown\n3 MAP\n4 LATI N4\n4 LONG W5\n" +
		"0 TRLR\n"
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	code, stdout, stderr := run(t, "list", "--format", "json", path, "plac")
	if code != ExitOK || stderr != "" {
		t.Fatalf("exit = %d, stderr = %q", code, stderr)
	}
	var got []placRow
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("stdout is not a JSON array of places: %v\n%s", err, stdout)
	}
	if want := []placRow{{Count: 3, Place: "Oldtown", Latitude: "N2", Longitude: "W3"}}; !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

// TestListPlacEmpty lists the places of a file that has none: a PLAC with no
// value, or only whitespace, is not a place.
func TestListPlacEmpty(t *testing.T) {
	path := filepath.Join(t.TempDir(), "none.ged")
	const data = "0 HEAD\n1 GEDC\n2 VERS 5.5.1\n1 CHAR UTF-8\n0 @I1@ INDI\n1 BIRT\n2 PLAC\n1 DEAT\n2 PLAC \t\n0 TRLR\n"
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	code, stdout, stderr := run(t, "list", path, "plac")
	if code != ExitOK || stdout != "" || stderr != "" {
		t.Errorf("text: exit = %d, stdout = %q, stderr = %q", code, stdout, stderr)
	}
	code, stdout, stderr = run(t, "list", "--format", "json", path, "plac")
	if code != ExitOK || stdout != "[]\n" || stderr != "" {
		t.Errorf("json: exit = %d, stdout = %q, stderr = %q; want []", code, stdout, stderr)
	}
}

func TestListUnknownType(t *testing.T) {
	code, stdout, stderr := run(t, "list", records, "people")
	if code != ExitUsage {
		t.Errorf("exit = %d, want %d", code, ExitUsage)
	}
	if stdout != "" {
		t.Errorf("stdout = %q, want nothing", stdout)
	}
	for _, want := range append([]string{`"people"`}, listTypes...) {
		if !strings.Contains(stderr, want) {
			t.Errorf("stderr %q does not name %s", stderr, want)
		}
	}
}

func TestListExitCodes(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want int
	}{
		{"no arguments", []string{"list"}, ExitUsage},
		{"file without type", []string{"list", records}, ExitUsage},
		{"too many arguments", []string{"list", records, "indi", "fam"}, ExitUsage},
		{"negative limit", []string{"list", "--limit", "-1", records, "indi"}, ExitUsage},
		{"limit not a number", []string{"list", "--limit", "many", records, "indi"}, ExitUsage},
		{"bad format", []string{"list", "--format", "xml", records, "indi"}, ExitUsage},
		{"unknown type before the file is read", []string{"list", "testdata/does-not-exist.ged", "people"}, ExitUsage},
		{"missing file", []string{"list", "testdata/does-not-exist.ged", "indi"}, ExitError},
		{"missing file for plac", []string{"list", "testdata/does-not-exist.ged", "plac"}, ExitError},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code, stdout, stderr := run(t, tt.args...)
			if code != tt.want {
				t.Errorf("exit = %d, want %d (stderr %q)", code, tt.want, stderr)
			}
			if stdout != "" || stderr == "" {
				t.Errorf("stdout = %q, stderr = %q: want a diagnostic on stderr only", stdout, stderr)
			}
		})
	}
}

func TestListHelpHasExample(t *testing.T) {
	code, stdout, stderr := run(t, "list", "--help")
	if code != ExitOK || stderr != "" {
		t.Fatalf("exit = %d, stderr = %q", code, stderr)
	}
	for _, want := range []string{"gedcom list family.ged indi", "gedcom list family.ged plac", "plac  count, place"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("help lacks %q:\n%s", want, stdout)
		}
	}
}
