package cli

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/cacack/gedcom-go/v3/decoder"
	"github.com/cacack/gedcom-go/v3/gedcom"
	"github.com/spf13/cobra"
)

// The actions of a move-value table, and the tag each puts the value at.
const (
	movePrependPlace = "prepend PLAC"
	moveToAddress    = "to ADDR"
	moveToPlace      = "to PLAC"
	moveToNote       = "to NOTE"
)

var moveActions = map[string]string{
	movePrependPlace: "PLAC",
	moveToAddress:    "ADDR",
	moveToPlace:      "PLAC",
	moveToNote:       "NOTE",
}

// moveRow is one row of a move-value table.
type moveRow struct {
	line   int      // in the table, the header being line 1
	path   []string // a tag path, or "*" and one tag
	value  string
	action string
	prefix string
}

// matches reports whether a line whose ancestors in its record, itself
// last, are stack, in a record of type recordType, is at r's path.
func (r moveRow) matches(recordType string, stack []*gedcom.Tag) bool {
	if r.path[0] == "*" {
		return len(stack) == 2 && stack[1].Tag == r.path[1]
	}
	if r.path[0] != recordType || len(stack) != len(r.path)-1 {
		return false
	}
	for i, tag := range stack {
		if tag.Tag != r.path[i+1] {
			return false
		}
	}
	return true
}

// moveValueReport is what move-value did: per row and in total, and what
// stopped the write.
type moveValueReport struct {
	Input   string `json:"input"`
	Table   string `json:"table"`
	Output  string `json:"output"`
	Written bool   `json:"written"`
	// Moved is the number of lines whose value moved; MapsRemoved the MAP
	// structures removed from a PLAC a value was prepended to.
	Moved          int            `json:"moved"`
	MapsRemoved    int            `json:"mapsRemoved"`
	RecordsChanged int            `json:"recordsChanged"`
	Rows           []moveRowStats `json:"rows"`
	// Unmatched is the table line of each row that matched no line.
	Unmatched []int `json:"unmatched"`
	// Refused is each matched line the row's action cannot be applied to,
	// which stopped the write.
	Refused  []moveRefusal  `json:"refused"`
	Counts   severityCounts `json:"counts"`
	Findings []finding      `json:"findings"`
}

// moveRowStats is what one table row did.
type moveRowStats struct {
	Line           int    `json:"line"`
	Path           string `json:"path"`
	Value          string `json:"value"`
	Action         string `json:"action"`
	Prefix         string `json:"prefix,omitempty"`
	Matched        int    `json:"matched"`
	Moved          int    `json:"moved"`
	MapsRemoved    int    `json:"mapsRemoved"`
	RecordsChanged int    `json:"recordsChanged"`
}

// moveRefusal is a matched line the action of row cannot be applied to.
type moveRefusal struct {
	XRef string `json:"xref"`
	// Line is the matched line's input line, and Tag its tag.
	Line int    `json:"line,omitempty"`
	Tag  string `json:"tag"`
	Row  int    `json:"row"`
	// Reason is "occupied" (the event already has the line the action
	// would make, named by Target), "missing" (the event has no PLAC to
	// prepend to), "conflict" (the PLAC is itself moved, or takes another
	// line too) or "substructures" (the line has lines under it, the first
	// named by Target).
	Reason string `json:"reason"`
	Target string `json:"target"`
}

// moveMatch is a line a row matched, and the event it stands under.
type moveMatch struct {
	record *gedcom.Record
	event  *gedcom.Tag
	line   *gedcom.Tag
	row    int // index into the table
}

func newMoveValueCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "move-value <file> <table.tsv> -o <out>",
		Short: "Move event lines' values into their PLAC, ADDR or a NOTE from a table, writing a new file",
		Long: `Move the value of a line under an event to another line of the same event,
as a table says, and write the result to --output. The input file is never
changed. It is the bulk edit for a value written on the wrong line: an ADDR
that is a building's name and belongs at the head of the PLAC, a PLAC that
is a street address, or a PLAC that is neither and belongs in a NOTE.

The table is tab-separated, with a header row naming its columns:

  path    the line's tag path, as 'gedcom list <file> tags' prints it, such
          as INDI.BIRT.ADDR, or *.<TAG> for a <TAG> line directly under any
          level-1 line of any record, such as *.PLAC (required)
  value   the line's whole value, exactly as written (required)
  action  what becomes of the value (required):
            prepend PLAC  it becomes the first element of the event's PLAC,
                          joined by ", ", and the line is removed; the MAP
                          under that PLAC is removed, its point being the
                          old place's
            to ADDR       the line becomes the event's ADDR
            to PLAC       the line becomes the event's PLAC
            to NOTE       the line becomes a NOTE of the event, the prefix
                          put before the value
  prefix  for to NOTE, text put before the value, such as "Place given as: "

Other columns are ignored. Cells are not quoted. The path names a line
below an event: a record type and at least two tags, the event being the
line one up the path. A value is matched byte for byte on the whole value,
its CONC and CONT lines folded in: a value continued with CONT holds a line
break, which no cell can, so it matches no row. An @@ in a 5.x file stays
@@, and a prefix's "@" is written as given. A line is moved by the first row,
in table order, that matches it in the input; a moved value is not matched
again. A row with no path, value or action, an unknown action, a prefix with
an action other than to NOTE, an action that puts the value on the tag the
path ends in, or a path and value given twice is reported with its line, the
exit code is 1 and nothing is written.

to ADDR, to PLAC and to NOTE retag the line where it stands and keep its
value. A line refused, with its record and input line, stops the write with
exit code 1:

  occupied       to ADDR or to PLAC on an event that already has that line,
                 or that two matched lines would give it
  missing        prepend PLAC on an event with no PLAC
  conflict       prepend PLAC to a PLAC that is itself moved, or that
                 another matched line is prepended to
  substructures  a matched line with lines under it other than its CONC and
                 CONT: no action says what becomes of them, and nothing is
                 dropped silently

Before writing, the output is validated by gedcom-go, with its grammar check,
against the file's GEDCOM version. An edit that adds a finding the input did
not have writes nothing and exits 1, with those findings reported. A row that
matches no line is not an error: the report lists it. The command decides
nothing: which values are building names is the table's to say. Every other
line is written exactly as it was read, so a table with no rows writes the
file byte-identical.

The report gives the lines moved, the MAP structures removed and the records
changed, per row and in total, and the rows that matched nothing. With
--redact-living, the value of a row that matched a line of a presumed-living
person reads Living, as does a finding about one; the file written is not
redacted.

-o is required and may not name file or table.tsv. An existing output file
is replaced only once the whole result is written. With -o - the GEDCOM goes
to stdout and the report to stderr.`,
		Example: `  gedcom list --format json --tag INDI.BIRT.ADDR family.ged values
  printf 'path\tvalue\taction\tprefix\nINDI.BIRT.ADDR\tExample General Hospital\tprepend PLAC\t\n*.PLAC\tat home\tto NOTE\tPlace given as: \n' > moves.tsv
  gedcom move-value family.ged moves.tsv -o out.ged
  gedcom move-value --format json family.ged moves.tsv -o out.ged | jq .unmatched`,
		Args: exactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			format, err := outputFormat(cmd)
			if err != nil {
				return err
			}
			opts, err := readLivingOptions(cmd)
			if err != nil {
				return err
			}
			out, err := outputPath(cmd, args[0], args[1])
			if err != nil {
				return err
			}
			table, err := readMoveTable(args[1])
			if err != nil {
				return err
			}
			report, editErr := moveValues(args[0], table, opts, func(doc *gedcom.Document) error {
				return writeOutput(cmd, out, doc)
			})
			if report == nil {
				return editErr
			}
			report.Input, report.Table, report.Output = args[0], args[1], out
			if editErr != nil {
				editErr = fmt.Errorf("%w; %s not written", editErr, outputName(out))
			}
			w := reportWriter(cmd, out)
			if format == formatJSON {
				err = writeJSON(w, report)
			} else {
				err = writeMoveValueText(w, report)
			}
			if err != nil {
				return err
			}
			return editErr
		},
	}
	addOutputFlag(cmd)
	return cmd
}

// readMoveTable reads a move-value table, reporting every malformed row.
func readMoveTable(name string) ([]moveRow, error) {
	f, err := os.Open(filepath.Clean(name))
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var rows []moveRow
	var errs []string
	fail := func(n int, format string, a ...any) {
		errs = append(errs, fmt.Sprintf("%s line %d: %s", name, n, fmt.Sprintf(format, a...)))
	}
	var columns map[string]int
	seen := map[[2]string]int{}
	scanner := bufio.NewScanner(f)
	for n := 1; scanner.Scan(); n++ {
		line := strings.TrimSuffix(scanner.Text(), "\r")
		if line == "" {
			continue
		}
		cells := strings.Split(line, "\t")
		if columns == nil {
			if columns, err = moveColumns(cells); err != nil {
				fail(n, "%v", err)
				break
			}
			continue
		}
		cell := func(column string) string {
			if i, ok := columns[column]; ok && i < len(cells) {
				return cells[i]
			}
			return ""
		}
		row := moveRow{line: n, value: cell("value"), action: cell("action"), prefix: cell("prefix")}
		path := cell("path")
		target, known := moveActions[row.action]
		switch {
		case path == "":
			fail(n, "no path")
			continue
		case row.value == "":
			fail(n, "no value")
			continue
		case row.action == "":
			fail(n, "no action")
			continue
		case !known:
			fail(n, "unknown action %q: want %q, %q, %q or %q", row.action, movePrependPlace, moveToAddress, moveToPlace, moveToNote)
			continue
		case row.prefix != "" && row.action != moveToNote:
			fail(n, "a prefix is for %q only", moveToNote)
			continue
		}
		if row.path, err = movePath(path); err != nil {
			fail(n, "%v", err)
			continue
		}
		if row.path[len(row.path)-1] == target {
			fail(n, "%q puts the value on the %s it is already on", row.action, target)
			continue
		}
		key := [2]string{path, row.value}
		if first, ok := seen[key]; ok {
			fail(n, "path %s and value %q are already on line %d", path, row.value, first)
			continue
		}
		seen[key] = n
		rows = append(rows, row)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if len(errs) > 0 {
		return nil, errors.New(strings.Join(errs, "\n"))
	}
	return rows, nil
}

// moveColumns maps the columns a move-value table's header names to their
// index. It needs path, value and action. Columns of other names are
// ignored, and may repeat.
func moveColumns(header []string) (map[string]int, error) {
	columns := map[string]int{}
	for i, name := range header {
		if !slices.Contains([]string{"path", "value", "action", "prefix"}, name) {
			continue
		}
		if _, ok := columns[name]; ok {
			return nil, fmt.Errorf("column %q is named twice", name)
		}
		columns[name] = i
	}
	if !hasColumn(columns, "path") || !hasColumn(columns, "value") || !hasColumn(columns, "action") {
		return nil, errors.New(`want a header row naming "path", "value" and "action" columns, and "prefix" as needed`)
	}
	return columns, nil
}

// movePath reads a row's path: a record type and at least two tags below
// it, or "*" and one tag.
func movePath(arg string) ([]string, error) {
	path := strings.Split(arg, ".")
	bad := len(path) < 2 || (path[0] != "*" && len(path) < 3) || (path[0] == "*" && len(path) != 2)
	for i, tag := range path {
		if i == 0 && tag == "*" {
			continue
		}
		bad = bad || !tagPattern.MatchString(tag) || isContinuation(tag)
	}
	if bad {
		return nil, fmt.Errorf("invalid path %q: want a record type and the tags down to a line under an event, joined by dots, such as INDI.BIRT.ADDR, or *.<TAG>", arg)
	}
	return path, nil
}

// moveValues applies the table's rows to the file at in and, when no line
// was refused and validation finds nothing new, writes the result with
// write. It returns the report once the file is read, with an error when the
// write was stopped.
func moveValues(in string, table []moveRow, opts livingOptions, write func(*gedcom.Document) error) (*moveValueReport, error) {
	res, err := decodeForWrite(in)
	if err != nil {
		return nil, err
	}
	doc := res.Document
	hidden := hiddenRecords(doc, presumedLiving(doc, opts.cfg))
	before := validationKeys(doc)
	report := &moveValueReport{Rows: make([]moveRowStats, len(table)), Unmatched: []int{}, Refused: []moveRefusal{}, Findings: []finding{}}
	for i, row := range table {
		report.Rows[i] = moveRowStats{Line: row.line, Path: strings.Join(row.path, "."), Value: row.value, Action: row.action, Prefix: row.prefix}
	}

	matches := findMoveMatches(doc, table)
	redacted := make([]bool, len(table))
	for _, m := range matches {
		report.Rows[m.row].Matched++
		redacted[m.row] = redacted[m.row] || hidden[m.record]
	}
	refusals, refused := refuseMoves(matches, table)
	report.Refused = refusals

	changed := map[*gedcom.Record]bool{}
	rowChanged := make([]map[*gedcom.Record]bool, len(table))
	for j, m := range matches {
		if refused[j] {
			continue
		}
		maps := applyMove(doc, m, table[m.row])
		stats := &report.Rows[m.row]
		stats.Moved++
		stats.MapsRemoved += maps
		if rowChanged[m.row] == nil {
			rowChanged[m.row] = map[*gedcom.Record]bool{}
		}
		rowChanged[m.row][m.record] = true
		changed[m.record] = true
	}
	for i := range report.Rows {
		stats := &report.Rows[i]
		stats.RecordsChanged = len(rowChanged[i])
		report.Moved += stats.Moved
		report.MapsRemoved += stats.MapsRemoved
		if stats.Matched == 0 {
			report.Unmatched = append(report.Unmatched, stats.Line)
		}
		if opts.redact && redacted[i] {
			stats.Value = redactedValue
		}
	}
	report.RecordsChanged = len(changed)
	if len(report.Refused) > 0 {
		return report, fmt.Errorf("%s refused", plural(len(report.Refused), "line is", "lines are"))
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

// findMoveMatches is every line of doc's records a row matches, each by the
// first row in table order that does, in file order.
func findMoveMatches(doc *gedcom.Document, table []moveRow) []moveMatch {
	byValue := map[string][]int{}
	for i, row := range table {
		byValue[row.value] = append(byValue[row.value], i)
	}
	var matches []moveMatch
	for _, record := range doc.Records {
		// stack is the line being read and its ancestors in the record.
		var stack []*gedcom.Tag
		for _, tag := range record.Tags {
			if tag == nil {
				continue
			}
			if tag.Level < 1 || tag.Level-1 > len(stack) {
				stack = nil
				continue
			}
			stack = append(stack[:tag.Level-1], tag)
			if len(stack) < 2 {
				continue
			}
			for _, i := range byValue[record.FullValue(tag)] {
				if table[i].matches(string(record.Type), stack) {
					matches = append(matches, moveMatch{record: record, event: stack[len(stack)-2], line: tag, row: i})
					break
				}
			}
		}
	}
	return matches
}

// refuseMoves is each match whose action cannot be applied, judged on the
// input, and the indexes of those matches.
func refuseMoves(matches []moveMatch, table []moveRow) ([]moveRefusal, map[int]bool) {
	moved := map[*gedcom.Tag]bool{}
	for _, m := range matches {
		moved[m.line] = true
	}
	// taken is, per event, the tags a match before has made or prepended
	// to.
	taken := map[*gedcom.Tag]map[string]bool{}
	refused := []moveRefusal{}
	indexes := map[int]bool{}
	for j, m := range matches {
		row := table[m.row]
		target := moveActions[row.action]
		refuse := func(reason, target string) {
			indexes[j] = true
			refused = append(refused, moveRefusal{XRef: m.record.XRef, Line: m.line.LineNumber, Tag: m.line.Tag, Row: row.line, Reason: reason, Target: target})
		}
		if sub := firstSubstructure(m.record, m.line); sub != nil {
			refuse("substructures", sub.Tag)
			continue
		}
		if taken[m.event] == nil {
			taken[m.event] = map[string]bool{}
		}
		switch row.action {
		case movePrependPlace:
			plac := childTag(m.record, m.event, "PLAC")
			switch {
			case plac == nil:
				refuse("missing", target)
			case moved[plac] || taken[m.event][target]:
				refuse("conflict", target)
			}
		case moveToAddress, moveToPlace:
			if childTag(m.record, m.event, target) != nil || taken[m.event][target] {
				refuse("occupied", target)
			}
		}
		if row.action != moveToNote {
			taken[m.event][target] = true
		}
	}
	return refused, indexes
}

// firstSubstructure is the first line directly under line in record other
// than a CONC or CONT continuing its value, or nil.
func firstSubstructure(record *gedcom.Record, line *gedcom.Tag) *gedcom.Tag {
	for _, tag := range record.Subtree(line)[1:] {
		if tag != nil && tag.Level == line.Level+1 && !isContinuation(tag.Tag) {
			return tag
		}
	}
	return nil
}

// applyMove applies row's action to the line m matched, editing the record's
// raw tags, and returns the number of MAP structures it removed.
func applyMove(doc *gedcom.Document, m moveMatch, row moveRow) int {
	var version gedcom.Version
	if doc.Header != nil {
		version = doc.Header.Version
	}
	record := m.record
	switch row.action {
	case movePrependPlace:
		plac := childTag(record, m.event, "PLAC")
		if plac == nil {
			return 0
		}
		maps := record.RemoveChildren(plac, "MAP")
		record.SetFullValue(plac, record.FullValue(m.line)+", "+record.FullValue(plac), version)
		if i := slices.Index(record.Tags, m.line); i >= 0 {
			record.Tags = slices.Delete(record.Tags, i, i+len(record.Subtree(m.line)))
		}
		return maps
	case moveToNote:
		m.line.Tag = "NOTE"
		if row.prefix != "" {
			record.SetFullValue(m.line, row.prefix+record.FullValue(m.line), version)
		}
	default:
		m.line.Tag = moveActions[row.action]
	}
	return 0
}

// moveRefusalText is how the text report names each refusal reason.
var moveRefusalText = map[string]string{
	"occupied":      "event has %s",
	"missing":       "event has no %s",
	"conflict":      "event's %s is moved, or prepended to twice",
	"substructures": "line has %s under it",
}

// writeMoveValueText writes a summary line, one line per row, the rows that
// matched nothing, then what stopped the write: the refused lines, or the
// findings the edit adds.
func writeMoveValueText(w io.Writer, r *moveValueReport) error {
	var b strings.Builder
	if r.Written {
		fmt.Fprintf(&b, "wrote %s: ", outputName(r.Output))
	} else {
		fmt.Fprintf(&b, "%s not written: ", outputName(r.Output))
	}
	fmt.Fprintf(&b, "%s moved, %d MAP removed, %s changed\n", plural(r.Moved, "line", "lines"), r.MapsRemoved,
		plural(r.RecordsChanged, "record", "records"))
	for _, row := range r.Rows {
		if row.Matched == 0 {
			continue
		}
		fmt.Fprintf(&b, "  row %d\t%s\t%s\t%s\t%d moved, %d MAP removed, %s changed\n", row.Line, row.Path, oneLine(row.Value), row.Action,
			row.Moved, row.MapsRemoved, plural(row.RecordsChanged, "record", "records"))
	}
	for _, row := range r.Rows {
		if row.Matched == 0 {
			fmt.Fprintf(&b, "  unmatched\trow %d\t%s\t%s\n", row.Line, row.Path, oneLine(row.Value))
		}
	}
	for _, f := range r.Refused {
		fmt.Fprintf(&b, "  "+moveRefusalText[f.Reason]+"\t%s\tline %d\t%s\trow %d\n", f.Target, oneLine(f.XRef), f.Line, f.Tag, f.Row)
	}
	if _, err := io.WriteString(w, b.String()); err != nil {
		return err
	}
	if len(r.Findings) == 0 {
		return nil
	}
	return writeFindingGroups(w, r.Counts, r.Findings)
}
