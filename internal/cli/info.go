package cli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"

	"github.com/cacack/gedcom-go/v3/decoder"
	"github.com/spf13/cobra"
)

// fileInfo is the result of the info command. Its JSON form is the command's
// machine-readable output.
type fileInfo struct {
	File         string         `json:"file"`
	Version      string         `json:"version"`
	Encoding     string         `json:"encoding"`
	SourceSystem string         `json:"sourceSystem,omitempty"`
	Records      int            `json:"records"`
	RecordTypes  map[string]int `json:"recordTypes"`
}

func newInfoCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "info <file>",
		Short: "Summarize a GEDCOM file: version, encoding, source system, record counts",
		Example: `  gedcom info family.ged
  gedcom info --format json family.ged`,
		Args: exactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			format, err := outputFormat(cmd)
			if err != nil {
				return err
			}
			info, err := readInfo(args[0])
			if err != nil {
				return err
			}
			if format == formatJSON {
				return writeJSON(cmd.OutOrStdout(), info)
			}
			return writeInfoText(cmd.OutOrStdout(), info)
		},
	}
}

func readInfo(path string) (*fileInfo, error) {
	f, err := os.Open(filepath.Clean(path))
	if err != nil {
		return nil, err
	}
	defer f.Close()

	doc, err := decoder.Decode(f)
	if err != nil {
		return nil, fmt.Errorf("decode %s: %w", path, err)
	}

	info := &fileInfo{
		File:        path,
		Records:     len(doc.Records),
		RecordTypes: make(map[string]int),
	}
	if doc.Header != nil {
		info.Version = string(doc.Header.Version)
		info.Encoding = string(doc.Header.Encoding)
		info.SourceSystem = doc.Header.SourceSystem
	}
	for _, record := range doc.Records {
		info.RecordTypes[string(record.Type)]++
	}
	return info, nil
}

func writeInfoText(w io.Writer, info *fileInfo) error {
	fmt.Fprintf(w, "File:          %s\n", info.File)
	fmt.Fprintf(w, "Version:       %s\n", info.Version)
	fmt.Fprintf(w, "Encoding:      %s\n", info.Encoding)
	if info.SourceSystem != "" {
		fmt.Fprintf(w, "Source system: %s\n", info.SourceSystem)
	}
	fmt.Fprintf(w, "Records:       %d\n", info.Records)

	types := make([]string, 0, len(info.RecordTypes))
	for t := range info.RecordTypes {
		types = append(types, t)
	}
	sort.Strings(types)
	for _, t := range types {
		if _, err := fmt.Fprintf(w, "  %-5s %d\n", t, info.RecordTypes[t]); err != nil {
			return err
		}
	}
	return nil
}
