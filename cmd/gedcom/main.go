// Command gedcom inspects, validates and corrects GEDCOM genealogy files.
package main

import (
	"os"

	"github.com/cacack/gedcom-cli/internal/cli"
)

func main() {
	os.Exit(cli.Run(os.Args[1:], os.Stdout, os.Stderr))
}
