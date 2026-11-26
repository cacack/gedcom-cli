package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// moveValueGED is a 5.5.1 file with values on the wrong line of an event: a
// building's name in an ADDR beside a PLAC with a MAP, PLAC values that are
// not places, a street address in a PLAC beside an ADDR, an ADDR with a CITY
// under it, and ADDR values continued with CONT and with CONC.
const moveValueGED = "testdata/move-value.ged"

// runMoveValue runs move-value on moveValueGED with a table of rows (the
// header row is added), returning the exit code, the file written ("" if
// none) and stdout.
func runMoveValue(t *testing.T, rows string, args ...string) (code int, written, stdout string) {
	t.Helper()
	table := writeTemp(t, "moves.tsv", "path\tvalue\taction\tprefix\n"+rows)
	out := filepath.Join(t.TempDir(), "out.ged")
	code, stdout, _ = run(t, append(append([]string{"move-value"}, args...), moveValueGED, table, "-o", out)...)
	got, err := os.ReadFile(out)
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	return code, string(got), stdout
}

// TestMoveValueChangesOnlyTheMatchedLines checks the whole output against
// the input: the matched lines move, and every other line is as read.
func TestMoveValueChangesOnlyTheMatchedLines(t *testing.T) {
	input := readFixture(t, moveValueGED)
	tests := []struct {
		name string
		rows string
		want string
		text []string
	}{
		{"prepend PLAC", "INDI.BIRT.ADDR\tExample General Hospital\tprepend PLAC\t\n",
			replaceOnce(t, input, "2 PLAC Springfield, Example, Ohio, USA\n3 MAP\n4 LATI N39.9242\n4 LONG W83.8088\n2 ADDR Example General Hospital\n",
				"2 PLAC Example General Hospital, Springfield, Example, Ohio, USA\n"),
			[]string{"wrote ", ": 1 line moved, 1 MAP removed, 1 record changed\n",
				"  row 2\tINDI.BIRT.ADDR\tExample General Hospital\tprepend PLAC\t1 moved, 1 MAP removed, 1 record changed\n"}},
		// Every event's PLAC with the value, in every record.
		{"to NOTE with a prefix", "*.PLAC\tat home\tto NOTE\tPlace given as: \n",
			strings.ReplaceAll(input, "2 PLAC at home\n", "2 NOTE Place given as: at home\n"),
			[]string{": 3 lines moved, 0 MAP removed, 3 records changed\n", "  row 2\t*.PLAC\tat home\tto NOTE\t3 moved, 0 MAP removed, 3 records changed\n"}},
		{"to PLAC", "INDI.BURI.ADDR\tExample Chapel\tto PLAC\t\n",
			replaceOnce(t, input, "2 ADDR Example Chapel\n", "2 PLAC Example Chapel\n"),
			[]string{": 1 line moved, 0 MAP removed, 1 record changed\n"}},
		{"to ADDR", "FAM.MARR.PLAC\tat home\tto ADDR\t\n",
			replaceOnce(t, input, "1 MARR\n2 PLAC at home\n", "1 MARR\n2 ADDR at home\n"),
			[]string{": 1 line moved, "}},
		// A value continued with CONC is matched, and moved, whole.
		{"continued with CONC", "*.ADDR\tExample School\tto NOTE\tAddress given as: \n",
			replaceOnce(t, input, "2 ADDR Example Sch\n3 CONC ool\n", "2 NOTE Address given as: Example School\n"),
			[]string{": 1 line moved, "}},
		// The first line of a value continued with CONT is not the value.
		{"continued with CONT", "*.ADDR\tat the\tto NOTE\t\nINDI.BIRT.ADDR\tNowhere\tto NOTE\t\n", input,
			[]string{": 0 lines moved, 0 MAP removed, 0 records changed\n", "  unmatched\trow 2\t*.ADDR\tat the\n", "  unmatched\trow 3\tINDI.BIRT.ADDR\tNowhere\n"}},
		// A path is exact: the family's PLAC is not at INDI.RESI.PLAC.
		{"exact path", "INDI.RESI.PLAC\tat home\tto NOTE\t\n",
			strings.Replace(input, "1 RESI\n2 PLAC at home\n", "1 RESI\n2 NOTE at home\n", 2),
			[]string{": 2 lines moved, 0 MAP removed, 2 records changed\n"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code, got, stdout := runMoveValue(t, tt.rows)
			if code != ExitOK {
				t.Fatalf("exit = %d, stdout = %q", code, stdout)
			}
			if got != tt.want {
				t.Errorf("output:\n%s\nwant:\n%s", got, tt.want)
			}
			for _, want := range tt.text {
				if !strings.Contains(stdout, want) {
					t.Errorf("stdout lacks %q:\n%s", want, stdout)
				}
			}
		})
	}
}

// TestMoveValueRefused checks that a matched line the action cannot be
// applied to stops the write, naming the record and the line.
func TestMoveValueRefused(t *testing.T) {
	tests := []struct {
		name, rows, want string
	}{
		{"to ADDR on an event with an ADDR", "INDI.DEAT.PLAC\t12 Example Street\tto ADDR\t\n", "  event has ADDR\t@I1@\tline 19\tPLAC\trow 2\n"},
		{"prepend PLAC with no PLAC", "INDI.BURI.ADDR\tExample Chapel\tprepend PLAC\t\n", "  event has no PLAC\t@I1@\tline 22\tADDR\trow 2\n"},
		{"a MAP under the line", "*.PLAC\tUnknown\tto NOTE\t\n", "  line has MAP under it\t@I1@\tline 24\tPLAC\trow 2\n"},
		{"a CITY under the line", "*.ADDR\tExample Hall\tto NOTE\t\n", "  line has CITY under it\t@I2@\tline 39\tADDR\trow 2\n"},
		// The event's PLAC is itself moved.
		{"prepend to a moved PLAC", "INDI.DEAT.ADDR\tExample Hospice\tprepend PLAC\t\nINDI.DEAT.PLAC\t12 Example Street\tto NOTE\t\n", "  event's PLAC is moved, or prepended to twice\t@I1@\tline 20\tADDR\trow 2\n"},
		// One record's RESI takes the ADDR; the other's has one.
		{"one of two events has an ADDR", "INDI.RESI.PLAC\tat home\tto ADDR\t\n", "  event has ADDR\t@I2@\tline 38\tPLAC\trow 2\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code, got, stdout := runMoveValue(t, tt.rows)
			if code != ExitError {
				t.Fatalf("exit = %d, want %d, stdout = %q", code, ExitError, stdout)
			}
			if got != "" {
				t.Errorf("a file was written:\n%s", got)
			}
			if !strings.Contains(stdout, " not written: ") || !strings.Contains(stdout, tt.want) {
				t.Errorf("stdout lacks %q:\n%s", tt.want, stdout)
			}
		})
	}
}

// TestMoveValueTwoLinesToOneLine checks that two lines of one event moved to
// the same line are refused, the second as occupied.
func TestMoveValueTwoLinesToOneLine(t *testing.T) {
	in := writeTemp(t, "in.ged", "0 HEAD\n1 GEDC\n2 VERS 5.5.1\n2 FORM LINEAGE-LINKED\n1 CHAR UTF-8\n"+
		"0 @I1@ INDI\n1 RESI\n2 NOTE Example Hall\n2 NOTE Example Lodge\n0 TRLR\n")
	table := writeTemp(t, "moves.tsv", "path\tvalue\taction\nINDI.RESI.NOTE\tExample Hall\tto PLAC\nINDI.RESI.NOTE\tExample Lodge\tto PLAC\n")
	out := filepath.Join(t.TempDir(), "out.ged")
	code, stdout, _ := run(t, "move-value", in, table, "-o", out)
	if code != ExitError || !strings.Contains(stdout, "  event has PLAC\t@I1@\tline 9\tNOTE\trow 3\n") {
		t.Errorf("exit = %d, stdout = %q", code, stdout)
	}
	if _, err := os.Stat(out); !os.IsNotExist(err) {
		t.Errorf("a file was written: %v", err)
	}
}

// TestMoveValuePrependEdges checks prepend PLAC on PLACs other than the
// fixture's: one continued with CONC, an empty one, two lines prepended to
// one PLAC, and a PLAC with a FORM or TRAN describing its old value.
func TestMoveValuePrependEdges(t *testing.T) {
	head := "0 HEAD\n1 GEDC\n2 VERS 5.5.1\n2 FORM LINEAGE-LINKED\n1 CHAR UTF-8\n0 @I1@ INDI\n1 BIRT\n"
	head70 := "0 HEAD\n1 GEDC\n2 VERS 7.0\n0 @I1@ INDI\n1 BIRT\n"
	tests := []struct {
		name, in, rows string
		code           int
		want, text     string // the file written, or "" for none; a line of stdout
	}{
		{"PLAC continued with CONC", head + "2 PLAC Springfield, Ohi\n3 CONC o\n2 ADDR Example Hospital\n0 TRLR\n",
			"INDI.BIRT.ADDR\tExample Hospital\tprepend PLAC\n", ExitOK,
			head + "2 PLAC Example Hospital, Springfield, Ohio\n0 TRLR\n", ": 1 line moved, 0 MAP removed, 1 record changed\n"},
		{"empty PLAC", head + "2 PLAC\n2 ADDR Example Hospital\n0 TRLR\n",
			"INDI.BIRT.ADDR\tExample Hospital\tprepend PLAC\n", ExitOK,
			head + "2 PLAC Example Hospital\n0 TRLR\n", ": 1 line moved, "},
		{"prepended to twice", head + "2 PLAC Springfield\n2 ADDR Example Hall\n2 ADDR Example Lodge\n0 TRLR\n",
			"INDI.BIRT.ADDR\tExample Hall\tprepend PLAC\nINDI.BIRT.ADDR\tExample Lodge\tprepend PLAC\n", ExitError,
			"", "  event's PLAC is moved, or prepended to twice\t@I1@\tline 10\tADDR\trow 3\n"},
		{"PLAC with a FORM", head + "2 PLAC Springfield, Ohio\n3 FORM City, State\n2 ADDR Example Hospital\n0 TRLR\n",
			"INDI.BIRT.ADDR\tExample Hospital\tprepend PLAC\n", ExitError,
			"", "  event's PLAC has FORM under it\t@I1@\tline 10\tADDR\trow 2\n"},
		{"PLAC with a TRAN", head70 + "2 PLAC Springfield\n3 TRAN Springfeld\n4 LANG de\n2 ADDR Example Hospital\n0 TRLR\n",
			"INDI.BIRT.ADDR\tExample Hospital\tprepend PLAC\n", ExitError,
			"", "  event's PLAC has TRAN under it\t@I1@\tline 9\tADDR\trow 2\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := writeTemp(t, "in.ged", tt.in)
			table := writeTemp(t, "moves.tsv", "path\tvalue\taction\n"+tt.rows)
			out := filepath.Join(t.TempDir(), "out.ged")
			code, stdout, _ := run(t, "move-value", in, table, "-o", out)
			if code != tt.code || !strings.Contains(stdout, tt.text) {
				t.Errorf("exit = %d, want %d; stdout lacks %q:\n%s", code, tt.code, tt.text, stdout)
			}
			got, err := os.ReadFile(out)
			if err != nil && !os.IsNotExist(err) {
				t.Fatal(err)
			}
			if string(got) != tt.want {
				t.Errorf("output:\n%s\nwant:\n%s", got, tt.want)
			}
		})
	}
}

// TestMoveValueFindings checks that an edit that adds a validation finding
// writes nothing.
func TestMoveValueFindings(t *testing.T) {
	in := writeTemp(t, "in.ged", "0 HEAD\n1 GEDC\n2 VERS 5.5.1\n2 FORM LINEAGE-LINKED\n1 CHAR UTF-8\n"+
		"0 @S1@ SOUR\n1 PUBL\n2 NOTE Example Press\n0 TRLR\n")
	table := writeTemp(t, "moves.tsv", "path\tvalue\taction\nSOUR.PUBL.NOTE\tExample Press\tto ADDR\n")
	out := filepath.Join(t.TempDir(), "out.ged")
	code, stdout, _ := run(t, "move-value", in, table, "-o", out)
	if code != ExitError || !strings.Contains(stdout, "not written: 1 line moved") || !strings.Contains(stdout, "UNEXPECTED_SUBSTRUCTURE") {
		t.Errorf("exit = %d, stdout = %q", code, stdout)
	}
	if _, err := os.Stat(out); !os.IsNotExist(err) {
		t.Errorf("a file was written: %v", err)
	}
}

func TestMoveValueJSON(t *testing.T) {
	rows := "INDI.BIRT.ADDR\tExample General Hospital\tprepend PLAC\t\n*.PLAC\tat home\tto NOTE\tPlace given as: \n*.ADDR\tNowhere\tto PLAC\t\n"
	code, _, stdout := runMoveValue(t, rows, "--format", "json")
	if code != ExitOK {
		t.Fatalf("exit = %d, stdout = %q", code, stdout)
	}
	var got moveValueReport
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("stdout is not JSON: %v\n%s", err, stdout)
	}
	if !got.Written || got.Moved != 4 || got.MapsRemoved != 1 || got.RecordsChanged != 3 || len(got.Rows) != 3 || len(got.Refused) != 0 {
		t.Errorf("report = %+v", got)
	}
	if r := got.Rows[1]; r.Line != 3 || r.Path != "*.PLAC" || r.Prefix != "Place given as: " || r.Matched != 3 || r.Moved != 3 || r.RecordsChanged != 3 {
		t.Errorf("row = %+v", r)
	}
	if len(got.Unmatched) != 1 || got.Unmatched[0] != 4 {
		t.Errorf("unmatched = %v, want [4]", got.Unmatched)
	}

	code, _, stdout = runMoveValue(t, "INDI.BURI.ADDR\tExample Chapel\tprepend PLAC\t\n", "--format", "json")
	if code != ExitError {
		t.Fatalf("exit = %d, stdout = %q", code, stdout)
	}
	got = moveValueReport{}
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("stdout is not JSON: %v\n%s", err, stdout)
	}
	want := moveRefusal{XRef: "@I1@", Line: 22, Tag: "ADDR", Row: 2, Reason: "missing", Target: "PLAC"}
	if got.Written || len(got.Refused) != 1 || got.Refused[0] != want {
		t.Errorf("refused = %+v, want %+v", got.Refused, want)
	}
}

// TestMoveValueStdout checks that with -o - the GEDCOM goes to stdout and the
// report to stderr.
func TestMoveValueStdout(t *testing.T) {
	table := writeTemp(t, "moves.tsv", "path\tvalue\taction\nINDI.BURI.ADDR\tExample Chapel\tto PLAC\n")
	code, stdout, stderr := run(t, "move-value", moveValueGED, table, "-o", "-")
	if code != ExitOK {
		t.Fatalf("exit = %d, stderr = %q", code, stderr)
	}
	if want := replaceOnce(t, readFixture(t, moveValueGED), "2 ADDR Example Chapel\n", "2 PLAC Example Chapel\n"); stdout != want {
		t.Errorf("stdout:\n%s\nwant:\n%s", stdout, want)
	}
	if !strings.HasPrefix(stderr, "wrote stdout: 1 line moved") {
		t.Errorf("stderr = %q", stderr)
	}
}

func TestMoveValueTableErrors(t *testing.T) {
	tests := []struct {
		name, table, want string
	}{
		{"no header", "INDI.BIRT.ADDR\tx\tto NOTE\n", `want a header row naming "path", "value" and "action" columns`},
		{"empty", "\n\n", "moves.tsv: no header row"},
		{"column twice", "path\tvalue\taction\tvalue\n", `column "value" is named twice`},
		{"no path", "path\tvalue\taction\n\tx\tto NOTE\n", "line 2: no path"},
		{"no value", "path\tvalue\taction\n*.PLAC\t\tto NOTE\n", "line 2: no value"},
		{"no action", "path\tvalue\taction\n*.PLAC\tx\t\n", "line 2: no action"},
		{"unknown action", "path\tvalue\taction\n*.PLAC\tx\tmove\n", `line 2: unknown action "move"`},
		{"prefix", "path\tvalue\taction\tprefix\n*.PLAC\tx\tto ADDR\tp\n", `line 2: a prefix is for "to NOTE" only`},
		{"path at an event", "path\tvalue\taction\nINDI.BIRT\tx\tto NOTE\n", `line 2: invalid path "INDI.BIRT"`},
		{"continuation path", "path\tvalue\taction\n*.CONT\tx\tto NOTE\n", `line 2: invalid path "*.CONT"`},
		{"deep star", "path\tvalue\taction\n*.BIRT.PLAC\tx\tto NOTE\n", `line 2: invalid path "*.BIRT.PLAC"`},
		{"same tag", "path\tvalue\taction\n*.PLAC\tx\tprepend PLAC\n", `line 2: "prepend PLAC" puts the value on the PLAC it is already on`},
		{"row twice", "path\tvalue\taction\n*.PLAC\tx\tto NOTE\n*.PLAC\tx\tto ADDR\n", `line 3: path *.PLAC and value "x" are already on line 2`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			table := writeTemp(t, "moves.tsv", tt.table)
			out := filepath.Join(t.TempDir(), "out.ged")
			code, stdout, stderr := run(t, "move-value", moveValueGED, table, "-o", out)
			if code != ExitError || stdout != "" || !strings.Contains(stderr, tt.want) {
				t.Errorf("exit = %d, stdout = %q, stderr = %q; want %q", code, stdout, stderr, tt.want)
			}
			if _, err := os.Stat(out); !os.IsNotExist(err) {
				t.Errorf("a file was written: %v", err)
			}
		})
	}
}

func TestMoveValueUsage(t *testing.T) {
	table := writeTemp(t, "moves.tsv", "path\tvalue\taction\n")
	for _, args := range [][]string{
		{"move-value", moveValueGED, table},
		{"move-value", moveValueGED, "-o", "-"},
		{"move-value", moveValueGED, table, "-o", table},
	} {
		if code, _, _ := run(t, args...); code != ExitUsage {
			t.Errorf("%v: exit = %d, want %d", args, code, ExitUsage)
		}
	}
}
