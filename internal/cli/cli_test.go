package cli

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

const sample = "testdata/minimal.ged"

func run(t *testing.T, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	var out, errOut bytes.Buffer
	code = Run(args, &out, &errOut)
	return code, out.String(), errOut.String()
}

func TestInfoText(t *testing.T) {
	code, stdout, stderr := run(t, "info", sample)
	if code != ExitOK {
		t.Fatalf("exit = %d, stderr = %q", code, stderr)
	}
	for _, want := range []string{"Version:       5.5.1", "Source system: gedcom-cli-test", "Records:       4", "INDI  2", "FAM   1"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("stdout missing %q:\n%s", want, stdout)
		}
	}
}

func TestInfoJSON(t *testing.T) {
	code, stdout, stderr := run(t, "info", "--format", "json", sample)
	if code != ExitOK {
		t.Fatalf("exit = %d, stderr = %q", code, stderr)
	}
	var got fileInfo
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("stdout is not JSON: %v\n%s", err, stdout)
	}
	if got.Version != "5.5.1" || got.Records != 4 || got.RecordTypes["INDI"] != 2 {
		t.Errorf("unexpected info: %+v", got)
	}
}

func TestVersionStampedByRelease(t *testing.T) {
	buildVersion = "v9.9.9"
	t.Cleanup(func() { buildVersion = "" })

	code, stdout, stderr := run(t, "--version")
	if code != ExitOK || stderr != "" {
		t.Fatalf("exit = %d, stderr = %q", code, stderr)
	}
	if !strings.Contains(stdout, "v9.9.9") {
		t.Errorf("stdout = %q, want the stamped version", stdout)
	}
}

func TestExitCodes(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want int
	}{
		{"help", []string{"--help"}, ExitOK},
		{"no args prints help", nil, ExitOK},
		{"version", []string{"--version"}, ExitOK},
		{"missing file", []string{"info", "testdata/does-not-exist.ged"}, ExitError},
		{"missing argument", []string{"info"}, ExitUsage},
		{"unknown flag", []string{"info", "--nope", sample}, ExitUsage},
		{"bad format", []string{"info", "--format", "xml", sample}, ExitUsage},
		{"unknown command", []string{"nope"}, ExitUsage},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code, _, stderr := run(t, tt.args...)
			if code != tt.want {
				t.Errorf("exit = %d, want %d (stderr %q)", code, tt.want, stderr)
			}
			if (code == ExitOK) != (stderr == "") {
				t.Errorf("exit %d with stderr %q: stderr must be set exactly on failure", code, stderr)
			}
		})
	}
}

// TestCommandsDocumented is the check for CONSTITUTION.md criterion C3.
func TestCommandsDocumented(t *testing.T) {
	for _, cmd := range newRootCmd().Commands() {
		if cmd.Short == "" {
			t.Errorf("command %q has no summary", cmd.Name())
		}
		if cmd.Example == "" {
			t.Errorf("command %q has no example in its help", cmd.Name())
		}
	}
}
