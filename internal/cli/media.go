package cli

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/spf13/cobra"
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
