package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// roundTripFixtures maps each fixture to the layout it exists to cover. The
// pattern guards the fixture: an editor or a git setting that normalizes it
// would otherwise leave the test passing on a file that no longer has the
// layout. .gitattributes and .editorconfig keep these files as written.
var roundTripFixtures = map[string]*regexp.Regexp{
	"bom.ged":                 regexp.MustCompile(`^\x{FEFF}0 HEAD`),
	"crlf.ged":                regexp.MustCompile(`(?s)^[^\n]*\r\n.*\r\n$`),
	"mixed-line-endings.ged":  regexp.MustCompile(`(?s)\r\n.*[^\r]\n`),
	"indented.ged":            regexp.MustCompile(`(?m)^ +1 NAME`),
	"blank-lines.ged":         regexp.MustCompile(`\n\n0 `),
	"valueless-delimiter.ged": regexp.MustCompile(`(?m)^2 CONT \n`),
	"whitespace-value.ged":    regexp.MustCompile(`(?m)^2 CONT {3}\n`),
}

// writeCommands holds every command that writes a GEDCOM file, as the
// arguments that run it on in, writing out, with no change requested. Each
// one must come back byte-identical (CONSTITUTION.md C2); add a new write
// command here. Every fixture is GEDCOM 5.5.1 in UTF-8. A command that always
// changes the file goes in changingWriteCommands instead;
// TestWriteCommandsListed fails for one in neither.
var writeCommands = map[string]func(t *testing.T, in, out string) []string{
	// Combining one file joins nothing to it; --keep-header leaves its
	// header unstamped.
	"combine": func(_ *testing.T, in, out string) []string {
		return []string{"combine", "--strategy", "zip", "--keep-header", in, "-o", out}
	},
	"convert": func(_ *testing.T, in, out string) []string {
		return []string{"convert", in, "--to", "5.5.1", "-o", out}
	},
	// An empty batch requests no change.
	"media add": func(t *testing.T, in, out string) []string {
		batch := filepath.Join(filepath.Dir(out), "empty.tsv")
		if err := os.WriteFile(batch, nil, 0o644); err != nil {
			t.Fatal(err)
		}
		return []string{"media", "add", in, "--batch", batch, "-o", out}
	},
	"extract": extractEveryone,
	// Moving a name to where it is changes nothing.
	"name": func(_ *testing.T, in, out string) []string {
		return []string{"name", in, "@I1@", "1", "--move-to", "1", "-o", out}
	},
	// A place table with no rows requests no change.
	"places apply": func(t *testing.T, in, out string) []string {
		table := filepath.Join(filepath.Dir(out), "places.tsv")
		if err := os.WriteFile(table, []byte("place\tstandard\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		return []string{"places", "apply", in, table, "-o", out}
	},
	// A path no line is at requests no change.
	"retag": func(_ *testing.T, in, out string) []string {
		return []string{"retag", in, "INDI._NONE", "EVEN", "-o", out}
	},
	// A file with no repeated events has nothing to fold.
	"repeated-events": func(_ *testing.T, in, out string) []string {
		return []string{"repeated-events", "--fold", in, "-o", out}
	},
	// A file with no repeated names has nothing to fold.
	"repeated-names": func(_ *testing.T, in, out string) []string {
		return []string{"repeated-names", "--fold", in, "-o", out}
	},
	// put with an unedited copy of every record, as get prints them.
	"put": func(t *testing.T, in, out string) []string {
		fragment := filepath.Join(t.TempDir(), "records.ged")
		if err := os.WriteFile(fragment, []byte(getAll(t, in)), 0o600); err != nil {
			t.Fatal(err)
		}
		return []string{"put", in, fragment, "-o", out}
	},
	// put-header with an unedited copy of the header, as get prints it.
	"put-header": func(t *testing.T, in, out string) []string {
		header := filepath.Join(t.TempDir(), "head.ged")
		if err := os.WriteFile(header, []byte(getRecord(t, in, "HEAD")), 0o600); err != nil {
			t.Fatal(err)
		}
		return []string{"put-header", in, header, "-o", out}
	},
}

// changingWriteCommands maps each write command that cannot run with no change
// requested to the test that holds it to C2 instead: only the lines of the
// records it changes may differ.
var changingWriteCommands = map[string]string{
	"delete":             "TestDeleteRemovesOnlyTheRecord",
	"merge-people":       "TestMergeKeepsUninvolvedRecords",
	"merge-families":     "TestMergeKeepsUninvolvedRecords",
	"merge-sources":      "TestMergeSourcesKeepsUninvolvedRecords",
	"merge-repositories": "TestMergeSourcesKeepsUninvolvedRecords",
}

// TestWriteCommandsListed checks that the commands with an --output flag are
// exactly those in writeCommands and changingWriteCommands.
func TestWriteCommandsListed(t *testing.T) {
	writes := map[string]bool{}
	for _, cmd := range allCommands(newRootCmd()) {
		if cmd.Flags().Lookup("output") == nil {
			continue
		}
		name := strings.TrimPrefix(cmd.CommandPath(), "gedcom ")
		writes[name] = true
		_, roundTrips := writeCommands[name]
		_, changes := changingWriteCommands[name]
		if roundTrips == changes {
			t.Errorf("command %q has an --output flag: list it in writeCommands, or in changingWriteCommands if it always changes the file", name)
		}
	}
	for name := range writeCommands {
		if !writes[name] {
			t.Errorf("writeCommands lists %q, which is not a command with an --output flag", name)
		}
	}
	for name := range changingWriteCommands {
		if !writes[name] {
			t.Errorf("changingWriteCommands lists %q, which is not a command with an --output flag", name)
		}
	}
}

// getAll is the get output for every record of the file at path.
func getAll(t *testing.T, path string) string {
	t.Helper()
	doc, err := decodeFile(path)
	if err != nil {
		t.Fatal(err)
	}
	args := []string{"get", path}
	for _, record := range doc.Records {
		args = append(args, record.XRef)
	}
	code, stdout, stderr := run(t, args...)
	if code != ExitOK {
		t.Fatalf("get: exit = %d, stderr = %q", code, stderr)
	}
	return stdout
}

// TestRoundTripByteIdentical settles CONSTITUTION.md C2: decoding and
// re-encoding a file through the write path, or through any write command with
// no change requested, gives back the input byte for byte.
func TestRoundTripByteIdentical(t *testing.T) {
	paths, err := filepath.Glob("testdata/roundtrip/*.ged")
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) != len(roundTripFixtures) {
		t.Fatalf("found %d fixtures, roundTripFixtures lists %d", len(paths), len(roundTripFixtures))
	}
	for _, path := range paths {
		name := filepath.Base(path)
		t.Run(name, func(t *testing.T) {
			input, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			layout, ok := roundTripFixtures[name]
			if !ok {
				t.Fatalf("%s is not listed in roundTripFixtures", name)
			}
			if !layout.Match(input) {
				t.Fatalf("%s no longer has the layout it covers (%s)", name, layout)
			}

			t.Run("write path", func(t *testing.T) {
				res, err := decodeForWrite(path)
				if err != nil {
					t.Fatal(err)
				}
				var out bytes.Buffer
				if err := encodeDocument(&out, res.Document); err != nil {
					t.Fatal(err)
				}
				assertSameBytes(t, out.Bytes(), input)
			})

			for cmd, args := range writeCommands {
				t.Run(cmd, func(t *testing.T) {
					out := filepath.Join(t.TempDir(), name)
					code, _, stderr := run(t, args(t, path, out)...)
					if code != ExitOK {
						t.Fatalf("exit = %d, stderr = %q", code, stderr)
					}
					got, err := os.ReadFile(out)
					if err != nil {
						t.Fatal(err)
					}
					assertSameBytes(t, got, input)
				})
			}
		})
	}
}

// assertSameBytes fails at the first byte where got and want differ, quoting
// the line around it so whitespace and terminators are visible.
func assertSameBytes(t *testing.T, got, want []byte) {
	t.Helper()
	if bytes.Equal(got, want) {
		return
	}
	i := 0
	for i < len(got) && i < len(want) && got[i] == want[i] {
		i++
	}
	start := bytes.LastIndexByte(want[:i], '\n') + 1
	t.Errorf("output differs from input at byte %d (input %d bytes, output %d)\n got: %q\nwant: %q",
		i, len(want), len(got), lineAt(got, start), lineAt(want, start))
}

// lineAt returns b's line beginning at start, terminator included.
func lineAt(b []byte, start int) []byte {
	if start >= len(b) {
		return nil
	}
	if end := bytes.IndexByte(b[start:], '\n'); end >= 0 {
		return b[start : start+end+1]
	}
	return b[start:]
}
