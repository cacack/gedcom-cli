package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// placesGED is a 5.5.1 file with places written several ways. Lines 48 (SOUR
// PUBL PLAC) and 51 (SOUR DATA EVEN PLAC) are where 5.5.1 permits no MAP.
const (
	placesGED   = "testdata/places.ged"
	placesTable = "testdata/places.tsv"
	springfield = "Springfield, Sangamon County, Illinois, USA"
)

// applyPlacesJSON runs places apply on in with table, returning the report
// and the file written.
func applyPlacesJSON(t *testing.T, in, table string) (placesReport, string) {
	t.Helper()
	out := filepath.Join(t.TempDir(), "out.ged")
	code, stdout, stderr := run(t, "places", "apply", "--format", "json", in, table, "-o", out)
	if code != ExitOK || stderr != "" {
		t.Fatalf("exit = %d, stderr = %q", code, stderr)
	}
	var report placesReport
	if err := json.Unmarshal([]byte(stdout), &report); err != nil {
		t.Fatalf("stdout is not JSON: %v\n%s", err, stdout)
	}
	got, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	return report, string(got)
}

// TestPlacesApplyChangesOnlyPlaces checks the whole output against the input:
// matched PLAC values are rewritten, MAP is added or updated where 5.5.1
// permits it, and every other line is as read.
func TestPlacesApplyChangesOnlyPlaces(t *testing.T) {
	report, got := applyPlacesJSON(t, placesGED, placesTable)
	input, err := os.ReadFile(placesGED)
	if err != nil {
		t.Fatal(err)
	}
	mapLines := "3 MAP\n4 LATI N39.7817\n4 LONG W89.6501\n"
	want := string(input)
	for _, r := range []struct{ old, new string }{
		// Already the standard name: no rewrite, but the MAP is added. First,
		// before other lines read the same.
		{"2 PLAC " + springfield + "\n", "2 PLAC " + springfield + "\n" + mapLines},
		// The MAP goes after the PLAC's other lines.
		{"2 PLAC Springfield, IL\n3 NOTE Written by the census taker\n", "2 PLAC " + springfield + "\n3 NOTE Written by the census taker\n" + mapLines},
		// Spacing is part of the key.
		{"2 PLAC Springfield,  Sangamon,   Illinois\n", "2 PLAC " + springfield + "\n" + mapLines},
		// An existing MAP takes the table's coordinates.
		{"2 PLAC Oak Ridge\n3 MAP\n4 LATI N1.0\n4 LONG W1.0\n", "2 PLAC Oak Ridge Cemetery, Springfield, Sangamon County, Illinois, USA\n3 MAP\n4 LATI N39.8208\n4 LONG W89.6559\n"},
		// No PLAC under PUBL, nor MAP under a 5.5.1 SOUR.DATA.EVEN.PLAC: the
		// value is rewritten, no MAP is added.
		{"2 PLAC Springfield, IL\n1 DATA\n2 EVEN BIRT\n3 PLAC Springfield, IL\n", "2 PLAC " + springfield + "\n1 DATA\n2 EVEN BIRT\n3 PLAC " + springfield + "\n"},
	} {
		if !strings.Contains(want, r.old) {
			t.Fatalf("fixture lacks %q", r.old)
		}
		want = strings.Replace(want, r.old, r.new, 1)
	}
	// Case is part of the key, and _PLAC is not PLAC.
	for _, kept := range []string{"2 PLAC springfield, il\n", "2 _PLAC Springfield, IL\n"} {
		if !strings.Contains(want, kept) {
			t.Fatalf("expected output lacks %q", kept)
		}
	}
	if got != want {
		t.Errorf("output:\n%s\nwant:\n%s", got, want)
	}

	wantReport := placesReport{
		Input:            placesGED,
		Table:            placesTable,
		Output:           report.Output,
		PlacesRewritten:  5,
		MapsAdded:        3,
		MapsUpdated:      1,
		MapsNotPermitted: []int{48, 51},
		RecordsChanged:   3,
		Unmatched:        []string{"Nowhere, Example County"},
	}
	if !reflect.DeepEqual(report, wantReport) {
		t.Errorf("report = %+v, want %+v", report, wantReport)
	}
}

func TestPlacesApplyText(t *testing.T) {
	out := filepath.Join(t.TempDir(), "out.ged")
	code, stdout, stderr := run(t, "places", "apply", placesGED, placesTable, "-o", out)
	if code != ExitOK || stderr != "" {
		t.Fatalf("exit = %d, stderr = %q", code, stderr)
	}
	want := "wrote " + out + ": 5 PLAC rewritten, 3 MAP added, 1 MAP updated, 3 records changed\n" +
		"  no MAP permitted\tline 48\n  no MAP permitted\tline 51\n" +
		"  unmatched\tNowhere, Example County\n"
	if stdout != want {
		t.Errorf("stdout = %q, want %q", stdout, want)
	}
}

// TestPlacesApplyListsCoordinates checks that list plac reads the coordinates
// written.
func TestPlacesApplyListsCoordinates(t *testing.T) {
	_, got := applyPlacesJSON(t, placesGED, placesTable)
	out := writeTemp(t, "out.ged", got)
	code, stdout, stderr := run(t, "list", "--format", "json", out, "plac")
	if code != ExitOK {
		t.Fatalf("exit = %d, stderr = %q", code, stderr)
	}
	var rows []placRow
	if err := json.Unmarshal([]byte(stdout), &rows); err != nil {
		t.Fatal(err)
	}
	want := map[string]placRow{
		springfield: {Count: 5, Place: springfield, Latitude: "N39.7817", Longitude: "W89.6501"},
		"Oak Ridge Cemetery, Springfield, Sangamon County, Illinois, USA": {Count: 1, Place: "Oak Ridge Cemetery, Springfield, Sangamon County, Illinois, USA", Latitude: "N39.8208", Longitude: "W89.6559"},
	}
	for _, r := range rows {
		if w, ok := want[r.Place]; ok && r != w {
			t.Errorf("row %+v, want %+v", r, w)
		}
		delete(want, r.Place)
	}
	if len(want) > 0 {
		t.Errorf("places not listed: %v", want)
	}
}

// TestPlacesApplyValidates checks that the output has no finding the input
// does not have.
func TestPlacesApplyValidates(t *testing.T) {
	_, got := applyPlacesJSON(t, placesGED, placesTable)
	out := writeTemp(t, "out.ged", got)
	codes := func(path string) map[string]int {
		_, stdout, stderr := run(t, "validate", "--format", "json", "--min-severity", "info", path)
		var report validateReport
		if err := json.Unmarshal([]byte(stdout), &report); err != nil {
			t.Fatalf("validate %s: %v, stderr %q", path, err, stderr)
		}
		counts := map[string]int{}
		for _, f := range report.Findings {
			counts[f.Code]++
		}
		return counts
	}
	before := codes(placesGED)
	for code, n := range codes(out) {
		if n > before[code] {
			t.Errorf("output has %d %s findings, input %d", n, code, before[code])
		}
	}
}

// TestPlacesApplyNoMAPIn55 checks that a GEDCOM 5.5 file, which has no MAP,
// gets none, while its values are still rewritten.
func TestPlacesApplyNoMAPIn55(t *testing.T) {
	in := writeTemp(t, "in.ged", "0 HEAD\n1 GEDC\n2 VERS 5.5\n2 FORM LINEAGE-LINKED\n1 CHAR ASCII\n"+
		"0 @I1@ INDI\n1 NAME Ada /Example/\n1 BIRT\n2 PLAC Springfield, IL\n0 TRLR\n")
	report, got := applyPlacesJSON(t, in, placesTable)
	if strings.Contains(got, "MAP") || !strings.Contains(got, "2 PLAC "+springfield+"\n") {
		t.Errorf("output:\n%s", got)
	}
	if report.MapsAdded != 0 || !reflect.DeepEqual(report.MapsNotPermitted, []int{9}) || report.PlacesRewritten != 1 {
		t.Errorf("report = %+v", report)
	}
}

// TestPlacesApplyEmptyTable checks that a table with no rows, with or without
// its header, writes the file byte-identical and reports nothing changed.
func TestPlacesApplyEmptyTable(t *testing.T) {
	input, err := os.ReadFile(placesGED)
	if err != nil {
		t.Fatal(err)
	}
	for name, table := range map[string]string{"empty": "", "header only": "place\tstandard\n"} {
		t.Run(name, func(t *testing.T) {
			report, got := applyPlacesJSON(t, placesGED, writeTemp(t, "table.tsv", table))
			assertSameBytes(t, []byte(got), input)
			if report.PlacesRewritten+report.MapsAdded+report.MapsUpdated+report.RecordsChanged != 0 || len(report.Unmatched) != 0 {
				t.Errorf("report = %+v", report)
			}
		})
	}
}

// TestPlacesApplyTableErrors checks that a malformed table is reported by
// line, exits 1 and writes nothing.
func TestPlacesApplyTableErrors(t *testing.T) {
	tests := []struct {
		name  string
		table string
		want  []string
	}{
		{"one coordinate", "place\tlatitude\tlongitude\nA\tN1.0\t\n", []string{"line 2: give both latitude and longitude"}},
		{"bad latitude", "place\tlatitude\tlongitude\nA\t39.78\tW1.0\nB\tN1.0\tW1.0\n", []string{"line 2: latitude \"39.78\""}},
		{"latitude given as longitude", "place\tlatitude\tlongitude\nA\tN1.0\tW1.0\nB\tW1.0\tN1.0\n", []string{"line 3: latitude \"W1.0\""}},
		{"bad longitude", "place\tlatitude\tlongitude\nA\tN1.0\tW1.0.5\n", []string{"line 2: longitude \"W1.0.5\""}},
		{"every row", "place\tlatitude\tlongitude\nA\tN1.0\t\n\nB\tN1\tX\n", []string{"line 2:", "line 4:"}},
		{"duplicate place", "place\tstandard\nA\tB\nA\tC\n", []string{"line 3: place \"A\" is already on line 2"}},
		{"empty place", "place\tstandard\n\tB\n", []string{"line 2: no place"}},
		{"no place column", "from\tto\nA\tB\n", []string{"line 1:", `"place" column`}},
		{"place column twice", "place\tnote\tplace\tnote\nA\t\tB\t\n", []string{"line 1:", `"place" is named twice`}},
		{"latitude column alone", "place\tlatitude\nA\tN1.0\n", []string{"line 1:", `"longitude"`}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			table := writeTemp(t, "table.tsv", tt.table)
			out := filepath.Join(t.TempDir(), "out.ged")
			code, stdout, stderr := run(t, "places", "apply", placesGED, table, "-o", out)
			if code != ExitError || stdout != "" {
				t.Fatalf("exit = %d, stdout = %q, want 1 and no report", code, stdout)
			}
			for _, want := range tt.want {
				if !strings.Contains(stderr, want) {
					t.Errorf("stderr lacks %q:\n%s", want, stderr)
				}
			}
			if _, err := os.Stat(out); !os.IsNotExist(err) {
				t.Errorf("output written: %v", err)
			}
		})
	}
}

// TestPlacesApplyStdout checks that -o - puts the file on stdout and the
// report on stderr.
func TestPlacesApplyStdout(t *testing.T) {
	code, stdout, stderr := run(t, "places", "apply", placesGED, placesTable, "-o", "-")
	if code != ExitOK {
		t.Fatalf("exit = %d, stderr = %q", code, stderr)
	}
	if !strings.HasPrefix(stdout, "0 HEAD\n") || !strings.HasPrefix(stderr, "wrote stdout: 5 PLAC rewritten") {
		t.Errorf("stdout = %q\nstderr = %q", stdout, stderr)
	}
}

func TestPlacesUsageErrors(t *testing.T) {
	for _, args := range [][]string{
		{"places", "apply", placesGED, placesTable},
		{"places", "apply", placesGED, "-o", "out.ged"},
		{"places", "apply", placesGED, placesTable, "-o", placesGED},
		{"places", "nope"},
	} {
		if code, _, stderr := run(t, args...); code != ExitUsage {
			t.Errorf("%v: exit = %d, stderr = %q; want a usage error", args, code, stderr)
		}
	}
}

// TestPlacesApplyAgain checks that applying a table to its own output changes
// nothing, so the pass can be run again as the table grows.
func TestPlacesApplyAgain(t *testing.T) {
	_, once := applyPlacesJSON(t, placesGED, placesTable)
	report, twice := applyPlacesJSON(t, writeTemp(t, "once.ged", once), placesTable)
	assertSameBytes(t, []byte(twice), []byte(once))
	if report.RecordsChanged != 0 {
		t.Errorf("report = %+v", report)
	}
}

// TestPlacesApplySkipsContinued checks that a PLAC continued on a CONC line is
// not matched on its first line, so its key is reported unmatched.
func TestPlacesApplySkipsContinued(t *testing.T) {
	input := "0 HEAD\n1 GEDC\n2 VERS 5.5.1\n2 FORM LINEAGE-LINKED\n1 CHAR UTF-8\n" +
		"0 @I1@ INDI\n1 NAME Ada /Example/\n1 BIRT\n2 PLAC Springfield,\n3 CONC  IL\n0 TRLR\n"
	in := writeTemp(t, "in.ged", input)
	table := writeTemp(t, "table.tsv", "place\tstandard\tlatitude\tlongitude\nSpringfield,\tX\tN1.0\tW1.0\n")
	report, got := applyPlacesJSON(t, in, table)
	assertSameBytes(t, []byte(got), []byte(input))
	if report.RecordsChanged != 0 || !reflect.DeepEqual(report.Unmatched, []string{"Springfield,"}) {
		t.Errorf("report = %+v", report)
	}
}
