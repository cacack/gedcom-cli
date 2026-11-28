// Package cli implements the gedcom command. It holds no GEDCOM logic of its
// own: every command is a thin wrapper over github.com/cacack/gedcom-go.
package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime/debug"

	"github.com/cacack/gedcom-go/v3/decoder"
	"github.com/cacack/gedcom-go/v3/gedcom"
	"github.com/spf13/cobra"
)

// Exit codes. They are part of the command's contract with scripts and agents.
const (
	ExitOK    = 0 // the command succeeded
	ExitError = 1 // the command ran and failed (unreadable file, decode error, validate found errors)
	ExitUsage = 2 // the command line was wrong (unknown flag, missing argument)
)

const (
	formatText = "text"
	formatJSON = "json"
)

// usageError marks a mistake in how the command was invoked.
type usageError struct{ err error }

func (e usageError) Error() string { return e.err.Error() }

// Run executes the gedcom command with args (without the program name) and
// returns the process exit code. Results go to stdout, diagnostics to stderr.
func Run(args []string, stdout, stderr io.Writer) int {
	return runWithStdin(args, nil, stdout, stderr)
}

// runWithStdin is Run reading stdin from stdin, or from os.Stdin when it is
// nil.
func runWithStdin(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	root := newRootCmd()
	root.SetArgs(args)
	if stdin != nil {
		root.SetIn(stdin)
	}
	root.SetOut(stdout)
	root.SetErr(stderr)

	err := root.Execute()
	if err == nil {
		return ExitOK
	}
	fmt.Fprintf(stderr, "gedcom: %v\n", err)
	var ue usageError
	if errors.As(err, &ue) {
		return ExitUsage
	}
	return ExitError
}

func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:           "gedcom",
		Short:         "Inspect, validate and correct GEDCOM genealogy files",
		Version:       version(),
		SilenceUsage:  true,
		SilenceErrors: true,
		Args: func(_ *cobra.Command, args []string) error {
			if len(args) > 0 {
				return usageError{fmt.Errorf("unknown command %q; see 'gedcom --help'", args[0])}
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, _ []string) error { return cmd.Help() },
	}
	root.PersistentFlags().String("format", formatText, `output format: "text" or "json"`)
	addLivingFlags(root)
	root.SetFlagErrorFunc(func(_ *cobra.Command, err error) error { return usageError{err} })
	root.AddCommand(newInfoCmd(), newGetCmd(), newRefsCmd(), newListCmd(), newAncestorsCmd(), newDescendantsCmd(), newRelativesCmd(), newMediaCmd(), newPlacesCmd(), newMergePeopleCmd(), newMergeFamiliesCmd(), newMergeSourcesCmd(), newMergeRepositoriesCmd(), newRepeatedEventsCmd(), newRepeatedNamesCmd(), newValidateCmd(), newDuplicatesCmd(), newExtractCmd(), newPutCmd(), newPutHeaderCmd(), newDeleteCmd(), newNameCmd(), newRetagCmd(), newMoveValueCmd(), newConvertCmd(), newDiffCmd(), newCombineCmd())
	return root
}

// outputFormat reads and checks the --format flag.
func outputFormat(cmd *cobra.Command) (string, error) {
	format, err := cmd.Flags().GetString("format")
	if err != nil {
		return "", err
	}
	if format != formatText && format != formatJSON {
		return "", usageError{fmt.Errorf(`invalid --format %q: want "text" or "json"`, format)}
	}
	return format, nil
}

// exactArgs is cobra.ExactArgs reporting a usageError.
func exactArgs(n int) cobra.PositionalArgs {
	return func(cmd *cobra.Command, args []string) error {
		if err := cobra.ExactArgs(n)(cmd, args); err != nil {
			return usageError{err}
		}
		return nil
	}
}

// minArgs is cobra.MinimumNArgs reporting a usageError.
func minArgs(n int) cobra.PositionalArgs {
	return func(cmd *cobra.Command, args []string) error {
		if err := cobra.MinimumNArgs(n)(cmd, args); err != nil {
			return usageError{err}
		}
		return nil
	}
}

// decodeFile reads and decodes the GEDCOM file at path.
func decodeFile(path string) (*gedcom.Document, error) {
	res, err := decodeFileWithDiagnostics(path)
	if err != nil {
		return nil, err
	}
	return res.Document, nil
}

// decodeFileWithDiagnostics is decodeFile keeping what the decoder could not
// read cleanly.
func decodeFileWithDiagnostics(path string) (*decoder.DecodeResult, error) {
	return decodeFileWithOptions(path, decoder.DefaultOptions())
}

// decodeFileWithOptions is decodeFileWithDiagnostics with the decoder's
// options given. Every command opens and decodes its input here.
func decodeFileWithOptions(path string, opts *decoder.DecodeOptions) (*decoder.DecodeResult, error) {
	f, err := os.Open(filepath.Clean(path))
	if err != nil {
		return nil, err
	}
	defer f.Close()

	res, err := decoder.DecodeWithDiagnostics(f, opts)
	if err != nil {
		return nil, fmt.Errorf("decode %s: %w", path, err)
	}
	return res, nil
}

func writeJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

// buildVersion is set by GoReleaser through -ldflags for release binaries.
var buildVersion string

// version reports the release version: stamped by GoReleaser for a downloaded
// binary, read from the module version for `go install`, or "dev".
func version() string {
	if buildVersion != "" {
		return buildVersion
	}
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" && info.Main.Version != "(devel)" {
		return info.Main.Version
	}
	return "dev"
}
