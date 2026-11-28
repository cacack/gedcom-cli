package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
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
	{
		kind: "subm",
		text: "@U1@\tTest Submitter\n",
		json: []map[string]any{
			{"xref": "@U1@", "name": "Test Submitter"},
		},
	},
}

// TestListNotes lists shared note records: NOTE in a 5.5.1 file, SNOTE in a
// 7.0 one. A multi-line text is one line in text output and kept in JSON.
func TestListNotes(t *testing.T) {
	tests := []struct {
		kind, data string
	}{
		{"note", "0 HEAD\n1 GEDC\n2 VERS 5.5.1\n1 CHAR UTF-8\n" +
			"0 @N1@ NOTE First note\n1 CONT second line\n0 @N2@ NOTE Another\n0 TRLR\n"},
		{"snote", "0 HEAD\n1 GEDC\n2 VERS 7.0\n" +
			"0 @N1@ SNOTE First note\n1 CONT second line\n0 @N2@ SNOTE Another\n0 TRLR\n"},
	}
	for _, tt := range tests {
		t.Run(tt.kind, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), tt.kind+".ged")
			if err := os.WriteFile(path, []byte(tt.data), 0o600); err != nil {
				t.Fatal(err)
			}
			code, stdout, stderr := run(t, "list", path, tt.kind)
			if want := "@N1@\tFirst note second line\n@N2@\tAnother\n"; code != ExitOK || stderr != "" || stdout != want {
				t.Errorf("text: exit = %d, stderr = %q, stdout = %q, want %q", code, stderr, stdout, want)
			}
			code, stdout, stderr = run(t, "list", "--format", "json", path, tt.kind)
			if code != ExitOK || stderr != "" {
				t.Fatalf("json: exit = %d, stderr = %q", code, stderr)
			}
			var got []map[string]any
			if err := json.Unmarshal([]byte(stdout), &got); err != nil {
				t.Fatalf("stdout is not a JSON array: %v\n%s", err, stdout)
			}
			want := []map[string]any{
				{"xref": "@N1@", "text": "First note\nsecond line"},
				{"xref": "@N2@", "text": "Another"},
			}
			if !reflect.DeepEqual(got, want) {
				t.Errorf("json: got %v, want %v", got, want)
			}
		})
	}
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
	for _, kind := range []string{"sour", "repo", "obje", "note", "snote"} {
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

// tagged is a file with vendor tags, an extension record, a continued note,
// a header the counts leave out, and EVEN TYPE values that repeat, differ in
// case or are absent.
const tagged = "0 HEAD\n1 SOUR gedcom-cli-test\n1 GEDC\n2 VERS 5.5.1\n1 CHAR UTF-8\n" +
	"0 @I1@ INDI\n1 NAME Cora /Sample/\n1 SEX F\n" +
	"1 EVEN\n2 TYPE Census\n1 EVEN\n2 TYPE census\n" +
	"1 _MILT\n2 DATE 1918\n" +
	"1 NOTE First line\n2 CONT second line\n" +
	"0 @I2@ INDI\n1 NAME Dell /Sample/\n1 SEX M\n" +
	"1 EVEN\n2 TYPE Census\n1 EVEN\n2 TYPE\n" +
	"0 @L1@ _LOC\n1 NAME Example Hall\n" +
	"0 TRLR\n"

func writeTagged(t *testing.T) string {
	t.Helper()
	return writeTemp(t, "tagged.ged", tagged)
}

func TestListTags(t *testing.T) {
	path := writeTagged(t)
	// Count descending, then path in byte order.
	const want = "4\tINDI.EVEN\n" +
		"4\tINDI.EVEN.TYPE\n" +
		"2\tINDI.NAME\n" +
		"2\tINDI.SEX\n" +
		"1\tINDI.NOTE\n" +
		"1\tINDI.NOTE.CONT\n" +
		"1\tINDI._MILT\n" +
		"1\tINDI._MILT.DATE\n" +
		"1\t_LOC.NAME\n"
	code, stdout, stderr := run(t, "list", path, "tags")
	if code != ExitOK || stderr != "" {
		t.Fatalf("exit = %d, stderr = %q", code, stderr)
	}
	if stdout != want {
		t.Errorf("stdout:\n%s\nwant:\n%s", stdout, want)
	}

	// The counts are every line but the header's, the trailer and the
	// level-0 lines.
	lines := strings.Split(strings.TrimSuffix(tagged, "\n"), "\n")
	inHeader, wantSum := false, 0
	for _, line := range lines {
		if strings.HasPrefix(line, "0 ") {
			inHeader = line == "0 HEAD"
			continue
		}
		if !inHeader {
			wantSum++
		}
	}
	sum := 0
	for _, line := range strings.Split(strings.TrimSuffix(stdout, "\n"), "\n") {
		n, err := strconv.Atoi(strings.Split(line, "\t")[0])
		if err != nil {
			t.Fatal(err)
		}
		sum += n
	}
	if sum != wantSum {
		t.Errorf("counts sum to %d, want %d", sum, wantSum)
	}
}

func TestListTagsVendor(t *testing.T) {
	code, stdout, stderr := run(t, "list", "--vendor", writeTagged(t), "tags")
	if want := "1\tINDI._MILT\n1\tINDI._MILT.DATE\n1\t_LOC.NAME\n"; code != ExitOK || stderr != "" || stdout != want {
		t.Errorf("exit = %d, stderr = %q, stdout:\n%s\nwant:\n%s", code, stderr, stdout, want)
	}
}

func TestListTagsSkippedLevel(t *testing.T) {
	// gedcom-go reads a 3 directly under a 1 as a 2, so tags and values agree
	// on its path.
	path := writeTemp(t, "skipped.ged", "0 HEAD\n1 GEDC\n2 VERS 5.5.1\n0 @I1@ INDI\n1 BIRT\n3 DATE 1900\n0 TRLR\n")
	code, stdout, stderr := run(t, "list", path, "tags")
	if want := "1\tINDI.BIRT\n1\tINDI.BIRT.DATE\n"; code != ExitOK || stderr != "" || stdout != want {
		t.Errorf("tags: exit = %d, stderr = %q, stdout = %q, want %q", code, stderr, stdout, want)
	}
	code, stdout, stderr = run(t, "list", "--tag", "INDI.BIRT.DATE", path, "values")
	if want := "1\t1900\n"; code != ExitOK || stderr != "" || stdout != want {
		t.Errorf("values: exit = %d, stderr = %q, stdout = %q, want %q", code, stderr, stdout, want)
	}
}

func TestListTagsLimitAndJSON(t *testing.T) {
	code, stdout, stderr := run(t, "list", "--format", "json", "--limit", "3", writeTagged(t), "tags")
	if code != ExitOK || stderr != "" {
		t.Fatalf("exit = %d, stderr = %q", code, stderr)
	}
	var got []map[string]any
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("stdout is not JSON: %v\n%s", err, stdout)
	}
	// --limit applies after sorting: the three most used paths.
	want := []map[string]any{
		{"count": float64(4), "path": "INDI.EVEN"},
		{"count": float64(4), "path": "INDI.EVEN.TYPE"},
		{"count": float64(2), "path": "INDI.NAME"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestListValues(t *testing.T) {
	path := writeTagged(t)
	// Values differing in case are separate; the TYPE with no value is left out.
	code, stdout, stderr := run(t, "list", "--tag", "INDI.EVEN.TYPE", path, "values")
	if want := "2\tCensus\n1\tcensus\n"; code != ExitOK || stderr != "" || stdout != want {
		t.Errorf("exit = %d, stderr = %q, stdout:\n%s\nwant:\n%s", code, stderr, stdout, want)
	}
	// A value is its line's own, without the CONT line continuing it.
	code, stdout, stderr = run(t, "list", "--tag", "INDI.NOTE", path, "values")
	if want := "1\tFirst line\n"; code != ExitOK || stderr != "" || stdout != want {
		t.Errorf("note: exit = %d, stderr = %q, stdout = %q, want %q", code, stderr, stdout, want)
	}
	// Another record type's NAME is not INDI.NAME.
	code, stdout, stderr = run(t, "list", "--format", "json", "--limit", "1", "--tag", "INDI.NAME", path, "values")
	if code != ExitOK || stderr != "" {
		t.Fatalf("json: exit = %d, stderr = %q", code, stderr)
	}
	var got []map[string]any
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("stdout is not JSON: %v\n%s", err, stdout)
	}
	if want := []map[string]any{{"count": float64(1), "value": "Cora /Sample/"}}; !reflect.DeepEqual(got, want) {
		t.Errorf("json: got %v, want %v", got, want)
	}
	// A path that matches nothing is an empty list.
	code, stdout, stderr = run(t, "list", "--format", "json", "--tag", "FAM.MARR", path, "values")
	if code != ExitOK || stdout != "[]\n" || stderr != "" {
		t.Errorf("none: exit = %d, stdout = %q, stderr = %q; want []", code, stdout, stderr)
	}
}

// withMedia is the media record of the issue asking for --with, a second with
// a note continued and repeated, and a third with none of those lines.
const withMedia = "0 HEAD\n1 SOUR gedcom-cli-test\n1 GEDC\n2 VERS 5.5.1\n1 CHAR UTF-8\n" +
	"0 @O1@ OBJE\n1 FILE ada.jpg\n2 FORM image/jpeg\n3 _SIZE 20480\n2 TITL Ada, 1920\n1 _OID 0001\n" +
	"0 @O2@ OBJE\n1 FILE ben.jpg\n1 NOTE First\n2 CONT second\n2 CONC  line\n1 NOTE Other\n" +
	"0 @O3@ OBJE\n1 FILE cora.jpg\n0 TRLR\n"

func TestListWith(t *testing.T) {
	path := writeTemp(t, "media.ged", withMedia)
	// The columns follow the type's own fields in the order given; a record
	// without the line has an empty value, several values are joined, and a
	// line break is a space.
	code, stdout, stderr := run(t, "list", "--with", "OBJE.FILE.FORM._SIZE", "--with", "OBJE._OID", "--with", "OBJE.NOTE", path, "obje")
	want := "@O1@\tAda, 1920\tada.jpg\timage/jpeg\t20480\t0001\t\n" +
		"@O2@\t\tben.jpg\t\t\t\tFirst second line | Other\n" +
		"@O3@\t\tcora.jpg\t\t\t\t\n"
	if code != ExitOK || stderr != "" || stdout != want {
		t.Errorf("exit = %d, stderr = %q, stdout:\n%q\nwant:\n%q", code, stderr, stdout, want)
	}
	code, stdout, stderr = run(t, "list", "--format", "json", "--limit", "2", "--with", "OBJE._OID", "--with", "OBJE.NOTE", path, "obje")
	if code != ExitOK || stderr != "" {
		t.Fatalf("json: exit = %d, stderr = %q", code, stderr)
	}
	var got []map[string]any
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("stdout is not JSON: %v\n%s", err, stdout)
	}
	wantJSON := []map[string]any{
		{"xref": "@O1@", "title": "Ada, 1920", "file": "ada.jpg", "format": "image/jpeg",
			"with": map[string]any{"OBJE._OID": []any{"0001"}, "OBJE.NOTE": []any{}}},
		{"xref": "@O2@", "title": "", "file": "ben.jpg", "format": "",
			"with": map[string]any{"OBJE._OID": []any{}, "OBJE.NOTE": []any{"First\nsecond line", "Other"}}},
	}
	if !reflect.DeepEqual(got, wantJSON) {
		t.Errorf("json: got %v, want %v", got, wantJSON)
	}
	// Without --with, a row has no with key.
	_, stdout, _ = run(t, "list", "--format", "json", path, "obje")
	if strings.Contains(stdout, `"with"`) {
		t.Errorf("json without --with has a with key:\n%s", stdout)
	}
	// Every record type takes it, each column holding its own path's values.
	for _, tt := range []struct {
		kind string
		with []string
		want string
	}{
		{"indi", []string{"INDI.SEX", "INDI.BIRT.PLAC"}, "@I1@\tCora /Sample/\t3 MAR 1901\t14 NOV 1978\tfalse\tF\tSpringfield, Example County\n" +
			"@I2@\tDell /Sample/\t21 JUL 1899\t2 FEB 1960\tfalse\tM\t\n" +
			"@I3@\tEdie /Sample/\t9 SEP 1925\t\ttrue\tF\t\n"},
		{"fam", []string{"FAM.CHIL", "FAM.MARR.PLAC"}, "@F1@\t@I2@\t@I1@\t12 JUN 1923\t1\t@I3@\t\n"},
		{"sour", []string{"SOUR.REPO"}, "@S1@\tSample Family Bible\tInvented Author\t@R1@\n"},
		{"repo", []string{"REPO.NAME"}, "@R1@\tExample County Archive\tExample County Archive\n"},
		{"subm", []string{"SUBM.NAME"}, "@U1@\tTest Submitter\tTest Submitter\n"},
	} {
		args := []string{"list"}
		for _, with := range tt.with {
			args = append(args, "--with", with)
		}
		code, stdout, stderr = run(t, append(args, records, tt.kind)...)
		if code != ExitOK || stderr != "" || stdout != tt.want {
			t.Errorf("%s: exit = %d, stderr = %q, stdout:\n%q\nwant:\n%q", tt.kind, code, stderr, stdout, tt.want)
		}
	}
	for kind, header := range map[string]string{"note": "5.5.1\n1 CHAR UTF-8", "snote": "7.0"} {
		tag := strings.ToUpper(kind)
		notes := writeTemp(t, kind+".ged", "0 HEAD\n1 GEDC\n2 VERS "+header+"\n"+
			"0 @N1@ "+tag+" First note\n1 REFN 7\n0 @N2@ "+tag+" Another\n0 TRLR\n")
		code, stdout, stderr = run(t, "list", "--with", tag+".REFN", notes, kind)
		if want := "@N1@\tFirst note\t7\n@N2@\tAnother\t\n"; code != ExitOK || stderr != "" || stdout != want {
			t.Errorf("%s: exit = %d, stderr = %q, stdout = %q, want %q", kind, code, stderr, stdout, want)
		}
	}
}

func TestListWithWrongPath(t *testing.T) {
	path := writeTemp(t, "media.ged", withMedia)
	for _, with := range []string{"INDI.NAME", "OBJE", "OBJE..FILE", "obje._OID"} {
		code, stdout, stderr := run(t, "list", "--with", "OBJE._OID", "--with", with, path, "obje")
		if code != ExitUsage || stdout != "" || !strings.Contains(stderr, `"`+with+`"`) {
			t.Errorf("--with %s: exit = %d, stdout = %q, stderr = %q; want %d naming the path", with, code, stdout, stderr, ExitUsage)
		}
	}
	code, stdout, stderr := run(t, "list", "--with", "OBJE._OID", "--with", "OBJE._OID", path, "obje")
	if code != ExitUsage || stdout != "" || !strings.Contains(stderr, `"OBJE._OID"`) {
		t.Errorf("repeated path: exit = %d, stdout = %q, stderr = %q; want %d naming the path", code, stdout, stderr, ExitUsage)
	}
	code, stdout, stderr = run(t, "list", "--with", "INDI.NAME", path, "names")
	if code != ExitUsage || stdout != "" || !strings.Contains(stderr, "--with") {
		t.Errorf("names: exit = %d, stdout = %q, stderr = %q", code, stdout, stderr)
	}
}

// named is a file whose first person has three names: the second repeats the
// first but for case, spacing and punctuation and carries other citations,
// the third differs. Its second person has one name.
const named = "0 HEAD\n1 SOUR gedcom-cli-test\n1 GEDC\n2 VERS 5.5.1\n1 CHAR UTF-8\n" +
	"0 @S1@ SOUR\n1 TITL Example register\n" +
	"0 @I1@ INDI\n" +
	"1 NAME John /Sample/ Jr.\n2 GIVN John\n2 SURN Sample\n2 NSFX Jr.\n2 SOUR @S1@\n3 PAGE 1\n" +
	"1 NAME john  sample, jr\n2 SOUR @S1@\n3 PAGE 2\n2 SOUR @S1@\n3 PAGE 3\n" +
	"1 NAME Jack /Sample/\n2 NICK Jack\n2 TYPE aka\n" +
	"1 SEX M\n" +
	"0 @I2@ INDI\n1 NAME Ann /Sample/\n2 GIVN Anne\n1 SEX F\n" +
	"0 TRLR\n"

func TestListNames(t *testing.T) {
	path := writeTemp(t, "named.ged", named)
	// One row per NAME, in file order; an absent part is empty, not taken
	// from the value.
	const want = "@I1@\t1\tJohn /Sample/ Jr.\tJohn\tSample\tJr.\t\t\t1\t0\n" +
		"@I1@\t2\tjohn  sample, jr\t\t\t\t\t\t2\t1\n" +
		"@I1@\t3\tJack /Sample/\t\t\t\tJack\taka\t0\t0\n" +
		"@I2@\t1\tAnn /Sample/\tAnne\t\t\t\t\t0\t0\n"
	code, stdout, stderr := run(t, "list", path, "names")
	if code != ExitOK || stderr != "" || stdout != want {
		t.Errorf("exit = %d, stderr = %q, stdout:\n%s\nwant:\n%s", code, stderr, stdout, want)
	}

	code, stdout, stderr = run(t, "list", "--format", "json", "--limit", "3", path, "names")
	if code != ExitOK || stderr != "" {
		t.Fatalf("json: exit = %d, stderr = %q", code, stderr)
	}
	var got []map[string]any
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("stdout is not JSON: %v\n%s", err, stdout)
	}
	name := func(position float64, value, givn, surn, nsfx, nick, typ string, citations, sameAs float64) map[string]any {
		return map[string]any{"xref": "@I1@", "position": position, "value": value, "givn": givn, "surn": surn,
			"nsfx": nsfx, "nick": nick, "type": typ, "citations": citations, "same_as": sameAs}
	}
	wantJSON := []map[string]any{
		name(1, "John /Sample/ Jr.", "John", "Sample", "Jr.", "", "", 1, 0),
		name(2, "john  sample, jr", "", "", "", "", "", 2, 1),
		name(3, "Jack /Sample/", "", "", "", "Jack", "aka", 0, 0),
	}
	if !reflect.DeepEqual(got, wantJSON) {
		t.Errorf("json: got %v, want %v", got, wantJSON)
	}
}

func TestListNamesEmptyValue(t *testing.T) {
	// A NAME with no value is still a row, and the parts stay with their own
	// NAME line.
	path := writeTemp(t, "empty.ged", "0 HEAD\n1 GEDC\n2 VERS 5.5.1\n1 CHAR UTF-8\n"+
		"0 @I1@ INDI\n1 NAME\n2 GIVN Ann\n1 NAME Bob /Sample/\n2 SURN Sample\n0 TRLR\n")
	const want = "@I1@\t1\t\tAnn\t\t\t\t\t0\t0\n@I1@\t2\tBob /Sample/\t\tSample\t\t\t\t0\t0\n"
	code, stdout, stderr := run(t, "list", path, "names")
	if code != ExitOK || stderr != "" || stdout != want {
		t.Errorf("exit = %d, stderr = %q, stdout = %q, want %q", code, stderr, stdout, want)
	}
}

func TestListNamesRepeated(t *testing.T) {
	path := writeTemp(t, "named.ged", named)
	code, stdout, stderr := run(t, "list", "--repeated", path, "names")
	if want := "@I1@\t2\tjohn  sample, jr\t\t\t\t\t\t2\t1\n"; code != ExitOK || stderr != "" || stdout != want {
		t.Errorf("exit = %d, stderr = %q, stdout = %q, want %q", code, stderr, stdout, want)
	}
	// A file with no repeated name reports nothing.
	for _, format := range []string{"text", "json"} {
		code, stdout, stderr = run(t, "list", "--repeated", "--format", format, records, "names")
		want := ""
		if format == "json" {
			want = "[]\n"
		}
		if code != ExitOK || stderr != "" || stdout != want {
			t.Errorf("%s, none: exit = %d, stderr = %q, stdout = %q, want %q", format, code, stderr, stdout, want)
		}
	}
}

func TestListNamesSameAsWords(t *testing.T) {
	// Values are equal when they hold the same words in the same order:
	// "OBrien" is one word and "O'Brien" two, and order counts.
	path := writeTemp(t, "words.ged", "0 HEAD\n1 GEDC\n2 VERS 5.5.1\n1 CHAR UTF-8\n"+
		"0 @I1@ INDI\n1 NAME Ann /O'Brien/\n1 NAME Ann /OBrien/\n1 NAME O'Brien Ann\n1 NAME ANN O BRIEN\n0 TRLR\n")
	code, stdout, stderr := run(t, "list", "--repeated", path, "names")
	if want := "@I1@\t4\tANN O BRIEN\t\t\t\t\t\t0\t1\n"; code != ExitOK || stderr != "" || stdout != want {
		t.Errorf("exit = %d, stderr = %q, stdout = %q, want %q", code, stderr, stdout, want)
	}
}

func TestListNamesRedactLiving(t *testing.T) {
	// Both people in named have no dates, so both are presumed living: each
	// lists one name, Living, and no name is repeated.
	path := writeTemp(t, "named.ged", named)
	code, stdout, stderr := run(t, "list", "--redact-living", path, "names")
	if want := "@I1@\t1\tLiving\t\t\t\t\t\t0\t0\n@I2@\t1\tLiving\t\t\t\t\t\t0\t0\n"; code != ExitOK || stderr != "" || stdout != want {
		t.Errorf("exit = %d, stderr = %q, stdout = %q, want %q", code, stderr, stdout, want)
	}
	code, stdout, stderr = run(t, "list", "--redact-living", "--repeated", path, "names")
	if code != ExitOK || stderr != "" || stdout != "" {
		t.Errorf("repeated: exit = %d, stderr = %q, stdout = %q, want nothing", code, stderr, stdout)
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
		{"values without --tag", []string{"list", records, "values"}, ExitUsage},
		{"--tag naming a record", []string{"list", "--tag", "INDI", records, "values"}, ExitUsage},
		{"--tag with an empty tag", []string{"list", "--tag", "INDI..DATE", records, "values"}, ExitUsage},
		{"--tag on another type", []string{"list", "--tag", "INDI.SEX", records, "tags"}, ExitUsage},
		{"--vendor on another type", []string{"list", "--vendor", records, "plac"}, ExitUsage},
		{"--repeated on another type", []string{"list", "--repeated", records, "indi"}, ExitUsage},
		{"missing file for tags", []string{"list", "testdata/does-not-exist.ged", "tags"}, ExitError},
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
	for _, want := range []string{"gedcom list family.ged indi", "gedcom list family.ged plac", "plac   count, place",
		"subm   xref, name", "snote  xref, text", "tags   count, path", "values count, value", "but SUBN",
		"names  xref, position, value, givn, surn, nsfx, nick, type, citations", "same_as", samePersonalNameHelp, "repeated-names", "NAME_PART_MISMATCH", "gedcom list --repeated family.ged names",
		"--with", "in the order the paths are given", "CONT lines folded in", `joined by " | "`, `"with" object`} {
		if !strings.Contains(stdout, want) {
			t.Errorf("help lacks %q:\n%s", want, stdout)
		}
	}
}
