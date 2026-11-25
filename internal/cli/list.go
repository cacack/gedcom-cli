package cli

import (
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"
	"unicode"

	"github.com/cacack/gedcom-go/v3/gedcom"
	"github.com/spf13/cobra"
)

// listTypes are the types list accepts, as the user writes them: the record
// types, names for the individuals' NAME lines, plac for the distinct places,
// tags for the tag paths and values for the distinct values of one tag path.
var listTypes = []string{"indi", "fam", "sour", "repo", "obje", "subm", "note", "snote", "names", "plac", "tags", "values"}

// listOptions are list's flags beyond --format: limit for every type, vendor
// for tags, path, the --tag of values split at its dots, and repeated for
// names.
type listOptions struct {
	limit    int
	vendor   bool
	path     []string
	repeated bool
}

// row is one listed record, place, tag path or value. Its JSON form is the
// row struct itself; fields gives the same values, in the same order, for text
// output. placRow alone has JSON keys, its coordinates, that text output
// leaves out.
type row interface{ fields() []string }

type indiRow struct {
	XRef   string `json:"xref"`
	Name   string `json:"name"`
	Birth  string `json:"birth"`
	Death  string `json:"death"`
	Living bool   `json:"living"`
}

type famRow struct {
	XRef     string `json:"xref"`
	Husband  string `json:"husband"`
	Wife     string `json:"wife"`
	Marriage string `json:"marriage"`
	Children int    `json:"children"`
}

type sourRow struct {
	XRef   string `json:"xref"`
	Title  string `json:"title"`
	Author string `json:"author"`
}

type repoRow struct {
	XRef string `json:"xref"`
	Name string `json:"name"`
}

type objeRow struct {
	XRef   string `json:"xref"`
	Title  string `json:"title"`
	File   string `json:"file"`
	Format string `json:"format"`
}

type submRow struct {
	XRef string `json:"xref"`
	Name string `json:"name"`
}

// noteRow is a shared note record: a 5.x NOTE or a 7.0 SNOTE.
type noteRow struct {
	XRef string `json:"xref"`
	Text string `json:"text"`
}

// nameRow is one NAME line of an individual. Position counts the person's
// names from 1, in file order. The parts are the values of the lines under
// the NAME, as written. SameAs is the position of the person's first earlier
// name that nameKey makes equal to this one, or 0.
type nameRow struct {
	XRef      string `json:"xref"`
	Position  int    `json:"position"`
	Value     string `json:"value"`
	Given     string `json:"givn"`
	Surname   string `json:"surn"`
	Suffix    string `json:"nsfx"`
	Nickname  string `json:"nick"`
	Type      string `json:"type"`
	Citations int    `json:"citations"`
	SameAs    int    `json:"same_as"`
}

// placRow is one distinct PLAC value. The coordinates are those of the MAP
// under a use of it, as written in the file; they are in JSON only.
type placRow struct {
	Count     int    `json:"count"`
	Place     string `json:"place"`
	Latitude  string `json:"latitude,omitempty"`
	Longitude string `json:"longitude,omitempty"`
}

// tagRow is one tag path, such as INDI.BIRT.DATE, with its number of lines.
type tagRow struct {
	Count int    `json:"count"`
	Path  string `json:"path"`
}

// valueRow is one distinct value of a tag path.
type valueRow struct {
	Count int    `json:"count"`
	Value string `json:"value"`
}

func (r indiRow) fields() []string {
	return []string{r.XRef, r.Name, r.Birth, r.Death, strconv.FormatBool(r.Living)}
}
func (r famRow) fields() []string {
	return []string{r.XRef, r.Husband, r.Wife, r.Marriage, strconv.Itoa(r.Children)}
}
func (r nameRow) fields() []string {
	return []string{r.XRef, strconv.Itoa(r.Position), r.Value, r.Given, r.Surname, r.Suffix, r.Nickname, r.Type,
		strconv.Itoa(r.Citations), strconv.Itoa(r.SameAs)}
}
func (r sourRow) fields() []string  { return []string{r.XRef, r.Title, r.Author} }
func (r repoRow) fields() []string  { return []string{r.XRef, r.Name} }
func (r objeRow) fields() []string  { return []string{r.XRef, r.Title, r.File, r.Format} }
func (r submRow) fields() []string  { return []string{r.XRef, r.Name} }
func (r noteRow) fields() []string  { return []string{r.XRef, r.Text} }
func (r placRow) fields() []string  { return []string{strconv.Itoa(r.Count), r.Place} }
func (r tagRow) fields() []string   { return []string{strconv.Itoa(r.Count), r.Path} }
func (r valueRow) fields() []string { return []string{strconv.Itoa(r.Count), r.Value} }

func newListCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list <file> <type>",
		Short: "List the records of one type (indi, fam, sour, repo, obje, subm, note, snote), the people's names (names), the places (plac), the tag paths (tags) or one tag's values (values), one per line",
		Long: `List the records of one type, one per line, in file order; or, for names,
each NAME line of each individual, in file order; or, for plac, tags and
values, the distinct places, tag paths or values of one tag path in
the file, most used first.

Text output is tab-separated with no header. The fields, which are also the
JSON keys, are:

  indi   xref, name, birth, death, living (true or false)
  fam    xref, husband, wife, marriage, children (a count)
  sour   xref, title, author
  repo   xref, name
  obje   xref, title, file, format (of the record's first file)
  subm   xref, name
  note   xref, text (shared NOTE records, GEDCOM 5.x)
  snote  xref, text (shared SNOTE records, GEDCOM 7.0)
  names  xref, position, value, givn, surn, nsfx, nick, type, citations,
         same_as
  plac   count, place (JSON adds latitude and longitude)
  tags   count, path
  values count, value (of the tag path --tag names)

These cover every level-0 record type but SUBN (the GEDCOM 5.5 submission
record) and extension records (tags starting with _); 'gedcom get' prints
any record by xref.

Dates are printed as written in the file. An absent value is empty. In text
output, control characters in a value (a line break in a multi-line title, a
tab) are printed as spaces; JSON keeps the value as it is.

For obje, title and format are those of the first FILE. A record that puts
TITL and FORM beside FILE instead of under it (the GEDCOM 5.5 layout) lists
them empty; 'gedcom get' shows the whole record.

For names, each NAME line of an individual is a row, so a person with three
names has three rows, in file order. position is the name's place among the
person's names, from 1. value is the NAME value; givn, surn, nsfx, nick and
type are the values of the GIVN, SURN, NSFX, NICK and TYPE lines under it,
as written, and empty when the line is absent (the first, if repeated).
citations is the number of SOUR lines under it. same_as is the position of
the person's first earlier name equal to this one, or 0: two names are equal
when their values match after ignoring case and removing every space and
punctuation character (the surname slashes, commas and periods among them),
so "John /Smith/ Jr." equals "john smith jr". --repeated lists only the names
with a same_as, the names to fold into an earlier one; on a file with none it
prints nothing. A person --redact-living hides has one name, Living.

For plac, each distinct PLAC value in the file's records is listed once,
exactly as written, with how many times it is used: values that differ only
in spacing or case are separate rows, and an @@ in a 5.x file stays @@. A
PLAC line with no value, or only whitespace, is not listed. The order is
count, highest first, then place in byte order; --limit applies after
sorting. JSON adds latitude and longitude, as written, when a use of the
place has a MAP giving both: those of the first such use, so uses with
other coordinates are not shown. With --redact-living, uses in hidden
records are not counted, so a place used only there is left out.

For tags, each tag path in the file's records is listed once with how many
lines it has. A path is the record's type and the tags down to the line,
joined by dots (INDI.BIRT.DATE), and CONT and CONC lines have paths of their
own (INDI.NOTE.CONT). The counts add up to the file's lines less the header,
the trailer and each record's level-0 line; a line gedcom-go cannot read is
not counted either. --vendor lists only the paths with an extension tag (one
starting with _) in them, such as INDI._MILT and INDI._MILT.DATE.

For values, --tag names one path as tags prints it, below the record level
(INDI.SEX, not INDI), and each distinct value of a line at that path is
listed once, exactly as written, with how many lines have it. Tag names
compare exactly, case included. A value is its line's own: the CONT and CONC
lines that continue it are not part of it. A line with no value, or only
whitespace, is not listed.

tags and values sort and limit as plac does. With --redact-living, the lines
of hidden records are not counted, so a path or value found only there is
left out.

living is whether gedcom-go presumes the person living: no death, burial or
cremation, and no date of theirs, of a family they are a spouse in or of
their children, more than --living-years before --reference-year. A person
with no dates at all is presumed living.`,
		Example: `  gedcom list family.ged indi
  gedcom list --limit 20 family.ged fam
  gedcom list family.ged names
  gedcom list --repeated family.ged names
  gedcom list family.ged plac
  gedcom list --vendor family.ged tags
  gedcom list --tag INDI.EVEN.TYPE family.ged values
  gedcom list --format json family.ged indi | jq '.[] | select(.name | test("/Example/"))'
  gedcom list --format json family.ged indi | jq '.[] | select(.living | not)'`,
		Args: exactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			format, err := outputFormat(cmd)
			if err != nil {
				return err
			}
			limit, err := cmd.Flags().GetInt("limit")
			if err != nil {
				return err
			}
			if limit < 0 {
				return usageError{fmt.Errorf("invalid --limit %d: want 0 (no limit) or more", limit)}
			}
			kind := strings.ToLower(args[1])
			if !slices.Contains(listTypes, kind) {
				return usageError{fmt.Errorf("unknown type %q: want one of %s", args[1], strings.Join(listTypes, ", "))}
			}
			opts := listOptions{limit: limit}
			if opts.vendor, err = cmd.Flags().GetBool("vendor"); err != nil {
				return err
			}
			if opts.vendor && kind != "tags" {
				return usageError{fmt.Errorf("--vendor applies only to tags, not %s", kind)}
			}
			if opts.repeated, err = cmd.Flags().GetBool("repeated"); err != nil {
				return err
			}
			if opts.repeated && kind != "names" {
				return usageError{fmt.Errorf("--repeated applies only to names, not %s", kind)}
			}
			tag, err := cmd.Flags().GetString("tag")
			if err != nil {
				return err
			}
			switch {
			case kind == "values" && tag == "":
				return usageError{fmt.Errorf("values needs --tag, a tag path such as INDI.SEX")}
			case kind != "values" && tag != "":
				return usageError{fmt.Errorf("--tag applies only to values, not %s", kind)}
			case tag != "":
				opts.path = strings.Split(tag, ".")
				if len(opts.path) < 2 || slices.Contains(opts.path, "") {
					return usageError{fmt.Errorf("invalid --tag %q: want a record type and the tags below it, joined by dots, such as INDI.SEX", tag)}
				}
			}
			doc, living, err := openDocument(cmd, args[0])
			if err != nil {
				return err
			}
			rows := listRows(doc, living, kind, opts)
			if format == formatJSON {
				return writeJSON(cmd.OutOrStdout(), rows)
			}
			return writeRowsText(cmd.OutOrStdout(), rows)
		},
	}
	cmd.Flags().Int("limit", 0, "print at most this many rows; 0 means no limit")
	cmd.Flags().Bool("vendor", false, "for tags, list only the paths with an extension tag (starting with _)")
	cmd.Flags().Bool("repeated", false, "for names, list only the names equal to an earlier name of the same person")
	cmd.Flags().String("tag", "", "for values, the tag path whose values to list, such as INDI.SEX")
	return cmd
}

// listRows builds the rows for kind, which must be one of listTypes.
// living is the set of individuals presumed living.
func listRows(doc *gedcom.Document, living map[*gedcom.Individual]bool, kind string, opts listOptions) []row {
	limit := opts.limit
	switch kind {
	case "indi":
		return rowsOf(doc.Individuals(), limit, func(i *gedcom.Individual) row { return indiRowOf(i, living[i]) })
	case "fam":
		return rowsOf(doc.Families(), limit, newFamRow)
	case "sour":
		return rowsOf(doc.Sources(), limit, newSourRow)
	case "repo":
		return rowsOf(doc.Repositories(), limit, newRepoRow)
	case "subm":
		return rowsOf(doc.Submitters(), limit, newSubmRow)
	case "note":
		return rowsOf(doc.Notes(), limit, newNoteRow)
	case "snote":
		return rowsOf(doc.SharedNotes(), limit, newSharedNoteRow)
	case "names":
		return nameRows(doc, opts.repeated, limit)
	case "plac":
		return placeRows(doc, limit)
	case "tags":
		return tagRows(doc, opts.vendor, limit)
	case "values":
		return valueRows(doc, opts.path, limit)
	default:
		return rowsOf(doc.MediaObjects(), limit, newObjeRow)
	}
}

// rowsOf maps at most limit entities to rows; a limit of 0 means all of them.
// The result is never nil, so that an empty list is [] in JSON.
func rowsOf[T any](entities []T, limit int, toRow func(T) row) []row {
	if limit > 0 && len(entities) > limit {
		entities = entities[:limit]
	}
	rows := make([]row, 0, len(entities))
	for _, entity := range entities {
		rows = append(rows, toRow(entity))
	}
	return rows
}

// nameRows lists each NAME line of each individual in doc, in file order, and
// returns at most limit of them; a limit of 0 means all of them. With
// repeated, only the names with a same_as are listed. The value and citations
// are gedcom-go's; the parts are read from the lines under the NAME, because
// the library fills a missing GIVN, SURN or NSFX from the value.
func nameRows(doc *gedcom.Document, repeated bool, limit int) []row {
	var rows []row
	for _, record := range doc.Records {
		ind, ok := record.GetIndividual()
		if !ok {
			continue
		}
		lines := record.FindAll("NAME")
		keys := make([]string, len(ind.Names))
		for i, name := range ind.Names {
			if name == nil {
				continue
			}
			keys[i] = nameKey(name.Full)
			r := nameRow{XRef: ind.XRef, Position: i + 1, Value: name.Full, Citations: len(name.SourceCitations),
				SameAs: slices.Index(keys[:i], keys[i]) + 1}
			if repeated && r.SameAs == 0 {
				continue
			}
			// A redacted record has no lines, so its name has no parts.
			if len(lines) == len(ind.Names) {
				r.Given, r.Surname, r.Suffix = namePart(record, lines[i], "GIVN"), namePart(record, lines[i], "SURN"), namePart(record, lines[i], "NSFX")
				r.Nickname, r.Type = namePart(record, lines[i], "NICK"), namePart(record, lines[i], "TYPE")
			}
			rows = append(rows, r)
		}
	}
	return rowsOf(rows, limit, func(r row) row { return r })
}

// namePart is the value of the first line tagged tag directly under name, a
// NAME line of record, or "" when it has none.
func namePart(record *gedcom.Record, name *gedcom.Tag, tag string) string {
	for _, line := range record.Subtree(name) {
		if line != nil && line.Level == name.Level+1 && line.Tag == tag {
			return line.Value
		}
	}
	return ""
}

// nameKey is a name value as names compare for same_as: in lower case, with
// every space and punctuation character removed.
func nameKey(value string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) || unicode.IsPunct(r) {
			return -1
		}
		return unicode.ToLower(r)
	}, value)
}

// placeRows counts the uses of each distinct PLAC value in doc's records and
// returns at most limit of them, most used first and then in byte order; a
// limit of 0 means all of them. gedcom-go has no call that enumerates places,
// so this walks the raw lines, as media check does for FILE.
func placeRows(doc *gedcom.Document, limit int) []row {
	counts := map[string]int{}
	coordinates := map[string]placRow{}
	for _, record := range doc.Records {
		for i, tag := range record.Tags {
			if tag == nil || tag.Tag != "PLAC" || strings.TrimSpace(tag.Value) == "" {
				continue
			}
			counts[tag.Value]++
			if _, ok := coordinates[tag.Value]; !ok {
				if latitude, longitude := placeCoordinates(record.Tags[i:]); latitude != "" && longitude != "" {
					coordinates[tag.Value] = placRow{Latitude: latitude, Longitude: longitude}
				}
			}
		}
	}
	return countedRows(counts, limit, func(place string, count int) row {
		p := coordinates[place]
		p.Count, p.Place = count, place
		return p
	})
}

// placeCoordinates returns the LATI and LONG of the MAP under tags[0], a PLAC
// line followed by the rest of its record, or "" for each one absent.
func placeCoordinates(tags []*gedcom.Tag) (latitude, longitude string) {
	plac := tags[0].Level
	inMap := false
	for _, tag := range tags[1:] {
		if tag == nil {
			continue
		}
		if tag.Level <= plac {
			break
		}
		switch {
		case tag.Level == plac+1:
			inMap = tag.Tag == "MAP"
		case inMap && tag.Level == plac+2 && tag.Tag == "LATI" && latitude == "":
			latitude = tag.Value
		case inMap && tag.Level == plac+2 && tag.Tag == "LONG" && longitude == "":
			longitude = tag.Value
		}
	}
	return latitude, longitude
}

// tagRows counts the lines at each tag path in doc's records and returns at
// most limit of them, most used first and then in byte order; a limit of 0
// means all of them. With vendor, only paths holding an extension tag are
// counted. A line's path is its record's type, the tags of the lines it sits
// under (by Level) and its own tag. gedcom-go has no call that enumerates tag
// paths, so this walks the raw lines, as placeRows does.
func tagRows(doc *gedcom.Document, vendor bool, limit int) []row {
	counts := map[string]int{}
	for _, record := range doc.Records {
		path := []string{string(record.Type)}
		for _, tag := range record.Tags {
			if tag == nil {
				continue
			}
			path = append(path[:min(max(tag.Level, 1), len(path))], tag.Tag)
			if vendor && !slices.ContainsFunc(path, func(t string) bool { return strings.HasPrefix(t, "_") }) {
				continue
			}
			counts[strings.Join(path, ".")]++
		}
	}
	return countedRows(counts, limit, func(path string, count int) row { return tagRow{Count: count, Path: path} })
}

// valueRows counts the uses of each distinct value at path, a record type and
// the tags below it, in doc's records, and returns at most limit of them as
// tagRows does. A line with no value, or only whitespace, is not counted.
func valueRows(doc *gedcom.Document, path []string, limit int) []row {
	counts := map[string]int{}
	for _, record := range doc.Records {
		if string(record.Type) != path[0] {
			continue
		}
		for _, tag := range record.FindAll(path[1:]...) {
			if strings.TrimSpace(tag.Value) != "" {
				counts[tag.Value]++
			}
		}
	}
	return countedRows(counts, limit, func(value string, count int) row { return valueRow{Count: count, Value: value} })
}

// countedRows returns at most limit of counts as rows, count highest first and
// then key in byte order; a limit of 0 means all of them.
func countedRows(counts map[string]int, limit int, toRow func(key string, count int) row) []row {
	keys := make([]string, 0, len(counts))
	for key := range counts {
		keys = append(keys, key)
	}
	slices.SortFunc(keys, func(a, b string) int {
		if counts[a] != counts[b] {
			return counts[b] - counts[a]
		}
		return strings.Compare(a, b)
	})
	return rowsOf(keys, limit, func(key string) row { return toRow(key, counts[key]) })
}

// indiRowOf is an individual as list indi prints it; living is whether it is
// presumed living.
func indiRowOf(i *gedcom.Individual, living bool) indiRow {
	return indiRow{
		XRef:   i.XRef,
		Name:   i.FullName(),
		Birth:  eventDate(i.BirthEvent()),
		Death:  eventDate(i.DeathEvent()),
		Living: living,
	}
}

func newFamRow(f *gedcom.Family) row {
	return famRow{
		XRef:     f.XRef,
		Husband:  f.Husband,
		Wife:     f.Wife,
		Marriage: eventDate(f.MarriageEvent()),
		Children: len(f.Children),
	}
}

func newSourRow(s *gedcom.Source) row {
	return sourRow{XRef: s.XRef, Title: s.Title, Author: s.Author}
}

func newRepoRow(r *gedcom.Repository) row {
	return repoRow{XRef: r.XRef, Name: r.Name}
}

func newSubmRow(s *gedcom.Submitter) row {
	return submRow{XRef: s.XRef, Name: s.Name}
}

func newNoteRow(n *gedcom.Note) row {
	return noteRow{XRef: n.XRef, Text: n.Text}
}

func newSharedNoteRow(n *gedcom.SharedNote) row {
	return noteRow{XRef: n.XRef, Text: n.Text}
}

func newObjeRow(o *gedcom.MediaObject) row {
	r := objeRow{XRef: o.XRef}
	if len(o.Files) > 0 && o.Files[0] != nil {
		r.Title, r.File, r.Format = o.Files[0].Title, o.Files[0].FileRef, o.Files[0].Form
	}
	return r
}

// eventDate is the event's date as written in the file, or "" when the record
// has no such event.
func eventDate(e *gedcom.Event) string {
	if e == nil {
		return ""
	}
	return e.Date
}

// oneLine replaces each control character in a value with a space. It keeps a
// multi-line value, such as a source title with continuation lines, from
// breaking the one-record-per-line text format, and keeps escape sequences in
// a file from reaching the terminal.
func oneLine(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, s)
}

func writeRowsText(w io.Writer, rows []row) error {
	for _, r := range rows {
		fields := r.fields()
		for i, field := range fields {
			fields[i] = oneLine(field)
		}
		if _, err := fmt.Fprintln(w, strings.Join(fields, "\t")); err != nil {
			return err
		}
	}
	return nil
}
