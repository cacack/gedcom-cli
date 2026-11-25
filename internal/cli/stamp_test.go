package cli

import (
	"encoding/json"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// stampLines are the header lines a stamp writes in place of the input's SOUR.
func stampLines() string {
	return "1 SOUR gedcom-cli\n2 VERS " + version() + "\n2 NAME gedcom-cli\n"
}

// stampBody is the records of the stamp fixtures: a submitter and two people
// merge-people can fold together.
const stampBody = `0 @U1@ SUBM
1 NAME Test Submitter
0 @I1@ INDI
1 NAME Ada /Example/
1 SEX F
0 @I2@ INDI
1 NAME Ada /Example/
1 SEX F
0 TRLR
`

// stampFixtures are a 5.5.1 and a 7.0 file whose header names another
// program, with its version, name and corporation, and the lines that
// describe its export (DEST, DATE, FILE), beside lines a stamp keeps. want is
// each header stamped.
var stampFixtures = []struct {
	name, header, want string
}{
	{
		name: "5.5.1",
		header: `0 HEAD
1 SOUR ExampleSoft
2 VERS 9.1
2 NAME Example Soft
2 CORP Example Corp
3 ADDR 1 Example Road
1 DEST ExampleDest
1 DATE 1 JAN 2020
2 TIME 12:00:00
1 SUBM @U1@
1 FILE export.ged
1 COPR Copyright Example
1 GEDC
2 VERS 5.5.1
2 FORM LINEAGE-LINKED
1 CHAR UTF-8
1 NOTE A header note
`,
		want: "0 HEAD\n" + stampLines() + `1 SUBM @U1@
1 COPR Copyright Example
1 GEDC
2 VERS 5.5.1
2 FORM LINEAGE-LINKED
1 CHAR UTF-8
1 NOTE A header note
`,
	},
	{
		name: "7.0",
		header: `0 HEAD
1 GEDC
2 VERS 7.0
1 SOUR ExampleSoft
2 VERS 9.1
2 NAME Example Soft
2 CORP Example Corp
1 DEST ExampleDest
1 DATE 1 JAN 2020
2 TIME 12:00:00
1 SUBM @U1@
1 COPR Copyright Example
1 NOTE A header note
`,
		want: "0 HEAD\n1 GEDC\n2 VERS 7.0\n" + stampLines() + `1 SUBM @U1@
1 COPR Copyright Example
1 NOTE A header note
`,
	},
}

// withFlag is args, a command and its arguments, with flag after the command.
func withFlag(args []string, flag string) []string {
	return slices.Insert(slices.Clone(args), 1, flag)
}

// headerText is the header of the GEDCOM text: its lines up to the first
// record.
func headerText(text string) string {
	return text[:strings.Index(text, "\n0 ")+1]
}

// findingKeys is each finding as its severity, code and xref, which do not
// depend on line numbers.
func findingKeys(findings []finding) []string {
	keys := make([]string, len(findings))
	for i, f := range findings {
		keys[i] = f.Severity + " " + f.Code + " " + f.XRef
	}
	return keys
}

// TestStampNewDatasets holds that combine and extract stamp the header of the
// file they write by default, with the version 'gedcom --version' prints, so
// that validate finds nothing new, and that --keep-header writes it as read.
func TestStampNewDatasets(t *testing.T) {
	_, versionOut, _ := run(t, "--version")
	if want := "gedcom version " + version() + "\n"; versionOut != want {
		t.Fatalf("--version = %q, want %q", versionOut, want)
	}
	commands := map[string]func(in, out string) []string{
		"extract": func(in, out string) []string { return []string{"extract", in, "I1", "I2", "-o", out} },
		"combine": func(in, out string) []string { return []string{"combine", "--strategy", "zip", in, "-o", out} },
	}
	for _, fx := range stampFixtures {
		in := writeTemp(t, "in.ged", fx.header+stampBody)
		inFindings := findingKeys(validateFindings(t, in))
		for name, args := range commands {
			t.Run(fx.name+"/"+name, func(t *testing.T) {
				out := filepath.Join(t.TempDir(), "out.ged")
				code, stdout, stderr := run(t, append([]string{"--format", "json"}, args(in, out)...)...)
				if code != ExitOK {
					t.Fatalf("exit = %d, stderr = %q", code, stderr)
				}
				if !strings.Contains(stdout, `"stamped": true`) {
					t.Errorf("report does not say the header was stamped:\n%s", stdout)
				}
				if got := headerText(fileText(t, out)); got != fx.want {
					t.Errorf("header =\n%s\nwant\n%s", got, fx.want)
				}
				for _, f := range findingKeys(validateFindings(t, out)) {
					if !slices.Contains(inFindings, f) {
						t.Errorf("validate finds %q in the stamped file, not in the input", f)
					}
				}

				code, stdout, stderr = run(t, append([]string{"--format", "json"}, withFlag(args(in, out), "--keep-header")...)...)
				if code != ExitOK {
					t.Fatalf("--keep-header: exit = %d, stderr = %q", code, stderr)
				}
				if !strings.Contains(stdout, `"stamped": false`) {
					t.Errorf("--keep-header: report says the header was stamped:\n%s", stdout)
				}
				if got := headerText(fileText(t, out)); got != fx.header {
					t.Errorf("--keep-header: header =\n%s\nwant it as read:\n%s", got, fx.header)
				}
			})
		}
	}
}

// TestStampEditingCommands holds that put and merge-people write the header
// as read, and with --stamp differ from that output only in the header lines
// the stamp replaces, saying so in the report.
func TestStampEditingCommands(t *testing.T) {
	empty := writeTemp(t, "empty.ged", "")
	tests := []struct {
		name string
		args func(in, out string) []string
		text string // the report's line that says the header was stamped
	}{
		{"put", func(in, out string) []string { return []string{"put", in, empty, "-o", out} }, "0 unchanged, header stamped\n"},
		{"merge-people", func(in, out string) []string { return []string{"merge-people", in, "@I1@", "@I2@", "-o", out} }, "\nstamped\tHEAD\n"},
	}
	for _, fx := range stampFixtures {
		input := fx.header + stampBody
		in := writeTemp(t, "in.ged", input)
		for _, tt := range tests {
			t.Run(fx.name+"/"+tt.name, func(t *testing.T) {
				plain := filepath.Join(t.TempDir(), "plain.ged")
				code, stdout, stderr := run(t, tt.args(in, plain)...)
				if code != ExitOK {
					t.Fatalf("exit = %d, stderr = %q", code, stderr)
				}
				if strings.Contains(stdout, "stamped\t") || strings.Contains(stdout, "header stamped") {
					t.Errorf("report says the header was stamped:\n%s", stdout)
				}
				if got := headerText(fileText(t, plain)); got != fx.header {
					t.Errorf("header =\n%s\nwant it as read:\n%s", got, fx.header)
				}

				stamped := filepath.Join(t.TempDir(), "stamped.ged")
				code, stdout, stderr = run(t, withFlag(tt.args(in, stamped), "--stamp")...)
				if code != ExitOK {
					t.Fatalf("--stamp: exit = %d, stderr = %q", code, stderr)
				}
				if !strings.HasSuffix(stdout, tt.text) {
					t.Errorf("--stamp: report = %q, want it to end %q", stdout, tt.text)
				}
				got, want := fileText(t, stamped), fileText(t, plain)
				if headerText(got) != fx.want {
					t.Errorf("--stamp: header =\n%s\nwant\n%s", headerText(got), fx.want)
				}
				if got[len(headerText(got)):] != want[len(headerText(want)):] {
					t.Errorf("--stamp: the records differ from those written without it:\n%s\nwant\n%s", got, want)
				}

				_, stdout, _ = run(t, append([]string{"--format", "json"}, withFlag(tt.args(in, stamped), "--stamp")...)...)
				if !strings.Contains(stdout, `"stamped": true`) {
					t.Errorf("--stamp: JSON report does not say the header was stamped:\n%s", stdout)
				}
			})
		}
	}
}

// TestStampMergeSeveral holds that with several pairs every pair's report
// says the file they share was stamped.
func TestStampMergeSeveral(t *testing.T) {
	in := writeTemp(t, "in.ged", stampFixtures[0].header+strings.Replace(stampBody, "0 TRLR\n", "0 @I3@ INDI\n1 NAME Ada /Example/\n0 TRLR\n", 1))
	out := filepath.Join(t.TempDir(), "out.ged")
	code, stdout, stderr := run(t, "merge-people", "--stamp", "--format", "json", in, "@I1@", "@I2@", "@I3@", "-o", out)
	if code != ExitOK {
		t.Fatalf("exit = %d, stderr = %q", code, stderr)
	}
	var reports []mergeReport
	if err := json.Unmarshal([]byte(stdout), &reports); err != nil || len(reports) != 2 {
		t.Fatalf("%v, %d reports:\n%s", err, len(reports), stdout)
	}
	for _, r := range reports {
		if !r.Stamped {
			t.Errorf("pair %s %s: stamped = false", r.Keep, r.Drop)
		}
	}
}

// TestStampHelp holds that each command that writes a header says what it
// does to it.
func TestStampHelp(t *testing.T) {
	for _, tt := range []struct {
		cmd  string
		says []string
	}{
		{"combine", []string{"--keep-header", "header is then stamped", "A stamped header names this tool"}},
		{"extract", []string{"--keep-header", "The header is the input's, stamped", "A stamped header names this tool"}},
		{"put", []string{"--stamp", "The header is written as read", "A stamped header names this tool"}},
		{"merge-people", []string{"--stamp", "The header is written as read", "A stamped header names this tool"}},
		{"merge-families", []string{"--stamp", "The header is written as read"}},
		{"merge-sources", []string{"--stamp", "The header is written as read"}},
		{"merge-repositories", []string{"--stamp", "The header is written as read"}},
	} {
		_, stdout, _ := run(t, tt.cmd, "--help")
		for _, want := range tt.says {
			if !strings.Contains(stdout, want) {
				t.Errorf("%s --help lacks %q", tt.cmd, want)
			}
		}
	}
}
