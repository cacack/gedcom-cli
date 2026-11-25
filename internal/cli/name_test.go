package cli

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// names is a person with three names: the first with citations, a note and a
// vendor tag, the second with every part and a TYPE, the third with no lines
// under it, and SEX between the second and third.
const (
	names  = "testdata/names.ged"
	names7 = "testdata/names-7.ged"
)

// The names fixture's NAME structures, as they are written.
const (
	adaName     = "1 NAME Ada /Example/\n2 GIVN Ada\n2 SURN Example\n2 SOUR @S1@\n3 PAGE folio 3\n2 NOTE Name as baptized\n2 _CUSTOM kept\n"
	adalineName = "1 NAME Adaline /Sample/ Jr\n2 TYPE aka\n2 NICK Addie\n2 GIVN Adaline\n2 SURN Sample\n2 NSFX Jr\n2 SOUR @S1@\n"
)

// runName runs name on in with args, writing to a new file, and returns the
// exit code, the file written ("" when none was) and stdout.
func runName(t *testing.T, in string, args ...string) (code int, written, stdout string) {
	t.Helper()
	out := filepath.Join(t.TempDir(), "out.ged")
	code, stdout, stderr := run(t, append(append([]string{"name", in}, args...), "-o", out)...)
	if (code == ExitOK) != (stderr == "") {
		t.Errorf("exit %d with stderr %q", code, stderr)
	}
	got, err := os.ReadFile(out)
	if errors.Is(err, fs.ErrNotExist) {
		return code, "", stdout
	}
	if err != nil {
		t.Fatal(err)
	}
	return code, string(got), stdout
}

func readFixture(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// replaceOnce is s with old replaced by new, failing when s lacks old.
func replaceOnce(t *testing.T, s, old, new string) string {
	t.Helper()
	if !strings.Contains(s, old) {
		t.Fatalf("fixture lacks %q", old)
	}
	return strings.Replace(s, old, new, 1)
}

func TestNameMove(t *testing.T) {
	input := readFixture(t, names)
	moved := replaceOnce(t, input, adaName+adalineName, adalineName+adaName)
	tests := []struct {
		name string
		args []string
		want string
	}{
		{"second to first", []string{"@I1@", "2", "--move-to", "1"}, moved},
		{"first to second", []string{"I1", "1", "--move-to", "2"}, moved},
		// The third name moves into the first slot; the others shift along
		// the slots, and SEX stays between the second and third.
		{"across a line", []string{"@I1@", "3", "--move-to", "1"},
			replaceOnce(t, input, adaName+adalineName+"1 SEX F\n1 NAME Ada Sample\n", "1 NAME Ada Sample\n"+adaName+"1 SEX F\n"+adalineName)},
		{"in place", []string{"@I1@", "2", "--move-to", "2"}, input},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code, got, stdout := runName(t, names, tt.args...)
			if code != ExitOK {
				t.Fatalf("exit = %d, stdout = %q", code, stdout)
			}
			if got != tt.want {
				t.Errorf("output:\n%s\nwant:\n%s", got, tt.want)
			}
		})
	}

	_, _, stdout := runName(t, names, "@I1@", "2", "--move-to", "1")
	want := "@I1@ name 2, now name 1\n  before\t1 NAME Adaline /Sample/ Jr | 2 TYPE aka | "
	if !strings.Contains(stdout, want) {
		t.Errorf("stdout lacks %q:\n%s", want, stdout)
	}
}

func TestNameType(t *testing.T) {
	input := readFixture(t, names)
	tests := []struct {
		name string
		args []string
		want string
	}{
		{"add", []string{"@I1@", "1", "--type", "birth"},
			replaceOnce(t, input, "1 NAME Ada /Example/\n", "1 NAME Ada /Example/\n2 TYPE birth\n")},
		{"replace", []string{"@I1@", "2", "--type", "married"},
			replaceOnce(t, input, "2 TYPE aka\n", "2 TYPE married\n")},
		{"clear", []string{"@I1@", "2", "--clear-type"},
			replaceOnce(t, input, "2 TYPE aka\n", "")},
		{"clear none", []string{"@I1@", "1", "--clear-type"}, input},
		// 5.5.1 takes any text as a name type.
		{"5.5.1 user-defined", []string{"@I1@", "3", "--type", "nickname"},
			replaceOnce(t, input, "1 NAME Ada Sample\n", "1 NAME Ada Sample\n2 TYPE nickname\n")},
		{"nick and type", []string{"@I1@", "3", "--nick", "Ady", "--type", "aka"},
			replaceOnce(t, input, "1 NAME Ada Sample\n", "1 NAME Ada Sample\n2 TYPE aka\n2 NICK Ady\n")},
		{"clear nick", []string{"@I1@", "2", "--clear-nick"},
			replaceOnce(t, input, "2 NICK Addie\n", "")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code, got, stdout := runName(t, names, tt.args...)
			if code != ExitOK {
				t.Fatalf("exit = %d, stdout = %q", code, stdout)
			}
			if got != tt.want {
				t.Errorf("output:\n%s\nwant:\n%s", got, tt.want)
			}
		})
	}
}

// TestNameTypeInvalidForVersion checks that a TYPE the file's version does not
// allow is refused, with the finding reported and nothing written.
func TestNameTypeInvalidForVersion(t *testing.T) {
	code, got, stdout := runName(t, names7, "@I1@", "2", "--type", "married")
	if code != ExitError || got != "" {
		t.Fatalf("exit = %d, output %q; want 1 and nothing written", code, got)
	}
	for _, want := range []string{"not written", "INVALID_ENUM_VALUE"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("stdout lacks %q:\n%s", want, stdout)
		}
	}

	code, got, _ = runName(t, names7, "@I1@", "1", "--type", "MARRIED")
	if code != ExitOK || !strings.Contains(got, "1 NAME Ada /Example/\n2 TYPE MARRIED\n") {
		t.Errorf("exit = %d, output:\n%s", code, got)
	}
}

func TestNameValue(t *testing.T) {
	input := readFixture(t, names)
	tests := []struct {
		name string
		args []string
		want string
	}{
		{"every part", []string{"@I1@", "2", "--value", "Adaline Kay /Sampler/ Sr"},
			replaceOnce(t, input, "1 NAME Adaline /Sample/ Jr\n2 TYPE aka\n2 NICK Addie\n2 GIVN Adaline\n2 SURN Sample\n2 NSFX Jr\n",
				"1 NAME Adaline Kay /Sampler/ Sr\n2 TYPE aka\n2 NICK Addie\n2 GIVN Adaline Kay\n2 SURN Sampler\n2 NSFX Sr\n")},
		// A word moved from the surname to the given name: the empty
		// surname and suffix are removed; TYPE, NICK and SOUR stay.
		{"empty parts removed", []string{"@I1@", "2", "--value", "Adaline Sample //"},
			replaceOnce(t, input, "1 NAME Adaline /Sample/ Jr\n2 TYPE aka\n2 NICK Addie\n2 GIVN Adaline\n2 SURN Sample\n2 NSFX Jr\n",
				"1 NAME Adaline Sample //\n2 TYPE aka\n2 NICK Addie\n2 GIVN Adaline Sample\n")},
		// Citations, notes and vendor tags stay; NSFX, which the name has
		// no line for, is not added.
		{"part not added", []string{"@I1@", "1", "--value", "Ada /Example/ III"},
			replaceOnce(t, input, "1 NAME Ada /Example/\n2 GIVN Ada\n", "1 NAME Ada /Example/ III\n2 GIVN Ada\n")},
		{"no part lines", []string{"@I1@", "3", "--value", "Ada /Sample/"},
			replaceOnce(t, input, "1 NAME Ada Sample\n", "1 NAME Ada /Sample/\n")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code, got, stdout := runName(t, names, tt.args...)
			if code != ExitOK {
				t.Fatalf("exit = %d, stdout = %q", code, stdout)
			}
			if got != tt.want {
				t.Errorf("output:\n%s\nwant:\n%s", got, tt.want)
			}
		})
	}
}

func TestNameJSON(t *testing.T) {
	out := filepath.Join(t.TempDir(), "out.ged")
	code, stdout, stderr := run(t, "name", "--format", "json", names, "@I1@", "2", "--move-to", "1", "--clear-type", "-o", out)
	if code != ExitOK {
		t.Fatalf("exit = %d, stderr = %q", code, stderr)
	}
	var got nameReport
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("stdout is not JSON: %v\n%s", err, stdout)
	}
	if !got.Written || got.XRef != "@I1@" || got.Name != 2 || got.Position != 1 ||
		!slices.Contains(got.Before, "2 TYPE aka") || slices.Contains(got.After, "2 TYPE aka") || len(got.Findings) != 0 {
		t.Errorf("report = %+v", got)
	}
}

func TestNameErrors(t *testing.T) {
	tests := []struct {
		name string
		in   string
		args []string
		code int
	}{
		{"no such name", names, []string{"@I1@", "4", "--type", "aka"}, ExitError},
		{"no such target", names, []string{"@I1@", "1", "--move-to", "4"}, ExitError},
		{"not an individual", names, []string{"@F1@", "1", "--type", "aka"}, ExitError},
		{"no such record", names, []string{"@I9@", "1", "--type", "aka"}, ExitError},
		{"position not a number", names, []string{"@I1@", "first", "--type", "aka"}, ExitUsage},
		{"position zero", names, []string{"@I1@", "0", "--type", "aka"}, ExitUsage},
		{"move-to zero", names, []string{"@I1@", "1", "--move-to", "0"}, ExitUsage},
		{"no operation", names, []string{"@I1@", "1"}, ExitUsage},
		{"set and clear", names, []string{"@I1@", "1", "--type", "aka", "--clear-type"}, ExitUsage},
		{"empty value", names, []string{"@I1@", "1", "--value", " "}, ExitUsage},
		{"line break", names, []string{"@I1@", "1", "--nick", "A\nB"}, ExitUsage},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code, got, stdout := runName(t, tt.in, tt.args...)
			if code != tt.code {
				t.Errorf("exit = %d, want %d", code, tt.code)
			}
			if got != "" || stdout != "" {
				t.Errorf("wrote %q, stdout %q; want nothing", got, stdout)
			}
		})
	}
}

// TestNameInputUntouched checks that the input file is never written, and
// that -o may not name it.
func TestNameInputUntouched(t *testing.T) {
	in := writeTemp(t, "in.ged", readFixture(t, names))
	before := readFixture(t, in)
	code, got, _ := runName(t, in, "@I1@", "2", "--move-to", "1", "--value", "Adaline //", "--type", "married")
	if code != ExitOK || got == before {
		t.Fatalf("exit = %d; want a changed output", code)
	}
	if code, _, _ := run(t, "name", in, "@I1@", "1", "--type", "aka", "-o", in); code != ExitUsage {
		t.Errorf("-o naming the input: exit = %d, want %d", code, ExitUsage)
	}
	if after := readFixture(t, in); after != before {
		t.Errorf("input changed:\n%s", after)
	}
}

func TestNameHelp(t *testing.T) {
	_, stdout, _ := run(t, "name", "--help")
	for _, want := range []string{"counted from 1, in file", "--move-to", "--type", "--clear-type", "--value", "--nick", "--clear-nick", "grammar check"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("help lacks %q", want)
		}
	}
}

// TestNameValidates checks that, on a 5.5.1 and a 7.0 file, the output of
// every operation validates with no finding the input did not have.
func TestNameValidates(t *testing.T) {
	for _, in := range []string{names, names7} {
		for _, args := range [][]string{
			{"@I1@", "2", "--move-to", "1"},
			{"@I1@", "1", "--type", "BIRTH"},
			{"@I1@", "2", "--clear-type"},
			{"@I1@", "2", "--value", "Adaline Sample //"},
			{"@I1@", "1", "--value", "/Example/", "--nick", "Ady"},
			{"@I1@", "2", "--clear-nick"},
		} {
			t.Run(filepath.Base(in)+" "+strings.Join(args[2:], " "), func(t *testing.T) {
				out := filepath.Join(t.TempDir(), "out.ged")
				if code, _, stderr := run(t, append(append([]string{"name", in}, args...), "-o", out)...); code != ExitOK {
					t.Fatalf("exit = %d, stderr = %q", code, stderr)
				}
				had := map[string]bool{}
				for _, f := range validateFindings(t, in) {
					had[f.Code+"\t"+f.XRef+"\t"+f.Message] = true
				}
				for _, f := range validateFindings(t, out) {
					if !had[f.Code+"\t"+f.XRef+"\t"+f.Message] {
						t.Errorf("output has a new finding: %+v", f)
					}
				}
			})
		}
	}
}

// validateFindings is validate's findings for path, at every severity.
func validateFindings(t *testing.T, path string) []finding {
	t.Helper()
	_, stdout, stderr := run(t, "validate", "--format", "json", "--min-severity", "info", path)
	var report validateReport
	if err := json.Unmarshal([]byte(stdout), &report); err != nil {
		t.Fatalf("validate %s: %v, stderr = %q", path, err, stderr)
	}
	return report.Findings
}
