package cli

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"mime"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/cacack/gedcom-go/v3/encoder"
	"github.com/cacack/gedcom-go/v3/gedcom"
	"github.com/spf13/cobra"
)

// Statuses of a FILE reference, as media check reports them.
const (
	statusOK           = "ok"
	statusMissing      = "missing"
	statusEmpty        = "empty"
	statusURL          = "url"
	statusAbsolute     = "absolute"
	statusOutside      = "outside"
	statusUnreferenced = "unreferenced"
)

var (
	// urlRef is a reference that starts with a URI scheme ("https:", "file:").
	// A scheme has at least two characters, so a Windows drive is not one.
	urlRef = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9+.-]+:`)
	// absoluteRef is a rooted path, POSIX or Windows ("/x", `\x`, `C:\x`).
	absoluteRef = regexp.MustCompile(`^([/\\]|[A-Za-z]:[/\\])`)
)

func newMediaCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "media",
		Short: "Check FILE references against a media folder, and attach media to records",
		Long: `Work with the multimedia references inside a GEDCOM file. The media files
themselves are never read, moved or changed.

  check  report whether each FILE reference resolves under a media folder
  add    create or reuse an OBJE record for a file and link it from a record`,
		Example: `  gedcom media check --root media family.ged
  gedcom media add family.ged --to @I1@ --path photos/ada.jpg -o out.ged`,
		Args: func(_ *cobra.Command, args []string) error {
			if len(args) > 0 {
				return usageError{fmt.Errorf("unknown media command %q; see 'gedcom media --help'", args[0])}
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, _ []string) error { return cmd.Help() },
	}
	cmd.AddCommand(newMediaCheckCmd(), newMediaAddCmd())
	return cmd
}

// mediaRef is one FILE reference and what it resolved to.
type mediaRef struct {
	XRef   string `json:"xref"`
	Line   int    `json:"line,omitempty"`
	File   string `json:"file"`
	Status string `json:"status"`
}

type mediaCheckReport struct {
	File       string     `json:"file"`
	Root       string     `json:"root"`
	References []mediaRef `json:"references"`
	// Unreferenced is set only with --unreferenced.
	Unreferenced *[]string `json:"unreferenced,omitempty"`
}

func newMediaCheckCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "check <file> --root <dir>",
		Short: "Report whether each FILE reference resolves to a file under a media folder",
		Long: `Report every FILE reference in the file's records, in file order, with its
status:

  ok        a relative path naming a file under --root
  missing   a relative path naming no file under --root
  empty     no value: the media is not a local file
  url       a URL (it starts with a scheme such as https: or file:)
  absolute  a rooted path (/x, \x or C:\x), which is not resolved
  outside   a relative path that climbs out of --root (../x), not resolved

Relative paths are resolved against --root, with \ read as a separator as
older Windows programs wrote it. FILE translations (7.0 FILE.TRAN) are not
checked.

--unreferenced also lists the files under --root that no FILE names, as paths
relative to --root. Hidden files and folders (a name starting with ".") are
skipped. A file a reference resolves to counts as referenced even when the
names differ in case or Unicode form, as they may on macOS and network shares.

Text output is tab-separated with no header: status, xref, line, file. An
unreferenced file is a line with status "unreferenced" and no xref or line.
Exit 1 when any reference is missing.`,
		Example: `  gedcom media check --root media family.ged
  gedcom media check --root media --unreferenced family.ged
  gedcom media check --root media --format json family.ged | jq '.references[] | select(.status == "missing")'`,
		Args: exactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			format, err := outputFormat(cmd)
			if err != nil {
				return err
			}
			root, err := cmd.Flags().GetString("root")
			if err != nil {
				return err
			}
			if root == "" {
				return usageError{errors.New("--root is required")}
			}
			if info, err := os.Stat(root); err != nil {
				return err
			} else if !info.IsDir() {
				return fmt.Errorf("--root %s is not a folder", root)
			}
			unreferenced, err := cmd.Flags().GetBool("unreferenced")
			if err != nil {
				return err
			}
			doc, err := decodeFile(args[0])
			if err != nil {
				return err
			}
			report, err := checkMedia(doc, args[0], root, unreferenced)
			if err != nil {
				return err
			}
			if format == formatJSON {
				err = writeJSON(cmd.OutOrStdout(), report)
			} else {
				err = writeMediaCheckText(cmd.OutOrStdout(), report)
			}
			if err != nil {
				return err
			}
			missing := 0
			for _, ref := range report.References {
				if ref.Status == statusMissing {
					missing++
				}
			}
			if missing == 1 {
				return fmt.Errorf("1 missing media file in %s", args[0])
			} else if missing > 1 {
				return fmt.Errorf("%d missing media files in %s", missing, args[0])
			}
			return nil
		},
	}
	cmd.Flags().String("root", "", "media folder that relative FILE paths resolve against (required)")
	cmd.Flags().Bool("unreferenced", false, "also list files under --root that no FILE references")
	return cmd
}

// checkMedia resolves every FILE line in doc's records against root.
func checkMedia(doc *gedcom.Document, file, root string, withUnreferenced bool) (*mediaCheckReport, error) {
	report := &mediaCheckReport{File: file, Root: root, References: []mediaRef{}}
	referenced := &referencedFiles{names: map[string]bool{}, bySize: map[int64][]os.FileInfo{}}
	for _, record := range doc.Records {
		for _, tag := range record.Tags {
			if tag == nil || tag.Tag != "FILE" {
				continue
			}
			ref := mediaRef{XRef: record.XRef, Line: tag.LineNumber, File: tag.Value}
			switch {
			case strings.TrimSpace(tag.Value) == "":
				ref.Status = statusEmpty
			case urlRef.MatchString(tag.Value):
				ref.Status = statusURL
			case absoluteRef.MatchString(tag.Value):
				ref.Status = statusAbsolute
			case escapesRoot(normalizeMediaPath(tag.Value)):
				ref.Status = statusOutside
			default:
				rel := normalizeMediaPath(tag.Value)
				referenced.names[rel] = true
				ref.Status = statusMissing
				if info := statMediaFile(root, rel); info != nil {
					ref.Status = statusOK
					referenced.bySize[info.Size()] = append(referenced.bySize[info.Size()], info)
				}
			}
			report.References = append(report.References, ref)
		}
	}
	if withUnreferenced {
		files, err := unreferencedFiles(root, referenced)
		if err != nil {
			return nil, err
		}
		report.Unreferenced = &files
	}
	return report, nil
}

// normalizeMediaPath is a relative FILE value as a clean slash-separated path,
// reading \ as a separator.
func normalizeMediaPath(value string) string {
	return path.Clean(strings.ReplaceAll(value, `\`, "/"))
}

// escapesRoot reports whether rel, a normalized path, climbs out of the
// folder it is relative to.
func escapesRoot(rel string) bool {
	return rel == ".." || strings.HasPrefix(rel, "../")
}

// statMediaFile returns the file info of rel, a normalized path, when it names
// a regular file under root, and nil otherwise.
func statMediaFile(root, rel string) os.FileInfo {
	info, err := os.Stat(filepath.Join(root, filepath.FromSlash(rel)))
	if err != nil || !info.Mode().IsRegular() {
		return nil
	}
	return info
}

// referencedFiles is what the FILE references under a root name: their
// normalized paths, and the files the resolved ones stat to, by size.
type referencedFiles struct {
	names  map[string]bool
	bySize map[int64][]os.FileInfo
}

// has reports whether the file d at p, rel under the root, is referenced: by
// its path, or as the same file a reference resolved to under another
// spelling (case or Unicode form, on a filesystem that ignores them).
func (r *referencedFiles) has(p, rel string, d fs.DirEntry) (bool, error) {
	if r.names[rel] {
		return true, nil
	}
	entry, err := d.Info()
	if err != nil {
		return false, err
	}
	candidates := r.bySize[entry.Size()]
	if len(candidates) == 0 {
		return false, nil
	}
	info, err := os.Stat(p)
	if err != nil {
		return false, err
	}
	for _, ref := range candidates {
		if os.SameFile(info, ref) {
			return true, nil
		}
	}
	return false, nil
}

// unreferencedFiles lists, in lexical order, the files under root that are not
// referenced, skipping hidden files and folders. A root that is a symlink is
// followed.
func unreferencedFiles(root string, referenced *referencedFiles) ([]string, error) {
	root, err := filepath.EvalSymlinks(root)
	if err != nil {
		return nil, err
	}
	files := []string{}
	err = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if p != root && strings.HasPrefix(d.Name(), ".") {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() {
			return nil
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		ok, err := referenced.has(p, rel, d)
		if err != nil {
			return err
		}
		if !ok {
			files = append(files, rel)
		}
		return nil
	})
	return files, err
}

func writeMediaCheckText(w io.Writer, report *mediaCheckReport) error {
	for _, ref := range report.References {
		if _, err := fmt.Fprintf(w, "%s\t%s\tline %d\t%s\n", ref.Status, ref.XRef, ref.Line, oneLine(ref.File)); err != nil {
			return err
		}
	}
	if report.Unreferenced != nil {
		for _, file := range *report.Unreferenced {
			if _, err := fmt.Fprintf(w, "%s\t\t\t%s\n", statusUnreferenced, oneLine(file)); err != nil {
				return err
			}
		}
	}
	return nil
}

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
result to --output. The input file is never changed.

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
			out, _ := flags.GetString("output")
			to, _ := flags.GetString("to")
			mediaPath, _ := flags.GetString("path")
			title, _ := flags.GetString("title")
			form, _ := flags.GetString("form")
			batch, _ := flags.GetString("batch")
			root, _ := flags.GetString("root")

			if out == "" {
				return usageError{errors.New("--output is required")}
			}
			if err := checkNotInput(args[0], out); err != nil {
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
			var buf bytes.Buffer
			if err := encodeDocument(&buf, doc); err != nil {
				return err
			}
			if err := os.WriteFile(out, buf.Bytes(), 0o644); err != nil {
				return err
			}
			if format == formatJSON {
				return writeJSON(cmd.OutOrStdout(), added)
			}
			for _, a := range added {
				if _, err := fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\t%s\t%s\n", a.To, a.Object, a.Action, oneLine(a.Path)); err != nil {
					return err
				}
			}
			return nil
		},
	}
	cmd.Flags().StringP("output", "o", "", "file to write the result to (required; never the input)")
	cmd.Flags().String("to", "", "xref of the individual, family or source to link the media from")
	cmd.Flags().String("path", "", "the media file's path, as FILE will hold it")
	cmd.Flags().String("title", "", "title of a new OBJE record's file (TITL)")
	cmd.Flags().String("form", "", "FORM of a new OBJE record's file; default from the extension")
	cmd.Flags().String("batch", "", "tab-separated file of rows: xref, path, optional title")
	cmd.Flags().String("root", "", "media folder each path must name a file in")
	return cmd
}

// checkNotInput refuses an output path that is the input file.
func checkNotInput(in, out string) error {
	inInfo, err := os.Stat(in)
	if err != nil {
		return err
	}
	if outInfo, err := os.Stat(out); err == nil && os.SameFile(inInfo, outInfo) {
		return usageError{fmt.Errorf("--output %s is the input file; write to a new file", out)}
	}
	return nil
}

// readMediaBatch reads the rows of a --batch file.
func readMediaBatch(name string) ([]mediaRow, error) {
	f, err := os.Open(filepath.Clean(name))
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var rows []mediaRow
	var errs []string
	scanner := bufio.NewScanner(f)
	for n := 1; scanner.Scan(); n++ {
		line := strings.TrimRight(scanner.Text(), "\r")
		if strings.TrimSpace(line) == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) < 2 || len(fields) > 3 || fields[0] == "" || fields[1] == "" {
			errs = append(errs, fmt.Sprintf("%s line %d: want xref, path and an optional title, tab-separated", name, n))
			continue
		}
		row := mediaRow{line: n, to: fields[0], path: fields[1]}
		if len(fields) == 3 {
			row.title = fields[2]
		}
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
