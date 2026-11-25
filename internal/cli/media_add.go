package cli

import (
	"errors"
	"fmt"
	"mime"
	"path"
	"strconv"
	"strings"

	"github.com/cacack/gedcom-go/v3/encoder"
	"github.com/cacack/gedcom-go/v3/gedcom"
	"github.com/spf13/cobra"
)

// Actions media add reports for a row.
const (
	actionCreated   = "created"   // a new OBJE record, linked from the record
	actionLinked    = "linked"    // an existing OBJE record, newly linked
	actionUnchanged = "unchanged" // the record already linked the OBJE record
)

// mediaRow is one requested link: a file to attach to a record.
type mediaRow struct {
	line  int // in --batch, 0 for --to/--path
	to    string
	path  string
	title string
}

// mediaAdded is what media add did for one row.
type mediaAdded struct {
	To     string `json:"to"`
	Object string `json:"object"`
	Path   string `json:"path"`
	Action string `json:"action"`
}

func newMediaAddCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "add <file> (--to <xref> --path <path> | --batch <tsv>) -o <out>",
		Short: "Link a media file from an individual, family or source, writing a new file",
		Long: `Attach a media file to an individual, family or source record, and write the
result to --output, or to stdout with -o - (the report then goes to stderr).
The input file is never changed.

The file is linked through an OBJE record: the first one with a FILE of that
path is reused, otherwise a new one is created (FILE, with FORM and TITL
under it, as the encoder writes them for the file's version) with the first
free xref of the form @O1@ that no record already points at. The record then
gets an OBJE line pointing at it, unless it already has one. Running the same command again changes nothing.

--form sets the FORM. Without it, the FORM is the file's extension for a 5.x
file (jpg, pdf) and its media type for a 7.0 file (image/jpeg).

With --root, the path must be relative, stay inside --root and name a file
there. The path is written as given, with \ turned into /.

--batch reads rows from a tab-separated file instead of --to and --path:
xref, path and an optional title. Blank lines and lines starting with # are
skipped. Every row is applied or none is: any row that names an unknown
record or a path outside --root is reported, by line, and nothing is written.

Text output is one tab-separated line per row: xref, object, action, path.
The action is "created" (a new OBJE record), "linked" (an existing one, newly
linked) or "unchanged" (already linked).

Records not involved are written exactly as they were read.`,
		Example: `  gedcom media add family.ged --to @I1@ --path photos/ada.jpg --title "Ada, 1920" -o out.ged
  gedcom media add family.ged --root media --batch links.tsv -o out.ged`,
		Args: exactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			format, err := outputFormat(cmd)
			if err != nil {
				return err
			}
			flags := cmd.Flags()
			to, _ := flags.GetString("to")
			mediaPath, _ := flags.GetString("path")
			title, _ := flags.GetString("title")
			form, _ := flags.GetString("form")
			batch, _ := flags.GetString("batch")
			root, _ := flags.GetString("root")

			out, err := outputPath(cmd, args[0])
			if err != nil {
				return err
			}
			var rows []mediaRow
			switch {
			case batch != "" && (to != "" || mediaPath != "" || title != ""):
				return usageError{errors.New("--batch cannot be combined with --to, --path or --title")}
			case batch != "":
				if rows, err = readMediaBatch(batch); err != nil {
					return err
				}
			case to == "" || mediaPath == "":
				return usageError{errors.New("give --to and --path, or --batch")}
			default:
				rows = []mediaRow{{to: to, path: mediaPath, title: title}}
			}

			res, err := decodeForWrite(args[0])
			if err != nil {
				return err
			}
			doc := res.Document
			added, err := addMedia(doc, rows, form, root)
			if err != nil {
				return err
			}
			if err := encoder.SyncTags(doc); err != nil {
				return err
			}
			if err := writeOutput(cmd, out, doc); err != nil {
				return err
			}
			w := reportWriter(cmd, out)
			if format == formatJSON {
				return writeJSON(w, added)
			}
			for _, a := range added {
				if _, err := fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", a.To, a.Object, a.Action, oneLine(a.Path)); err != nil {
					return err
				}
			}
			return nil
		},
	}
	addOutputFlag(cmd)
	cmd.Flags().String("to", "", "xref of the individual, family or source to link the media from")
	cmd.Flags().String("path", "", "the media file's path, as FILE will hold it")
	cmd.Flags().String("title", "", "title of a new OBJE record's file (TITL)")
	cmd.Flags().String("form", "", "FORM of a new OBJE record's file; default from the extension")
	cmd.Flags().String("batch", "", "tab-separated file of rows: xref, path, optional title")
	cmd.Flags().String("root", "", "media folder each path must name a file in")
	return cmd
}

// readMediaBatch reads the rows of a --batch file.
func readMediaBatch(name string) ([]mediaRow, error) {
	batch, err := readBatch(name, 2, 3, "xref, path and an optional title")
	if err != nil {
		return nil, err
	}
	rows := make([]mediaRow, 0, len(batch))
	for _, b := range batch {
		row := mediaRow{line: b.line, to: b.fields[0], path: b.fields[1]}
		if len(b.fields) == 3 {
			row.title = b.fields[2]
		}
		rows = append(rows, row)
	}
	return rows, nil
}

// rowError prefixes a row's error with where the row came from.
func rowError(row mediaRow, format string, a ...any) string {
	msg := fmt.Sprintf(format, a...)
	if row.line > 0 {
		return fmt.Sprintf("line %d: %s", row.line, msg)
	}
	return msg
}

// addMedia checks every row, then applies them all to doc's typed entities.
// When any row fails, doc is left unchanged and every failure is returned.
func addMedia(doc *gedcom.Document, rows []mediaRow, form, root string) ([]mediaAdded, error) {
	type target struct {
		record *gedcom.Record
		media  *[]*gedcom.MediaLink
		form   string
	}
	targets := make([]target, len(rows))
	var errs []string
	for i, row := range rows {
		records, err := findRecords(doc, []string{row.to})
		if err != nil {
			errs = append(errs, rowError(row, "%v", err))
			continue
		}
		record := records[0]
		media := mediaLinks(record)
		if media == nil {
			errs = append(errs, rowError(row, "%s is a %s record; want an individual, family or source", record.XRef, record.Type))
			continue
		}
		if msg := checkMediaPath(row.path, root); msg != "" {
			errs = append(errs, rowError(row, "%s", msg))
			continue
		}
		rowForm := form
		if rowForm == "" {
			rowForm = defaultForm(doc, row.path)
		}
		if rowForm == "" {
			errs = append(errs, rowError(row, "cannot tell the FORM of %s from its extension; pass --form", row.path))
			continue
		}
		targets[i] = target{record: record, media: media, form: rowForm}
	}
	if len(errs) > 0 {
		return nil, errors.New(strings.Join(errs, "\n"))
	}

	pointed := pointedXRefs(doc)
	added := make([]mediaAdded, 0, len(rows))
	for i, row := range rows {
		t := targets[i]
		filePath := strings.ReplaceAll(row.path, `\`, "/")
		result := mediaAdded{To: t.record.XRef, Path: filePath, Action: actionLinked}
		object := findMediaObject(doc, filePath)
		if object == nil {
			object = &gedcom.MediaObject{
				XRef:  freeXRef(doc, pointed, "O"),
				Files: []*gedcom.MediaFile{{FileRef: filePath, Form: t.form, Title: row.title}},
			}
			if err := doc.AddRecord(&gedcom.Record{XRef: object.XRef, Type: gedcom.RecordTypeMedia, Entity: object}); err != nil {
				return nil, err
			}
			result.Action = actionCreated
		}
		result.Object = object.XRef
		if hasMediaLink(*t.media, object.XRef) {
			result.Action = actionUnchanged
		} else {
			*t.media = append(*t.media, &gedcom.MediaLink{MediaXRef: object.XRef})
		}
		added = append(added, result)
	}
	return added, nil
}

// mediaLinks returns the media links of an individual, family or source
// record, or nil for any other record.
func mediaLinks(record *gedcom.Record) *[]*gedcom.MediaLink {
	if indi, ok := record.GetIndividual(); ok {
		return &indi.Media
	}
	if fam, ok := record.GetFamily(); ok {
		return &fam.Media
	}
	if sour, ok := record.GetSource(); ok {
		return &sour.Media
	}
	return nil
}

// checkMediaPath returns why p cannot be added, or "" when it can. With a
// root, p must be relative, stay inside root and name a file there.
func checkMediaPath(p, root string) string {
	if strings.TrimSpace(p) == "" {
		return "empty path"
	}
	if root == "" {
		return ""
	}
	if urlRef.MatchString(p) || absoluteRef.MatchString(p) {
		return fmt.Sprintf("path %s is outside --root %s: want a path relative to it", p, root)
	}
	rel := normalizeMediaPath(p)
	if escapesRoot(rel) {
		return fmt.Sprintf("path %s is outside --root %s", p, root)
	}
	if statMediaFile(root, rel) == nil {
		return fmt.Sprintf("no file %s under --root %s", p, root)
	}
	return ""
}

// defaultForm is the FORM for a file at p when --form is not given: its media
// type for a 7.0 document, its lowercased extension otherwise.
func defaultForm(doc *gedcom.Document, p string) string {
	ext := strings.ToLower(path.Ext(strings.ReplaceAll(p, `\`, "/")))
	if ext == "" {
		return ""
	}
	if doc.Header != nil && doc.Header.Version == gedcom.Version70 {
		mediaType, _, _ := mime.ParseMediaType(mime.TypeByExtension(ext))
		return mediaType
	}
	return strings.TrimPrefix(ext, ".")
}

// findMediaObject returns the first OBJE record with a FILE of path p.
func findMediaObject(doc *gedcom.Document, p string) *gedcom.MediaObject {
	want := normalizeMediaPath(p)
	for _, object := range doc.MediaObjects() {
		for _, file := range object.Files {
			if file != nil && normalizeMediaPath(file.FileRef) == want {
				return object
			}
		}
	}
	return nil
}

func hasMediaLink(links []*gedcom.MediaLink, xref string) bool {
	for _, link := range links {
		if link != nil && link.MediaXRef == xref {
			return true
		}
	}
	return false
}

// pointedXRefs is the set of xrefs that doc's records point at, defined or not.
func pointedXRefs(doc *gedcom.Document) map[string]bool {
	var version gedcom.Version
	if doc.Header != nil {
		version = doc.Header.DeclaredVersion
	}
	pointed := map[string]bool{}
	for _, record := range doc.Records {
		gedcom.VisitForVersion(record, version, func(xref string) { pointed[xref] = true })
	}
	return pointed
}

// freeXRef returns the first xref @<prefix>N@, from N = 1, that doc does not
// define and that is not in pointed, so a new record never picks up a
// dangling pointer meant for a record that no longer exists.
func freeXRef(doc *gedcom.Document, pointed map[string]bool, prefix string) string {
	for n := 1; ; n++ {
		xref := "@" + prefix + strconv.Itoa(n) + "@"
		if doc.GetRecord(xref) == nil && !pointed[xref] {
			return xref
		}
	}
}
