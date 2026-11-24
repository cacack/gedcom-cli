package cli

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

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
