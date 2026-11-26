package cli

import (
	"errors"
	"fmt"
	"strings"

	"github.com/cacack/gedcom-go/v3/encoder"
	"github.com/cacack/gedcom-go/v3/gedcom"
	"github.com/spf13/cobra"
)

// Actions media set reports for a row, beside actionUnchanged.
const (
	actionSet      = "set"      // an empty FILE now holds the path
	actionReplaced = "replaced" // a FILE with another value, replaced with --replace
)

// mediaSetRow is one requested edit: the FILE, and optionally FORM, of an
// OBJE record.
type mediaSetRow struct {
	line   int // in --batch, 0 for --obje/--path
	object string
	path   string
	form   string
}

// mediaSetResult is what media set did for one row.
type mediaSetResult struct {
	Object string `json:"object"`
	Path   string `json:"path"`
	Action string `json:"action"`
}

func newMediaSetCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "set <file> (--obje <xref> --path <path> | --batch <tsv>) -o <out>",
		Short: "Set the FILE of an existing OBJE record, writing a new file",
		Long: `Set the FILE, and optionally the FORM, of an OBJE record that already exists,
and write the result to --output, or to stdout with -o - (the report then goes
to stderr). The input file is never changed. The record keeps its xref, its
other lines and every link to it.

A record whose FILE is empty takes the path. A record whose FILE already
holds that path is left as it is. A record whose FILE holds another path is
refused unless --replace is given. A record with more than one FILE is
refused: say which file is meant by editing it with get and put.

--form sets the FORM as well; without it the FORM is left as it is.

With --root, the path must be relative, stay inside --root and name a file
there. The path is written as given, with \ turned into /.

--batch reads rows from a tab-separated file instead of --obje and --path:
xref, path and an optional FORM. Blank lines and lines starting with # are
skipped. Two records may be given the same path, but one record only one row.
Every row is applied or none is: any row that names an unknown or non-OBJE
record, a path outside --root, or a FILE that already holds another path is
reported, by line, and nothing is written.

Text output is one tab-separated line per row: xref, action, path. The action
is "set" (an empty FILE now holds the path), "replaced" (with --replace) or
"unchanged" (the FILE, and the FORM if given, already held it).

Records not involved are written exactly as they were read.`,
		Example: `  gedcom media set family.ged --obje @O1@ --path photos/ada.jpg -o out.ged
  gedcom media set family.ged --root media --batch files.tsv -o out.ged`,
		Args: exactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			format, err := outputFormat(cmd)
			if err != nil {
				return err
			}
			flags := cmd.Flags()
			object, _ := flags.GetString("obje")
			mediaPath, _ := flags.GetString("path")
			form, _ := flags.GetString("form")
			batch, _ := flags.GetString("batch")
			root, _ := flags.GetString("root")
			replace, _ := flags.GetBool("replace")

			out, err := outputPath(cmd, args[0])
			if err != nil {
				return err
			}
			var rows []mediaSetRow
			switch {
			case batch != "" && (object != "" || mediaPath != "" || form != ""):
				return usageError{errors.New("--batch cannot be combined with --obje, --path or --form")}
			case batch != "":
				if rows, err = readMediaSetBatch(batch); err != nil {
					return err
				}
			case object == "" || mediaPath == "":
				return usageError{errors.New("give --obje and --path, or --batch")}
			default:
				rows = []mediaSetRow{{object: object, path: mediaPath, form: form}}
			}

			res, err := decodeForWrite(args[0])
			if err != nil {
				return err
			}
			doc := res.Document
			results, err := setMedia(doc, rows, root, replace)
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
				return writeJSON(w, results)
			}
			for _, r := range results {
				if _, err := fmt.Fprintf(w, "%s\t%s\t%s\n", r.Object, r.Action, oneLine(r.Path)); err != nil {
					return err
				}
			}
			return nil
		},
	}
	addOutputFlag(cmd)
	cmd.Flags().String("obje", "", "xref of the OBJE record whose FILE to set")
	cmd.Flags().String("path", "", "the media file's path, as FILE will hold it")
	cmd.Flags().String("form", "", "FORM to set as well; default: leave it as it is")
	cmd.Flags().String("batch", "", "tab-separated file of rows: xref, path, optional FORM")
	cmd.Flags().String("root", "", "media folder each path must name a file in")
	cmd.Flags().Bool("replace", false, "replace a FILE that already holds another path")
	return cmd
}

// readMediaSetBatch reads the rows of a media set --batch file.
func readMediaSetBatch(name string) ([]mediaSetRow, error) {
	batch, err := readBatch(name, 2, 3, "xref, path and an optional FORM")
	if err != nil {
		return nil, err
	}
	rows := make([]mediaSetRow, 0, len(batch))
	for _, b := range batch {
		row := mediaSetRow{line: b.line, object: b.fields[0], path: b.fields[1]}
		if len(b.fields) == 3 {
			row.form = b.fields[2]
		}
		rows = append(rows, row)
	}
	return rows, nil
}

// setMedia checks every row, then applies them all to doc's typed entities.
// When any row fails, doc is left unchanged and every failure is returned.
func setMedia(doc *gedcom.Document, rows []mediaSetRow, root string, replace bool) ([]mediaSetResult, error) {
	type target struct {
		object *gedcom.MediaObject
		file   *gedcom.MediaFile
		path   string
		action string
	}
	targets := make([]target, len(rows))
	seen := map[string]int{} // xref to the index of the row that sets it
	var errs []string
	for i, row := range rows {
		records, err := findRecords(doc, []string{row.object})
		if err != nil {
			errs = append(errs, rowError(row.line, "%v", err))
			continue
		}
		record := records[0]
		object, ok := record.GetMediaObject()
		if !ok {
			errs = append(errs, rowError(row.line, "%s is a %s record; want an OBJE record", record.XRef, record.Type))
			continue
		}
		if j, ok := seen[record.XRef]; ok {
			errs = append(errs, rowError(row.line, "%s is already set by line %d", record.XRef, rows[j].line))
			continue
		}
		seen[record.XRef] = i
		if len(object.Files) != 1 || object.Files[0] == nil {
			errs = append(errs, rowError(row.line, "%s has %d FILE lines; want exactly one", record.XRef, len(object.Files)))
			continue
		}
		if msg := checkMediaPath(row.path, root); msg != "" {
			errs = append(errs, rowError(row.line, "%s", msg))
			continue
		}
		file := object.Files[0]
		filePath := strings.ReplaceAll(row.path, `\`, "/")
		action := actionSet
		switch {
		case strings.TrimSpace(file.FileRef) == "":
		case normalizeMediaPath(file.FileRef) == normalizeMediaPath(filePath):
			action = actionUnchanged
			if row.form != "" && mediaForm(object, file) != row.form {
				action = actionSet
			}
		case replace:
			action = actionReplaced
		default:
			errs = append(errs, rowError(row.line, "%s already has FILE %s; pass --replace to replace it", record.XRef, oneLine(file.FileRef)))
			continue
		}
		targets[i] = target{object: object, file: file, path: filePath, action: action}
	}
	if len(errs) > 0 {
		return nil, errors.New(strings.Join(errs, "\n"))
	}

	results := make([]mediaSetResult, 0, len(rows))
	for i, row := range rows {
		t := targets[i]
		if t.action != actionUnchanged {
			// A FILE that already names the path keeps how it spells it.
			if t.action != actionSet || strings.TrimSpace(t.file.FileRef) == "" {
				t.file.FileRef = t.path
			}
			if row.form != "" {
				setMediaForm(t.object, t.file, row.form)
			}
		}
		results = append(results, mediaSetResult{Object: t.object.XRef, Path: t.path, Action: t.action})
	}
	return results, nil
}

// mediaForm is the FORM of file in object: under FILE, or beside it in the
// GEDCOM 5.5 record layout.
func mediaForm(object *gedcom.MediaObject, file *gedcom.MediaFile) string {
	if file.Form == "" {
		return object.Form
	}
	return file.Form
}

// setMediaForm sets the FORM of file in object where mediaForm reads it.
func setMediaForm(object *gedcom.MediaObject, file *gedcom.MediaFile, form string) {
	if file.Form == "" && object.Form != "" {
		object.Form = form
		return
	}
	file.Form = form
}
