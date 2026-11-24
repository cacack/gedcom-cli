package cli

import (
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/cacack/gedcom-go/v3/gedcom"
	"github.com/spf13/cobra"
)

// diffReport is the output of diff: the library's DocumentDiff, flattened to
// xrefs, types and GEDCOM lines. Every list is non-nil, so that none is [] in
// JSON.
type diffReport struct {
	Old     string        `json:"old"`
	New     string        `json:"new"`
	Added   []diffRecord  `json:"added"`
	Removed []diffRecord  `json:"removed"`
	Changed []diffChanged `json:"changed"`
	Header  *diffChanged  `json:"header"`
}

type diffRecord struct {
	XRef string `json:"xref"`
	Type string `json:"type"`
}

// diffChanged is a record both files have whose meaning differs, or the
// header. Redacted is set, and Changes empty, when --redact-living hides it.
type diffChanged struct {
	diffRecord
	Redacted bool       `json:"redacted"`
	Changes  []diffHunk `json:"changes"`
}

// diffHunk is one library LineChange: the lines under one parent structure
// that only the old file has, and those only the new file has.
type diffHunk struct {
	Path    []string   `json:"path"`
	Removed []diffLine `json:"removed"`
	Added   []diffLine `json:"added"`
}

// diffLine is one GEDCOM line and its line number in its own file.
type diffLine struct {
	Line int    `json:"line"`
	Text string `json:"text"`
}

func newDiffCmd() *cobra.Command {
	var ignore []string // bound directly: GetStringArray reads back a lone "" as no tag
	cmd := &cobra.Command{
		Use:   "diff <old> <new>",
		Short: "Compare two versions of a file record by record: added, removed and changed",
		Long: `Compare two versions of the same tree, such as an earlier export and a
re-download, record by record with gedcom-go's document diff. Records are
matched by xref and type: a renumbered record, or one whose type changed, is
reported as removed and added. Neither file is changed.

Only changes in meaning are reported. These are not changes:

  - substructures in a different order relative to siblings with a different
    tag (BIRT before or after NAME)
  - a value split differently across CONT/CONC lines, when the joined text is
    the same
  - line numbers

Everything else is a change, including reordering among siblings with the
same tag (the first NAME is the preferred one, CHIL is in birth order) and
case or whitespace in a value.

The header (HEAD) is not compared unless --include-header is given: its DATE
and TIME change on every export. --ignore-tag TAG (repeatable, exact and
case-sensitive) leaves a tag out at every level with everything under it, and
a record of that type is not compared at all; use it for vendor tags that
change between exports of an unchanged tree, such as _USER, _HPID and _WPID.
It applies to the header too, so --ignore-tag DATE would also hide every
record's dates.

Text output is one line per record, tab-separated: added, removed or changed,
then the xref and the type (the header is "changed", no xref, HEAD). Added
and removed records are listed only; 'gedcom get' prints them. Each changed
record is followed by its hunks: "  in TYPE > TAG ..." naming the structure the
lines belong to, then "  - line N: ..." for a line of the old file and
"  + line N: ..." for one of the new, in GEDCOM form. A hunk with both is a
structure whose own value changed; one with only removals or additions is
whole structures with every line under them. Files with no difference print
"no differences".

--format json prints one object with old, new, added, removed, changed and
header (null when not compared or equal; its xref is ""). Each change has
path, removed and added, and each line has line and text. path is the tags
below the record, without its type: ["BIRT"] is printed "in INDI > BIRT", and
[] is the record's own lines.

Differences are not a failure: diff exits 0 whether or not it finds any. With
--redact-living, a changed record that is hidden in either file is listed with
its lines replaced by Living (redacted in JSON).`,
		Example: `  gedcom diff old-export.ged family.ged
  gedcom diff --ignore-tag _USER --ignore-tag _HPID --ignore-tag _WPID old-export.ged family.ged
  gedcom diff --include-header old-export.ged family.ged
  gedcom diff --format json old-export.ged family.ged | jq '.changed[].xref'`,
		Args: exactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			format, err := outputFormat(cmd)
			if err != nil {
				return err
			}
			opts, err := diffOptions(cmd, ignore)
			if err != nil {
				return err
			}
			living, err := readLivingOptions(cmd)
			if err != nil {
				return err
			}
			oldDoc, err := decodeFile(args[0])
			if err != nil {
				return err
			}
			newDoc, err := decodeFile(args[1])
			if err != nil {
				return err
			}
			// The diff sees both files whole; --redact-living then hides the
			// lines of each changed record either file hides.
			hidden := func(*gedcom.Record) bool { return false }
			if living.redact {
				oldHidden := hiddenRecords(oldDoc, presumedLiving(oldDoc, living.cfg))
				newHidden := hiddenRecords(newDoc, presumedLiving(newDoc, living.cfg))
				hidden = func(r *gedcom.Record) bool { return oldHidden[r] || newHidden[r] }
			}
			report := newDiffReport(args[0], args[1], gedcom.Diff(oldDoc, newDoc, opts), hidden)
			if format == formatJSON {
				return writeJSON(cmd.OutOrStdout(), report)
			}
			return writeDiffText(cmd.OutOrStdout(), report)
		},
	}
	cmd.Flags().StringArrayVar(&ignore, "ignore-tag", nil, "leave this tag out of the comparison, with everything under it (repeatable)")
	cmd.Flags().Bool("include-header", false, "compare the header (HEAD) too")
	return cmd
}

// diffOptions reads and checks the diff flags.
func diffOptions(cmd *cobra.Command, ignore []string) (*gedcom.DiffOptions, error) {
	for _, tag := range ignore {
		if tag == "" || strings.ContainsAny(tag, " \t\r\n@") {
			return nil, usageError{fmt.Errorf("invalid --ignore-tag %q: want a tag such as _USER", tag)}
		}
	}
	header, err := cmd.Flags().GetBool("include-header")
	if err != nil {
		return nil, err
	}
	return &gedcom.DiffOptions{IgnoreTags: ignore, IncludeHeader: header}, nil
}

// newDiffReport flattens the library's diff. hidden reports whether
// --redact-living hides a record.
func newDiffReport(oldPath, newPath string, d *gedcom.DocumentDiff, hidden func(*gedcom.Record) bool) diffReport {
	report := diffReport{
		Old:     oldPath,
		New:     newPath,
		Added:   make([]diffRecord, 0, len(d.Added)),
		Removed: make([]diffRecord, 0, len(d.Removed)),
		Changed: make([]diffChanged, 0, len(d.Modified)),
	}
	for _, r := range d.Added {
		report.Added = append(report.Added, diffRecord{XRef: r.XRef, Type: string(r.Type)})
	}
	for _, r := range d.Removed {
		report.Removed = append(report.Removed, diffRecord{XRef: r.XRef, Type: string(r.Type)})
	}
	for _, m := range d.Modified {
		report.Changed = append(report.Changed, newDiffChanged(m, hidden(m.Old) || hidden(m.New)))
	}
	if d.Header != nil {
		header := newDiffChanged(d.Header, false)
		report.Header = &header
	}
	return report
}

func newDiffChanged(m *gedcom.RecordDiff, redacted bool) diffChanged {
	c := diffChanged{diffRecord: diffRecord{XRef: m.XRef, Type: string(m.Type)}, Redacted: redacted, Changes: []diffHunk{}}
	if redacted {
		return c
	}
	for _, change := range m.Changes {
		c.Changes = append(c.Changes, diffHunk{
			Path:    append([]string{}, change.Path...),
			Removed: diffLines(change.Removed),
			Added:   diffLines(change.Added),
		})
	}
	return c
}

func diffLines(tags []*gedcom.Tag) []diffLine {
	lines := make([]diffLine, 0, len(tags))
	for _, t := range tags {
		lines = append(lines, diffLine{Line: t.LineNumber, Text: gedcomLine(t)})
	}
	return lines
}

// gedcomLine prints a tag as the line it was read from: level, xref, tag and
// value.
func gedcomLine(t *gedcom.Tag) string {
	fields := []string{strconv.Itoa(t.Level)}
	if t.XRef != "" {
		fields = append(fields, t.XRef)
	}
	line := strings.Join(append(fields, t.Tag), " ")
	if t.Value != "" {
		line += " " + t.Value
	}
	return line
}

func writeDiffText(w io.Writer, report diffReport) error {
	if len(report.Added)+len(report.Removed)+len(report.Changed) == 0 && report.Header == nil {
		_, err := fmt.Fprintf(w, "%s and %s: no differences\n", report.Old, report.New)
		return err
	}
	var errs []error
	printf := func(format string, args ...any) {
		_, err := fmt.Fprintf(w, format, args...)
		errs = append(errs, err)
	}
	for _, r := range report.Added {
		printf("added\t%s\t%s\n", r.XRef, r.Type)
	}
	for _, r := range report.Removed {
		printf("removed\t%s\t%s\n", r.XRef, r.Type)
	}
	changed := report.Changed
	if report.Header != nil {
		changed = append(changed, *report.Header)
	}
	for _, c := range changed {
		printf("changed\t%s\t%s\n", c.XRef, c.Type)
		if c.Redacted {
			printf("  %s\n", redactedValue)
		}
		for _, h := range c.Changes {
			printf("  in %s\n", strings.Join(append([]string{c.Type}, h.Path...), " > "))
			for _, l := range h.Removed {
				printf("  - line %d: %s\n", l.Line, l.Text)
			}
			for _, l := range h.Added {
				printf("  + line %d: %s\n", l.Line, l.Text)
			}
		}
	}
	return errors.Join(errs...)
}
