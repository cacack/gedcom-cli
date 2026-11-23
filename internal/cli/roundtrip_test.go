package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"regexp"
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
// command here. It is empty until the first write command lands, so until then
// the test holds only the write path itself.
var writeCommands = map[string]func(in, out string) []string{}

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
					code, _, stderr := run(t, args(path, out)...)
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
