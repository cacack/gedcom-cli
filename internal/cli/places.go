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

	"github.com/cacack/gedcom-go/v3/gedcom"
	"github.com/cacack/gedcom-go/v3/validator"
	"github.com/spf13/cobra"
)

// placeTableRow is one row of a place table: what a PLAC value becomes.
type placeTableRow struct {
	line      int    // in the table, the header being line 1
	standard  string // the replacement value; "" keeps the value as written
	latitude  string // "" when the row gives no coordinates
	longitude string
}

// placeTable is a place table: its rows by place, and the places in table
// order.
type placeTable struct {
	rows   map[string]placeTableRow
	places []string
}

// placesReport is what places apply did.
type placesReport struct {
	Input  string `json:"input"`
	Table  string `json:"table"`
	Output string `json:"output"`
	// PlacesRewritten is the number of PLAC lines whose value changed.
	PlacesRewritten int `json:"placesRewritten"`
	// MapsAdded and MapsUpdated are the MAP structures added under a PLAC,
	// and those already there whose LATI or LONG changed.
	MapsAdded   int `json:"mapsAdded"`
	MapsUpdated int `json:"mapsUpdated"`
	// MapsNotPermitted is the input line of each PLAC that would have gained
	// a MAP but stands where the file's version permits none.
	MapsNotPermitted []int `json:"mapsNotPermitted"`
	RecordsChanged   int   `json:"recordsChanged"`
	// Unmatched is the table's places that no PLAC has, in table order.
	Unmatched []string `json:"unmatched"`
}

func newPlacesCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "places",
		Short: "Rewrite PLAC values and set their coordinates from a place table",
		Long: `Work with the places inside a GEDCOM file.

  apply  rewrite PLAC values, and set MAP coordinates, from a table`,
		Example: `  gedcom places apply family.ged places.tsv -o out.ged`,
		Args: func(_ *cobra.Command, args []string) error {
			if len(args) > 0 {
				return usageError{fmt.Errorf("unknown places command %q; see 'gedcom places --help'", args[0])}
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, _ []string) error { return cmd.Help() },
	}
	cmd.AddCommand(newPlacesApplyCmd())
	return cmd
}

func newPlacesApplyCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "apply <file> <table.tsv> -o <out>",
		Short: "Rewrite PLAC values and set MAP coordinates from a place table, writing a new file",
		Long: `Rewrite each PLAC whose value is in a place table to the table's standard
name, and give it the table's coordinates, writing the result to --output, or
to stdout with -o - (the report then goes to stderr). The input file is never
changed.

The table is tab-separated, with a header row naming its columns:

  place      a PLAC value, exactly as written (required)
  standard   what the value becomes; empty keeps the value as written
  latitude   in GEDCOM form, such as N39.7817
  longitude  in GEDCOM form, such as W89.6501

Other columns are ignored, so a working list with notes can be passed as it
is. Cells are not quoted. A place is matched byte for byte, as 'gedcom list
plac' prints it: values that differ only in spacing or case are different
places, and an @@ in a 5.x file stays @@. The standard name is written as
given, and is not looked up in the table again. Empty lines are skipped.

A row gives both coordinates or neither. A row with one only, a coordinate
gedcom-go does not read as a latitude (N or S) or a longitude (E or W), an
empty place, or a place given twice is reported with its line, the exit code
is 1 and nothing is written.

Coordinates: a PLAC that has no MAP gets one, with LATI and LONG under it,
after the PLAC's other lines. A PLAC that already has a MAP keeps it, with its
LATI and LONG set to the table's: the table is taken as the authority. A row
with no coordinates leaves MAP as it is. A row whose standard name is the
place itself still sets the coordinates.

MAP is added only where the file's version permits it, as gedcom-go's grammar
check reads that version: not under a PLAC where the version defines none
(a place of publication under SOUR.PUBL, say), not in a GEDCOM 5.5 file, which
has no MAP, and not under a 5.5.1 SOUR.DATA.EVEN.PLAC. Such a PLAC still has
its value rewritten; the report gives its line.

Only PLAC lines in records are read: the header's PLAC, and extension tags
that hold a place (_PLAC), are left alone. The command only rewrites the value
and sets MAP: moving part of a place into the event's ADDR is an edit for
'gedcom put'.

The report gives the PLAC lines rewritten, the MAP structures added and
updated, the PLAC lines that could not take a MAP, the records changed and the
table's places that matched no PLAC. Every other line is written exactly as it
was read, so an empty table writes the file byte-identical.`,
		Example: `  gedcom places apply family.ged places.tsv -o out.ged
  # Start a table from the places in the file and the coordinates they have.
  gedcom list --format json family.ged plac |
    jq -r '"place\tstandard\tlatitude\tlongitude", (.[] | [.place, "", .latitude // "", .longitude // ""] | join("\t"))' > places.tsv
  gedcom places apply --format json family.ged places.tsv -o out.ged | jq .unmatched`,
		Args: exactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			format, err := outputFormat(cmd)
			if err != nil {
				return err
			}
			out, err := outputPath(cmd, args[0], args[1])
			if err != nil {
				return err
			}
			table, err := readPlaceTable(args[1])
			if err != nil {
				return err
			}
			res, err := decodeForWrite(args[0])
			if err != nil {
				return err
			}
			report := applyPlaces(res.Document, table)
			report.Input, report.Table, report.Output = args[0], args[1], out
			if err := writeOutput(cmd, out, res.Document); err != nil {
				return err
			}
			w := reportWriter(cmd, out)
			if format == formatJSON {
				return writeJSON(w, report)
			}
			return writePlacesText(w, report)
		},
	}
	addOutputFlag(cmd)
	return cmd
}

// readPlaceTable reads a place table, reporting every malformed row.
func readPlaceTable(name string) (placeTable, error) {
	table := placeTable{rows: map[string]placeTableRow{}}
	f, err := os.Open(filepath.Clean(name))
	if err != nil {
		return table, err
	}
	defer f.Close()

	var errs []string
	fail := func(n int, format string, a ...any) {
		errs = append(errs, fmt.Sprintf("%s line %d: %s", name, n, fmt.Sprintf(format, a...)))
	}
	var columns map[string]int
	scanner := bufio.NewScanner(f)
	for n := 1; scanner.Scan(); n++ {
		line := strings.TrimSuffix(scanner.Text(), "\r")
		if line == "" {
			continue
		}
		cells := strings.Split(line, "\t")
		if columns == nil {
			if columns, err = placeColumns(cells); err != nil {
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
		place := cell("place")
		row := placeTableRow{
			line:      n,
			standard:  cell("standard"),
			latitude:  strings.TrimSpace(cell("latitude")),
			longitude: strings.TrimSpace(cell("longitude")),
		}
		switch {
		case place == "":
			fail(n, "no place")
			continue
		case (row.latitude == "") != (row.longitude == ""):
			fail(n, "give both latitude and longitude, or neither")
			continue
		}
		if row.latitude != "" {
			if _, err := gedcom.ParseLatitude(row.latitude); err != nil {
				fail(n, "latitude %q: %v", row.latitude, err)
				continue
			}
			if _, err := gedcom.ParseLongitude(row.longitude); err != nil {
				fail(n, "longitude %q: %v", row.longitude, err)
				continue
			}
		}
		if first, ok := table.rows[place]; ok {
			fail(n, "place %q is already on line %d", place, first.line)
			continue
		}
		table.rows[place] = row
		table.places = append(table.places, place)
	}
	if err := scanner.Err(); err != nil {
		return table, err
	}
	if len(errs) > 0 {
		return table, errors.New(strings.Join(errs, "\n"))
	}
	return table, nil
}

// placeColumns maps the columns a place table's header names to their
// index. It needs place; latitude and longitude come together. Columns of
// other names are ignored, and may repeat.
func placeColumns(header []string) (map[string]int, error) {
	columns := map[string]int{}
	for i, name := range header {
		if !slices.Contains([]string{"place", "standard", "latitude", "longitude"}, name) {
			continue
		}
		if _, ok := columns[name]; ok {
			return nil, fmt.Errorf("column %q is named twice", name)
		}
		columns[name] = i
	}
	latitude, longitude := hasColumn(columns, "latitude"), hasColumn(columns, "longitude")
	switch {
	case !hasColumn(columns, "place"):
		return nil, errors.New(`want a header row naming a "place" column, and "standard", "latitude" and "longitude" as needed`)
	case latitude != longitude:
		return nil, errors.New(`want both a "latitude" and a "longitude" column, or neither`)
	}
	return columns, nil
}

func hasColumn(columns map[string]int, name string) bool {
	_, ok := columns[name]
	return ok
}

// addedMap is a MAP places apply inserted under a PLAC, kept until the
// grammar check says whether the version permits it there.
type addedMap struct {
	record *gedcom.Record
	tag    *gedcom.Tag
	plac   int // the PLAC's input line
}

// applyPlaces applies table to the PLAC lines of doc's records by editing
// their raw tags, the supported route for structures read from Tags.
func applyPlaces(doc *gedcom.Document, table placeTable) *placesReport {
	report := &placesReport{MapsNotPermitted: []int{}, Unmatched: []string{}}
	matched := map[string]bool{}
	changed := map[*gedcom.Record]bool{}
	var added []addedMap
	// An inserted MAP carries a line number past the input's last line until
	// the grammar check has run, so an issue about it can be told apart.
	marker := 0
	if doc.Layout != nil {
		marker = len(doc.Layout.Lines)
	}
	for _, record := range doc.Records {
		for i := 0; i < len(record.Tags); i++ {
			plac := record.Tags[i]
			if plac == nil || plac.Tag != "PLAC" {
				continue
			}
			row, ok := table.rows[plac.Value]
			if !ok {
				continue
			}
			matched[plac.Value] = true
			if row.standard != "" && row.standard != plac.Value {
				plac.Value = row.standard
				report.PlacesRewritten++
				changed[record] = true
			}
			if row.latitude == "" {
				continue
			}
			if m := childTag(record, plac, "MAP"); m != nil {
				lati := setChildValue(record, m, "LATI", row.latitude)
				long := setChildValue(record, m, "LONG", row.longitude)
				if lati || long {
					report.MapsUpdated++
					changed[record] = true
				}
				continue
			}
			marker++
			m := &gedcom.Tag{Level: plac.Level + 1, Tag: "MAP", LineNumber: marker}
			end := i + len(record.Subtree(plac))
			record.Tags = slices.Insert(record.Tags, end, m,
				&gedcom.Tag{Level: plac.Level + 2, Tag: "LATI", Value: row.latitude},
				&gedcom.Tag{Level: plac.Level + 2, Tag: "LONG", Value: row.longitude})
			added = append(added, addedMap{record: record, tag: m, plac: plac.LineNumber})
		}
	}

	if len(added) > 0 {
		refused := notPermittedLines(doc)
		for _, a := range added {
			if refused[a.tag.LineNumber] || (a.plac > 0 && refused[a.plac]) {
				i := slices.Index(a.record.Tags, a.tag)
				a.record.Tags = slices.Delete(a.record.Tags, i, i+len(a.record.Subtree(a.tag)))
				report.MapsNotPermitted = append(report.MapsNotPermitted, a.plac)
				continue
			}
			a.tag.LineNumber = 0
			report.MapsAdded++
			changed[a.record] = true
		}
	}

	report.RecordsChanged = len(changed)
	for _, place := range table.places {
		if !matched[place] {
			report.Unmatched = append(report.Unmatched, place)
		}
	}
	return report
}

// notPermittedLines is the set of lines holding a structure gedcom-go's
// grammar check finds the file's version does not permit where it stands.
func notPermittedLines(doc *gedcom.Document) map[int]bool {
	v := validator.NewWithConfig(&validator.ValidateOptions{
		CheckGrammar:           true,
		Strictness:             validator.StrictnessStrict,
		SkipDuplicateDetection: true,
	})
	lines := map[int]bool{}
	for _, issue := range v.ValidateAll(doc) {
		if issue.LineNumber > 0 && (issue.Code == validator.CodeUnexpectedSubstructure || issue.Code == validator.CodeVersionForeignStructure) {
			lines[issue.LineNumber] = true
		}
	}
	return lines
}

// childTag returns the first tag named name directly under parent in
// record, or nil.
func childTag(record *gedcom.Record, parent *gedcom.Tag, name string) *gedcom.Tag {
	for _, tag := range record.Subtree(parent)[1:] {
		if tag != nil && tag.Level == parent.Level+1 && tag.Tag == name {
			return tag
		}
	}
	return nil
}

// setChildValue sets the value of the first tag named name directly under
// parent in record, adding one after parent's other lines when there is
// none. It reports whether anything changed.
func setChildValue(record *gedcom.Record, parent *gedcom.Tag, name, value string) bool {
	if tag := childTag(record, parent, name); tag != nil {
		if tag.Value == value {
			return false
		}
		tag.Value = value
		return true
	}
	end := slices.Index(record.Tags, parent) + len(record.Subtree(parent))
	record.Tags = slices.Insert(record.Tags, end, &gedcom.Tag{Level: parent.Level + 1, Tag: name, Value: value})
	return true
}

// writePlacesText writes a summary line, then one line per PLAC that could not
// take a MAP and per table place that matched nothing.
func writePlacesText(w io.Writer, r *placesReport) error {
	var b strings.Builder
	fmt.Fprintf(&b, "wrote %s: %d PLAC rewritten, %d MAP added, %d MAP updated, %s changed\n",
		outputName(r.Output), r.PlacesRewritten, r.MapsAdded, r.MapsUpdated,
		plural(r.RecordsChanged, "record", "records"))
	for _, line := range r.MapsNotPermitted {
		fmt.Fprintf(&b, "  no MAP permitted\tline %d\n", line)
	}
	for _, place := range r.Unmatched {
		fmt.Fprintf(&b, "  unmatched\t%s\n", oneLine(place))
	}
	_, err := io.WriteString(w, b.String())
	return err
}
