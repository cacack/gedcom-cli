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

// listTypes are the record types list accepts, as the user writes them.
var listTypes = []string{"indi", "fam", "sour", "repo", "obje"}

// row is one listed record. Its JSON form is the row struct itself; fields
// gives the same values, in the same order, for text output.
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

func (r indiRow) fields() []string {
	return []string{r.XRef, r.Name, r.Birth, r.Death, strconv.FormatBool(r.Living)}
}
func (r famRow) fields() []string {
	return []string{r.XRef, r.Husband, r.Wife, r.Marriage, strconv.Itoa(r.Children)}
}
func (r sourRow) fields() []string { return []string{r.XRef, r.Title, r.Author} }
func (r repoRow) fields() []string { return []string{r.XRef, r.Name} }
func (r objeRow) fields() []string { return []string{r.XRef, r.Title, r.File, r.Format} }

func newListCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list <file> <type>",
		Short: "List the records of one type, one per line: indi, fam, sour, repo or obje",
		Long: `List the records of one type, one per line, in file order.

Text output is tab-separated with no header. The fields, which are also the
JSON keys, are:

  indi  xref, name, birth, death, living (true or false)
  fam   xref, husband, wife, marriage, children (a count)
  sour  xref, title, author
  repo  xref, name
  obje  xref, title, file, format (of the record's first file)

Dates are printed as written in the file. An absent value is empty. In text
output, control characters in a value (a line break in a multi-line title, a
tab) are printed as spaces; JSON keeps the value as it is.

For obje, title and format are those of the first FILE. A record that puts
TITL and FORM beside FILE instead of under it (the GEDCOM 5.5 layout) lists
them empty; 'gedcom get' shows the whole record.

living is whether gedcom-go presumes the person living: no death, burial or
cremation, and no date of theirs, of a family they are a spouse in or of
their children, more than --living-years before --reference-year. A person
with no dates at all is presumed living.`,
		Example: `  gedcom list family.ged indi
  gedcom list --limit 20 family.ged fam
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
				return usageError{fmt.Errorf("unknown record type %q: want one of %s", args[1], strings.Join(listTypes, ", "))}
			}
			doc, living, err := openDocument(cmd, args[0])
			if err != nil {
				return err
			}
			rows := listRows(doc, living, kind, limit)
			if format == formatJSON {
				return writeJSON(cmd.OutOrStdout(), rows)
			}
			return writeRowsText(cmd.OutOrStdout(), rows)
		},
	}
	cmd.Flags().Int("limit", 0, "print at most this many records; 0 means no limit")
	return cmd
}

// listRows builds the rows for kind, which must be one of listTypes.
// living is the set of individuals presumed living.
func listRows(doc *gedcom.Document, living map[*gedcom.Individual]bool, kind string, limit int) []row {
	switch kind {
	case "indi":
		return rowsOf(doc.Individuals(), limit, func(i *gedcom.Individual) row { return indiRowOf(i, living[i]) })
	case "fam":
		return rowsOf(doc.Families(), limit, newFamRow)
	case "sour":
		return rowsOf(doc.Sources(), limit, newSourRow)
	case "repo":
		return rowsOf(doc.Repositories(), limit, newRepoRow)
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
