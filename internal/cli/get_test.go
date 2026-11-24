package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const records = "testdata/records.ged"

const (
	recordI1 = `0 @I1@ INDI
1 NAME Cora /Sample/
1 SEX F
1 BIRT
2 DATE 3 MAR 1901
2 PLAC Springfield, Example County
1 DEAT
2 DATE 14 NOV 1978
1 FAMS @F1@
`
	recordF1 = `0 @F1@ FAM
1 HUSB @I2@
1 WIFE @I1@
1 CHIL @I3@
1 MARR
2 DATE 12 JUN 1923
`
)

func TestGetText(t *testing.T) {
	tests := []struct {
		name  string
		xrefs []string
		want  string
	}{
		{"one record", []string{"@I1@"}, recordI1},
		{"argument order", []string{"@F1@", "@I1@"}, recordF1 + recordI1},
		{"bare xref", []string{"I1", "@F1@"}, recordI1 + recordF1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code, stdout, stderr := run(t, append([]string{"get", records}, tt.xrefs...)...)
			if code != ExitOK || stderr != "" {
				t.Fatalf("exit = %d, stderr = %q", code, stderr)
			}
			if stdout != tt.want {
				t.Errorf("stdout =\n%s\nwant\n%s", stdout, tt.want)
			}
		})
	}
}

// TestGetTextMatchesFixture checks that every record comes back as the lines
// it has in the file, with no header or trailer.
func TestGetTextMatchesFixture(t *testing.T) {
	data, err := os.ReadFile(records)
	if err != nil {
		t.Fatal(err)
	}
	for _, xref := range []string{"@U1@", "@I1@", "@I2@", "@I3@", "@F1@", "@S1@", "@R1@", "@O1@"} {
		start := strings.Index(string(data), "0 "+xref+" ")
		if start < 0 {
			t.Fatalf("fixture has no record %s", xref)
		}
		want := string(data)[start:]
		want = want[:1+strings.Index(want[1:], "\n0 ")+1]

		code, stdout, stderr := run(t, "get", records, xref)
		if code != ExitOK || stderr != "" {
			t.Fatalf("%s: exit = %d, stderr = %q", xref, code, stderr)
		}
		if stdout != want {
			t.Errorf("%s: stdout =\n%s\nwant\n%s", xref, stdout, want)
		}
	}
}

func TestGetJSON(t *testing.T) {
	tests := []struct {
		name  string
		xrefs []string
		want  []string // the XRef of each element, in order
	}{
		{"one record is still an array", []string{"@I1@"}, []string{"@I1@"}},
		{"argument order", []string{"F1", "@I2@", "@S1@"}, []string{"@F1@", "@I2@", "@S1@"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args := append([]string{"get", "--format", "json", records}, tt.xrefs...)
			code, stdout, stderr := run(t, args...)
			if code != ExitOK || stderr != "" {
				t.Fatalf("exit = %d, stderr = %q", code, stderr)
			}
			var got []map[string]any
			if err := json.Unmarshal([]byte(stdout), &got); err != nil {
				t.Fatalf("stdout is not a JSON array: %v\n%s", err, stdout)
			}
			if len(got) != len(tt.want) {
				t.Fatalf("got %d elements, want %d:\n%s", len(got), len(tt.want), stdout)
			}
			for i, xref := range tt.want {
				if got[i]["XRef"] != xref {
					t.Errorf("element %d XRef = %v, want %s", i, got[i]["XRef"], xref)
				}
			}
		})
	}
}

// TestGetJSONTypedEntity checks that the elements are the library's typed
// entities (Names, Husband) rather than raw records (Entity), each with its
// record Type added first.
func TestGetJSONTypedEntity(t *testing.T) {
	code, stdout, stderr := run(t, "get", "--format", "json", records, "@I1@", "@F1@")
	if code != ExitOK {
		t.Fatalf("exit = %d, stderr = %q", code, stderr)
	}
	var got []map[string]json.RawMessage
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("stdout is not a JSON array: %v\n%s", err, stdout)
	}
	if len(got) != 2 {
		t.Fatalf("got %d elements, want 2", len(got))
	}
	var names []struct{ Given, Surname string }
	if err := json.Unmarshal(got[0]["Names"], &names); err != nil || len(names) != 1 {
		t.Fatalf("individual has no Names: %v\n%s", err, stdout)
	}
	if names[0].Given != "Cora" || names[0].Surname != "Sample" {
		t.Errorf("name = %+v", names[0])
	}
	if string(got[1]["Husband"]) != `"@I2@"` {
		t.Errorf("family Husband = %s, want \"@I2@\"", got[1]["Husband"])
	}
	for i, element := range got {
		if _, ok := element["Entity"]; ok {
			t.Errorf("element %d is a raw record, want its typed entity", i)
		}
	}
	if !strings.Contains(stdout, "{\n    \"Type\": \"INDI\",\n    \"XRef\": \"@I1@\",") {
		t.Errorf("individual does not start with its Type:\n%s", stdout)
	}
}

// TestGetJSONType checks that every record type carries its tag as Type.
func TestGetJSONType(t *testing.T) {
	xrefs := []string{"@U1@", "@I1@", "@F1@", "@S1@", "@R1@", "@O1@"}
	code, stdout, stderr := run(t, append([]string{"get", "--format", "json", records}, xrefs...)...)
	if code != ExitOK {
		t.Fatalf("exit = %d, stderr = %q", code, stderr)
	}
	var got []struct{ Type, XRef string }
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("stdout is not a JSON array: %v\n%s", err, stdout)
	}
	want := []string{"SUBM", "INDI", "FAM", "SOUR", "REPO", "OBJE"}
	if len(got) != len(want) {
		t.Fatalf("got %d elements, want %d", len(got), len(want))
	}
	for i, element := range got {
		if element.Type != want[i] || element.XRef != xrefs[i] {
			t.Errorf("element %d = %+v, want Type %s, XRef %s", i, element, want[i], xrefs[i])
		}
	}
}

// TestGetJSONUntypedRecord covers a record the library has no typed entity
// for: the record itself is emitted.
func TestGetJSONUntypedRecord(t *testing.T) {
	path := filepath.Join(t.TempDir(), "custom.ged")
	const data = "0 HEAD\n1 GEDC\n2 VERS 5.5.1\n1 CHAR UTF-8\n0 @X1@ _CUST\n1 _VAL invented\n0 TRLR\n"
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	code, stdout, stderr := run(t, "get", "--format", "json", path, "@X1@")
	if code != ExitOK {
		t.Fatalf("exit = %d, stderr = %q", code, stderr)
	}
	var got []map[string]any
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("stdout is not a JSON array: %v\n%s", err, stdout)
	}
	if len(got) != 1 || got[0]["XRef"] != "@X1@" || got[0]["Type"] != "_CUST" {
		t.Errorf("unexpected output:\n%s", stdout)
	}
}

const recordsHeader = `0 HEAD
1 SOUR gedcom-cli-test
1 GEDC
2 VERS 5.5.1
2 FORM LINEAGE-LINKED
1 CHAR UTF-8
1 SUBM @U1@
`

// TestGetHeaderText checks that HEAD prints the header's lines as the file
// has them, in argument order among records.
func TestGetHeaderText(t *testing.T) {
	if !strings.HasPrefix(readFile(t, records), recordsHeader) {
		t.Fatal("recordsHeader is not the fixture's header")
	}
	tests := []struct {
		name string
		args []string
		want string
	}{
		{"header alone", []string{"HEAD"}, recordsHeader},
		{"argument order", []string{"@I1@", "HEAD", "F1"}, recordI1 + recordsHeader + recordF1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code, stdout, stderr := run(t, append([]string{"get", records}, tt.args...)...)
			if code != ExitOK || stderr != "" {
				t.Fatalf("exit = %d, stderr = %q", code, stderr)
			}
			if stdout != tt.want {
				t.Errorf("stdout =\n%s\nwant\n%s", stdout, tt.want)
			}
		})
	}
}

// TestGetHeaderJSON checks that the header is the library's Header, with Type
// HEAD, in argument order among records.
func TestGetHeaderJSON(t *testing.T) {
	code, stdout, stderr := run(t, "get", "--format", "json", records, "@I1@", "HEAD")
	if code != ExitOK || stderr != "" {
		t.Fatalf("exit = %d, stderr = %q", code, stderr)
	}
	var got []struct {
		Type, XRef, SourceSystem, Submitter string
		Tags                                []struct{ Tag string }
	}
	if err := json.Unmarshal([]byte(stdout), &got); err != nil {
		t.Fatalf("stdout is not a JSON array: %v\n%s", err, stdout)
	}
	if len(got) != 2 || got[0].XRef != "@I1@" {
		t.Fatalf("unexpected output:\n%s", stdout)
	}
	header := got[1]
	if header.Type != "HEAD" || header.SourceSystem != "gedcom-cli-test" || header.Submitter != "@U1@" || len(header.Tags) != 6 {
		t.Errorf("header = %+v", header)
	}
	if !strings.Contains(stdout, "{\n    \"Type\": \"HEAD\",\n    \"Version\": \"5.5.1\",") {
		t.Errorf("header does not start with its Type:\n%s", stdout)
	}
}

// TestGetHeaderNotRedacted checks that --redact-living prints the header as
// the file has it, as the help says.
func TestGetHeaderNotRedacted(t *testing.T) {
	_, plain, _ := run(t, "get", living, "HEAD")
	code, redacted, stderr := run(t, "get", "--redact-living", living, "HEAD")
	if code != ExitOK || stderr != "" {
		t.Fatalf("exit = %d, stderr = %q", code, stderr)
	}
	if redacted != plain || !strings.HasPrefix(readFile(t, living), plain) {
		t.Errorf("redacted header =\n%s\nwant the file's\n%s", redacted, plain)
	}
}

// TestGetHeaderXRef checks that only HEAD names the header, @HEAD@ being an
// xref, and that a missing xref beside HEAD prints nothing.
func TestGetHeaderXRef(t *testing.T) {
	for _, arg := range []string{"@HEAD@", "head"} {
		code, stdout, stderr := run(t, "get", records, arg)
		if code != ExitError || stdout != "" || !strings.Contains(stderr, "no such record: @"+strings.Trim(arg, "@")+"@\n") {
			t.Errorf("%s: exit = %d, stdout = %q, stderr = %q", arg, code, stdout, stderr)
		}
	}
	// A missing xref fails before the header is printed.
	code, stdout, stderr := run(t, "get", records, "HEAD", "@NOPE@")
	if code != ExitError || stdout != "" || !strings.Contains(stderr, "no such record: @NOPE@\n") {
		t.Errorf("HEAD @NOPE@: exit = %d, stdout = %q, stderr = %q", code, stdout, stderr)
	}
}

func TestGetMissing(t *testing.T) {
	for _, format := range []string{formatText, formatJSON} {
		t.Run(format, func(t *testing.T) {
			code, stdout, stderr := run(t, "get", "--format", format, records, "@I1@", "@NOPE@", "ZZ9")
			if code != ExitError {
				t.Errorf("exit = %d, want %d", code, ExitError)
			}
			if stdout != "" {
				t.Errorf("stdout = %q, want nothing when a record is missing", stdout)
			}
			for _, want := range []string{"@NOPE@", "@ZZ9@"} {
				if !strings.Contains(stderr, want) {
					t.Errorf("stderr %q does not name %s", stderr, want)
				}
			}
			if strings.Contains(stderr, "@I1@") {
				t.Errorf("stderr %q names a record that exists", stderr)
			}
			if strings.Count(stderr, "\n") != 1 {
				t.Errorf("stderr = %q, want one message", stderr)
			}
		})
	}
}

// TestGetPartlyDelimitedXRef checks that a missing delimiter on either side is
// supplied, and that the error names the xref in its full form.
func TestGetPartlyDelimitedXRef(t *testing.T) {
	for _, xref := range []string{"I1", "@I1", "I1@", "@I1@"} {
		code, stdout, stderr := run(t, "get", records, xref)
		if code != ExitOK || stdout != recordI1 {
			t.Errorf("%s: exit = %d, stderr = %q, stdout = %q", xref, code, stderr, stdout)
		}
	}
	code, _, stderr := run(t, "get", records, "NOPE@")
	if code != ExitError || !strings.Contains(stderr, "no such record: @NOPE@\n") {
		t.Errorf("exit = %d, stderr = %q", code, stderr)
	}
}

func TestGetExitCodes(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want int
	}{
		{"no arguments", []string{"get"}, ExitUsage},
		{"file without xref", []string{"get", records}, ExitUsage},
		{"bad format", []string{"get", "--format", "xml", records, "@I1@"}, ExitUsage},
		{"missing file", []string{"get", "testdata/does-not-exist.ged", "@I1@"}, ExitError},
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

func TestGetHelpHasExample(t *testing.T) {
	code, stdout, stderr := run(t, "get", "--help")
	if code != ExitOK || stderr != "" {
		t.Fatalf("exit = %d, stderr = %q", code, stderr)
	}
	if !strings.Contains(stdout, "gedcom get family.ged @I1@") {
		t.Errorf("help has no example:\n%s", stdout)
	}
}
