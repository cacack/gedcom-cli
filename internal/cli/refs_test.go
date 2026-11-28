package cli

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

// refsFixture has a media record pointed at from a person, a family and a
// citation inside an event, a submitter the header points at, and a family
// nothing points at.
const refsFixture = "0 HEAD\n1 GEDC\n2 VERS 5.5.1\n1 CHAR UTF-8\n1 SUBM @U1@\n" +
	"0 @U1@ SUBM\n1 NAME Test Submitter\n" +
	"0 @I1@ INDI\n1 NAME Ada /Example/\n1 BIRT\n2 SOUR @S1@\n3 OBJE @O1@\n1 OBJE @O1@\n" +
	"0 @F1@ FAM\n1 WIFE @I1@\n1 OBJE @O1@\n" +
	"0 @S1@ SOUR\n1 TITL Example register\n" +
	"0 @O1@ OBJE\n1 FILE photos/ada.jpg\n" +
	"0 TRLR\n"

func TestRefsText(t *testing.T) {
	path := writeTemp(t, "refs.ged", refsFixture)
	tests := []struct {
		name string
		args []string
		want string
	}{
		{"one xref", []string{"@O1@"}, "@I1@\tINDI.BIRT.SOUR.OBJE\t@O1@\n@I1@\tINDI.OBJE\t@O1@\n@F1@\tFAM.OBJE\t@O1@\n"},
		{"header", []string{"U1"}, "-\tHEAD.SUBM\t@U1@\n"},
		// In the order given, each xref once.
		{"several", []string{"I1", "S1", "I1"}, "@F1@\tFAM.WIFE\t@I1@\n@I1@\tINDI.BIRT.SOUR\t@S1@\n"},
		{"nothing points at it", []string{"@F1@"}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code, stdout, stderr := run(t, append([]string{"refs", path}, tt.args...)...)
			if code != ExitOK || stderr != "" {
				t.Fatalf("exit = %d, stderr = %q", code, stderr)
			}
			if stdout != tt.want {
				t.Errorf("stdout = %q, want %q", stdout, tt.want)
			}
		})
	}
}

func TestRefsJSON(t *testing.T) {
	path := writeTemp(t, "refs.ged", refsFixture)
	code, stdout, stderr := run(t, "refs", "--format", "json", path, "U1", "S1")
	if code != ExitOK || stderr != "" {
		t.Fatalf("exit = %d, stderr = %q", code, stderr)
	}
	var got []map[string]string
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("stdout is not JSON: %v\n%s", err, stdout)
	}
	want := []map[string]string{
		{"from": "", "path": "HEAD.SUBM", "to": "@U1@"},
		{"from": "@I1@", "path": "INDI.BIRT.SOUR", "to": "@S1@"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}

	code, stdout, _ = run(t, "refs", "--format", "json", path, "F1")
	if code != ExitOK || stdout != "[]\n" {
		t.Errorf("nothing points at it: exit = %d, stdout = %q, want []", code, stdout)
	}
}

// TestRefsStdin checks "-": xrefs read from stdin are answered as the same
// xrefs given as arguments, where "-" stands.
func TestRefsStdin(t *testing.T) {
	path := writeTemp(t, "refs.ged", refsFixture)
	_, want, _ := run(t, "refs", path, "U1", "S1", "I1")
	code, stdout, stderr := runStdin(t, "S1\n@I1@\n", "refs", path, "U1", "-")
	if code != ExitOK || stderr != "" {
		t.Fatalf("exit = %d, stderr = %q", code, stderr)
	}
	if stdout != want {
		t.Errorf("stdout = %q, want %q", stdout, want)
	}
}

func TestRefsExitCodes(t *testing.T) {
	path := writeTemp(t, "refs.ged", refsFixture)
	dup := writeTemp(t, "dup.ged", "0 HEAD\n1 GEDC\n2 VERS 5.5.1\n1 CHAR UTF-8\n"+
		"0 @I1@ INDI\n1 NAME Ada /Example/\n0 @I1@ INDI\n1 NAME Ben /Example/\n0 @F1@ FAM\n1 WIFE @I1@\n0 TRLR\n")
	tests := []struct {
		name, stdin, wantErr string
		args                 []string
		want                 int
	}{
		{"no such record", "", "no such record: @O9@", []string{"refs", path, "O1", "O9"}, ExitError},
		{"defined twice", "", "more than once", []string{"refs", dup, "I1"}, ExitError},
		{"missing file", "", "", []string{"refs", "testdata/does-not-exist.ged", "O1"}, ExitError},
		{"no xref", "", "", []string{"refs", path}, ExitUsage},
		{"empty stdin", "", "no xrefs on stdin", []string{"refs", path, "-"}, ExitError},
		{"stdin twice", "O1", `"-" may be given only once`, []string{"refs", path, "-", "-"}, ExitUsage},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code, stdout, stderr := runStdin(t, tt.stdin, tt.args...)
			if code != tt.want {
				t.Errorf("exit = %d, want %d (stderr %q)", code, tt.want, stderr)
			}
			if stdout != "" || stderr == "" || !strings.Contains(stderr, tt.wantErr) {
				t.Errorf("stdout = %q, stderr = %q; want only the error, containing %q", stdout, stderr, tt.wantErr)
			}
		})
	}
}
