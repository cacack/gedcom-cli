package cli

import (
	"fmt"
	"io"
	"regexp"
	"slices"
	"strings"

	"github.com/cacack/gedcom-go/v3/decoder"
	"github.com/cacack/gedcom-go/v3/gedcom"
	"github.com/spf13/cobra"
)

// retagReport is what retag did: the lines it renamed and added, and the
// findings the edit would have added, which stopped the write.
type retagReport struct {
	Input   string `json:"input"`
	Output  string `json:"output"`
	Written bool   `json:"written"`
	Path    string `json:"path"`
	Tag     string `json:"tag"`
	// Renamed is the number of lines at Path given the new tag; Added is the
	// number of lines put under them.
	Renamed        int `json:"renamed"`
	Added          int `json:"added"`
	RecordsChanged int `json:"recordsChanged"`
	// Occupied is each structure at Path that already has a line --add would
	// add, which stopped the write.
	Occupied []retagOccupied `json:"occupied"`
	Counts   severityCounts  `json:"counts"`
	Findings []finding       `json:"findings"`
}

// retagOccupied is a structure that already has a line tagged Tag directly
// under it.
type retagOccupied struct {
	XRef string `json:"xref"`
	Line int    `json:"line,omitempty"`
	Tag  string `json:"tag"`
}

// retagLine is a line --add puts under each renamed line.
type retagLine struct {
	tag, value string
}

// tagPattern is a GEDCOM tag: a standard one, or an extension tag with its
// leading underscore.
var tagPattern = regexp.MustCompile(`^_?[A-Z][A-Z0-9_]*$`)

func newRetagCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "retag <file> <path> <tag> -o <out>",
		Short: "Give every line at one tag path a new tag, writing a new file",
		Long: `Give every line at a tag path a new tag, and write the result to --output.
The input file is never changed. It is the bulk edit for a structure written
with one tag that belongs under another: a vendor tag that has a standard
form, such as an Ancestry _MILT that is an EVEN with a TYPE, or a record's
_APID that is a GEDCOM 7.0 EXID.

<path> is a tag path as 'gedcom list <file> tags' prints it: a record type
and the tags down to the line, joined by dots, such as INDI._MILT or
SOUR._APID. Only lines at exactly that path are renamed: INDI._MILT does not
match a _MILT anywhere else, and SOUR._APID does not match the _APID of a
citation (INDI.BIRT.SOUR._APID). The path names lines below the record
level; a record's own tag, the header and continuation lines (CONC, CONT)
cannot be renamed. <tag> is the new tag.

Each renamed line keeps its value and every line under it. --add puts a new
line directly under each renamed line, before its other lines, and may be
repeated; the lines are added in the order given:

  --add "TYPE Military Service"    a tag and its value
  --add MAP                        a tag alone

A structure that already has a line with an --add tag directly under it is
reported with its xref and input line, the exit code is 1 and nothing is
written: which of the two lines is right is yours to settle with 'gedcom
put'. A value's "@" is written as given.

Before writing, the output is validated by gedcom-go, with its grammar check,
against the file's GEDCOM version. An edit that adds a finding the input did
not have, such as a tag the version does not permit where the line stands,
writes nothing and exits 1, with those findings reported. A path that matches
no line is not an error: the report says 0 renamed and the file is written
byte-identical. Every other line is written exactly as it was read.

The report gives the lines renamed, the lines added and the records changed.
With --redact-living, a finding about a presumed-living person reads Living;
the file written is not redacted.

-o is required and may not name file. An existing output file is replaced
only once the whole result is written. With -o - the GEDCOM goes to stdout
and the report to stderr.`,
		Example: `  gedcom list --vendor family.ged tags
  gedcom retag family.ged INDI._MILT EVEN --add "TYPE Military Service" -o out.ged
  gedcom retag family.ged SOUR._APID EXID --add "TYPE https://example.com/ids/" -o out.ged
  gedcom retag --format json family.ged INDI._DEG GRAD -o out.ged | jq .renamed`,
		Args: exactArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			format, err := outputFormat(cmd)
			if err != nil {
				return err
			}
			opts, err := readLivingOptions(cmd)
			if err != nil {
				return err
			}
			out, err := outputPath(cmd, args[0])
			if err != nil {
				return err
			}
			path, err := retagPath(args[1])
			if err != nil {
				return err
			}
			tag := args[2]
			if !tagPattern.MatchString(tag) || isContinuation(tag) {
				return usageError{fmt.Errorf("invalid tag %q: want a GEDCOM tag such as EVEN or _MILT", tag)}
			}
			add, err := readRetagLines(cmd)
			if err != nil {
				return err
			}

			report, editErr := retag(args[0], path, tag, add, opts, func(doc *gedcom.Document) error {
				return writeOutput(cmd, out, doc)
			})
			if report == nil {
				return editErr
			}
			report.Input, report.Output = args[0], out
			if editErr != nil {
				editErr = fmt.Errorf("%w; %s not written", editErr, outputName(out))
			}
			w := reportWriter(cmd, out)
			if format == formatJSON {
				err = writeJSON(w, report)
			} else {
				err = writeRetagText(w, report)
			}
			if err != nil {
				return err
			}
			return editErr
		},
	}
	addOutputFlag(cmd)
	cmd.Flags().StringArray("add", nil, `a line to put directly under each renamed line, as "TAG value" or "TAG"; may be repeated`)
	return cmd
}

func isContinuation(tag string) bool { return tag == "CONC" || tag == "CONT" }

// retagPath reads a tag path: a record type and at least one tag below it.
func retagPath(arg string) ([]string, error) {
	path := strings.Split(arg, ".")
	bad := len(path) < 2 || isContinuation(path[len(path)-1])
	for _, tag := range path {
		bad = bad || !tagPattern.MatchString(tag)
	}
	if bad {
		return nil, usageError{fmt.Errorf("invalid path %q: want a record type and the tags below it, joined by dots, such as INDI._MILT", arg)}
	}
	return path, nil
}

// readRetagLines reads the --add flags.
func readRetagLines(cmd *cobra.Command) ([]retagLine, error) {
	args, err := cmd.Flags().GetStringArray("add")
	if err != nil {
		return nil, err
	}
	var lines []retagLine
	for _, arg := range args {
		tag, value, _ := strings.Cut(arg, " ")
		switch {
		case !tagPattern.MatchString(tag) || isContinuation(tag):
			return nil, usageError{fmt.Errorf(`invalid --add %q: want "TAG value" or "TAG"`, arg)}
		case strings.ContainsAny(value, "\r\n"):
			return nil, usageError{fmt.Errorf("--add %q holds a line break", arg)}
		case slices.ContainsFunc(lines, func(l retagLine) bool { return l.tag == tag }):
			return nil, usageError{fmt.Errorf("--add gives %s twice", tag)}
		}
		lines = append(lines, retagLine{tag: tag, value: value})
	}
	return lines, nil
}

// retag gives each line at path in the file at in the tag tag, puts the add
// lines under it and, when no structure was occupied and validation finds
// nothing new, writes the result with write. It returns the report once the
// file is read, with an error when the write was stopped.
func retag(in string, path []string, tag string, add []retagLine, opts livingOptions, write func(*gedcom.Document) error) (*retagReport, error) {
	res, err := decodeForWrite(in)
	if err != nil {
		return nil, err
	}
	doc := res.Document
	hidden := hiddenRecords(doc, presumedLiving(doc, opts.cfg))
	before := validationKeys(doc)
	report := &retagReport{
		Path:     strings.Join(path, "."),
		Tag:      tag,
		Occupied: []retagOccupied{},
		Findings: []finding{},
	}

	for _, record := range doc.Records {
		if string(record.Type) != path[0] {
			continue
		}
		// FindAll returns the lines at the path; the edits below keep every
		// one of them in record.Tags.
		lines := record.FindAll(path[1:]...)
		for _, line := range lines {
			for _, a := range add {
				if childTag(record, line, a.tag) != nil {
					report.Occupied = append(report.Occupied, retagOccupied{XRef: record.XRef, Line: line.LineNumber, Tag: a.tag})
				}
			}
			line.Tag = tag
			added := make([]*gedcom.Tag, len(add))
			for i, a := range add {
				added[i] = &gedcom.Tag{Level: line.Level + 1, Tag: a.tag, Value: a.value}
			}
			record.Tags = slices.Insert(record.Tags, slices.Index(record.Tags, line)+1, added...)
			report.Added += len(added)
		}
		report.Renamed += len(lines)
		if len(lines) > 0 {
			report.RecordsChanged++
		}
	}
	if len(report.Occupied) > 0 {
		return report, fmt.Errorf("%s --add would add", plural(len(report.Occupied), "structure already has a line", "structures already have a line"))
	}

	decoder.SyncEntities(doc)
	report.Findings = addedFindings(doc, before)
	if opts.redact {
		redactFindings(report.Findings, doc, hidden)
	}
	report.Counts = countSeverities(report.Findings)
	if len(report.Findings) > 0 {
		return report, fmt.Errorf("the edit adds %s", plural(len(report.Findings), "finding", "findings"))
	}
	if err := write(doc); err != nil {
		return nil, err
	}
	report.Written = true
	return report, nil
}

// writeRetagText writes a summary line, then what stopped the write: the
// occupied structures, or the findings the edit adds.
func writeRetagText(w io.Writer, r *retagReport) error {
	var b strings.Builder
	if r.Written {
		fmt.Fprintf(&b, "wrote %s: ", outputName(r.Output))
	} else {
		fmt.Fprintf(&b, "%s not written: ", outputName(r.Output))
	}
	fmt.Fprintf(&b, "%s %s renamed %s, %s added, %s changed\n", plural(r.Renamed, "line", "lines"), r.Path, r.Tag,
		plural(r.Added, "line", "lines"), plural(r.RecordsChanged, "record", "records"))
	for _, o := range r.Occupied {
		fmt.Fprintf(&b, "  has %s\t%s\tline %d\n", o.Tag, oneLine(o.XRef), o.Line)
	}
	if _, err := io.WriteString(w, b.String()); err != nil {
		return err
	}
	if len(r.Findings) == 0 {
		return nil
	}
	return writeFindingGroups(w, r.Counts, r.Findings)
}
