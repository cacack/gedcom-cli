package cli

import (
	"github.com/cacack/gedcom-go/v3/gedcom"
	"github.com/spf13/cobra"
)

func newDescendantsCmd() *cobra.Command {
	return newWalkCmd("descendants", "children", "grandchildren",
		"children in the file order of their families, then of the CHIL lines.",
		(*gedcom.Document).DescendantsWithGenerations)
}
