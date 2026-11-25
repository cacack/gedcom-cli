package cli

import (
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	gedcomgo "github.com/cacack/gedcom-go/v3"
	"github.com/cacack/gedcom-go/v3/decoder"
	"github.com/cacack/gedcom-go/v3/gedcom"
	"github.com/cacack/gedcom-go/v3/merge"
	"github.com/cacack/gedcom-go/v3/validator"
	"github.com/spf13/cobra"
)

// nameReport is what name did to one name: its lines before and after, and
// the findings the edit would have added, which stopped the write.
type nameReport struct {
	Input   string `json:"input"`
	Output  string `json:"output"`
	Written bool   `json:"written"`
	XRef    string `json:"xref"`
	// Name is the position addressed; Position is the name's position in
	// the output.
	Name     int            `json:"name"`
	Position int            `json:"position"`
	Before   []string       `json:"before"`
	After    []string       `json:"after"`
	Counts   severityCounts `json:"counts"`
	Findings []finding      `json:"findings"`
}

// nameEdit is the change asked of one name. A nil field is left as it is; an
// empty type or nick clears it.
type nameEdit struct {
	value, typ, nick *string
	// moveTo is the position to move the name to, or 0 to leave it.
	moveTo int
}

func newNameCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "name <file> <xref> <n> -o <out>",
		Short: "Edit one of a person's names: reorder it, set its TYPE, value or NICK",
		Long: `Edit the n-th NAME structure of the individual <xref> and write the result to
--output. The input file is never changed. Names are counted from 1, in file
order, among the person's level-1 NAME lines, as 'gedcom get' prints them.

The operations, any of which may be combined in one run:

  --move-to <m>      move the name, with every line under it, to position m;
                     the other names keep their order, and every NAME stays
                     in the place a name held, so a line between two names
                     stays between them
  --type <t>         set the name's TYPE: the first 2 TYPE line under it
                     takes the value (and keeps a PHRASE under it), later
                     ones are removed; with none, one is added directly
                     under the NAME line
  --clear-type       remove every TYPE line under the name, with its PHRASE
  --value <v>        set the NAME line's value, "Given /Surname/ Suffix";
                     GIVN, SURN and NSFX under it are rewritten to the text
                     before, between and after the slashes, as gedcom-go
                     reads the value, and removed when that text is empty;
                     a repeat of one is removed, and a part the name has no
                     line for is not added. NPFX, SPFX, NICK, citations
                     and notes are not touched
  --nick <n>         set the name's NICK, as --type sets TYPE
  --clear-nick       remove every NICK line under the name

Choosing which name is right is yours: no operation is inferred. Every other
line is written exactly as it was read.

Before writing, the output is validated by gedcom-go, with its grammar check,
against the file's GEDCOM version. An edit that adds a finding the input did
not have, such as a TYPE the version does not allow (7.0 takes AKA, BIRTH,
IMMIGRANT, MAIDEN, MARRIED, PROFESSIONAL or OTHER; 5.5.1 takes any text),
writes nothing and exits 1, with those findings reported.

The report gives the name's lines before and after the edit, joined with
" | ". An xref that is not an individual, or a position the person has no
name at, is an error and nothing is written. With --redact-living, the lines
of a presumed-living person read Living; the file written is not redacted.

-o is required and may not name file. An existing output file is replaced
only once the whole result is written. With -o - the GEDCOM goes to stdout
and the report to stderr.`,
		Example: `  gedcom name family.ged @I1@ 2 --move-to 1 -o fixed.ged
  gedcom name family.ged @I1@ 2 --type married -o fixed.ged
  gedcom name family.ged I1 1 --value "Anna Maria /Example/" --clear-nick -o fixed.ged
  gedcom name --format json family.ged @I1@ 3 --nick Nan -o fixed.ged | jq .after`,
		Args: exactArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			format, err := outputFormat(cmd)
			if err != nil {
				return err
			}
			opts, err := readLivingOptions(cmd)
			if err != nil {
				return err
			}
			out, err := outputPath(cmd, args[0])
			if err != nil {
				return err
			}
			pos, err := strconv.Atoi(args[2])
			if err != nil || pos < 1 {
				return usageError{fmt.Errorf("invalid name position %q: want a number from 1", args[2])}
			}
			edit, err := readNameEdit(cmd)
			if err != nil {
				return err
			}

			report, editErr := editName(args[0], args[1], pos, edit, out, opts, func(doc *gedcom.Document) error {
				return writeOutput(cmd, out, doc)
			})
			if report == nil {
				return editErr
			}
			w := reportWriter(cmd, out)
			if format == formatJSON {
				err = writeJSON(w, report)
			} else {
				err = writeNameText(w, report)
			}
			if err != nil {
				return err
			}
			return editErr
		},
	}
	addOutputFlag(cmd)
	flags := cmd.Flags()
	flags.Int("move-to", 0, "move the name, with every line under it, to this position")
	flags.String("type", "", "set the name's TYPE")
	flags.Bool("clear-type", false, "remove the name's TYPE")
	flags.String("value", "", `set the name's value, "Given /Surname/ Suffix", rewriting GIVN, SURN and NSFX to match`)
	flags.String("nick", "", "set the name's NICK")
	flags.Bool("clear-nick", false, "remove the name's NICK")
	return cmd
}

// readNameEdit reads the operation flags; at least one is required.
func readNameEdit(cmd *cobra.Command) (nameEdit, error) {
	flags := cmd.Flags()
	var edit nameEdit
	if flags.Changed("move-to") {
		edit.moveTo, _ = flags.GetInt("move-to")
		if edit.moveTo < 1 {
			return edit, usageError{fmt.Errorf("invalid --move-to %d: want a number from 1", edit.moveTo)}
		}
	}
	text := func(flag, clear string) (*string, error) {
		set, cleared := flags.Changed(flag), false
		if clear != "" {
			cleared, _ = flags.GetBool(clear)
		}
		switch {
		case set && cleared:
			return nil, usageError{fmt.Errorf("--%s and --%s cannot be combined", flag, clear)}
		case cleared:
			empty := ""
			return &empty, nil
		case !set:
			return nil, nil
		}
		v, _ := flags.GetString(flag)
		if strings.TrimSpace(v) == "" {
			hint := ""
			if clear != "" {
				hint = "; use --" + clear
			}
			return nil, usageError{fmt.Errorf("--%s is empty%s", flag, hint)}
		}
		if strings.ContainsAny(v, "\r\n") {
			return nil, usageError{fmt.Errorf("--%s holds a line break", flag)}
		}
		return &v, nil
	}
	var err error
	if edit.value, err = text("value", ""); err != nil {
		return edit, err
	}
	if edit.typ, err = text("type", "clear-type"); err != nil {
		return edit, err
	}
	if edit.nick, err = text("nick", "clear-nick"); err != nil {
		return edit, err
	}
	if edit.value == nil && edit.typ == nil && edit.nick == nil && edit.moveTo == 0 {
		return edit, usageError{errors.New("nothing to do: give --move-to, --type, --clear-type, --value, --nick or --clear-nick")}
	}
	return edit, nil
}

// editName applies edit to name pos of the individual xref in the file at in
// and, when validation finds nothing new, writes the result with write. It
// returns the report whenever the name was found, with an error when new
// findings stopped the write.
func editName(in, xref string, pos int, edit nameEdit, out string, opts livingOptions, write func(*gedcom.Document) error) (*nameReport, error) {
	res, err := decodeForWrite(in)
	if err != nil {
		return nil, err
	}
	doc := res.Document
	records, err := findRecords(doc, []string{xref})
	if err != nil {
		return nil, err
	}
	old := records[0]
	if old.Type != gedcom.RecordTypeIndividual {
		return nil, fmt.Errorf("%s is a %s record; want an individual", old.XRef, old.Type)
	}
	hidden := hiddenRecords(doc, presumedLiving(doc, opts.cfg))
	redact := opts.redact && hidden[old]
	before := validationKeys(doc)

	record := old.Clone()
	blocks := splitBlocks(record.Tags, func(t *gedcom.Tag) bool { return t.Level <= 1 })
	var names []int // indexes in blocks of the NAME structures
	for i, b := range blocks {
		if b[0].Level == 1 && b[0].Tag == "NAME" {
			names = append(names, i)
		}
	}
	for _, n := range []int{pos, edit.moveTo} {
		if n > len(names) {
			return nil, fmt.Errorf("%s has %s; there is no name %d", old.XRef, plural(len(names), "name", "names"), n)
		}
	}
	name := blocks[names[pos-1]]
	report := &nameReport{
		Input:    in,
		Output:   out,
		XRef:     old.XRef,
		Name:     pos,
		Position: pos,
		Before:   newMergedPart(merge.Substructure{Tags: name}, redact).Lines,
		Findings: []finding{},
	}

	if edit.value != nil {
		given, surname, suffix, err := nameParts(doc, *edit.value)
		if err != nil {
			return nil, err
		}
		// The new value replaces the whole old one, including any part of
		// it carried on CONC or CONT lines.
		name[0].Value = *edit.value
		name = setNameLine(name, "CONC", "", false)
		name = setNameLine(name, "CONT", "", false)
		name = setNameLine(name, "GIVN", given, false)
		name = setNameLine(name, "SURN", surname, false)
		name = setNameLine(name, "NSFX", suffix, false)
	}
	// NICK first, so that a TYPE added in the same run is the first line.
	if edit.nick != nil {
		name = setNameLine(name, "NICK", *edit.nick, true)
	}
	if edit.typ != nil {
		name = setNameLine(name, "TYPE", *edit.typ, true)
	}
	blocks[names[pos-1]] = name
	if edit.moveTo != 0 {
		moveBlock(blocks, names, pos-1, edit.moveTo-1)
		report.Position = edit.moveTo
	}
	report.After = newMergedPart(merge.Substructure{Tags: name}, redact).Lines
	record.Tags = joinBlocks(blocks)

	if err := doc.ReplaceRecord(record); err != nil {
		return nil, err
	}
	decoder.SyncEntities(doc)
	report.Findings = addedFindings(doc, before)
	if opts.redact {
		hidden[record] = hidden[old]
		redactFindings(report.Findings, doc, hidden)
	}
	report.Counts = countSeverities(report.Findings)
	if len(report.Findings) > 0 {
		return report, fmt.Errorf("the edit adds %s; %s not written", plural(len(report.Findings), "finding", "findings"), outputName(out))
	}
	if err := write(doc); err != nil {
		return nil, err
	}
	report.Written = true
	return report, nil
}

// nameParts is gedcom-go's reading of a NAME value: the text before, between
// and after the slashes.
func nameParts(doc *gedcom.Document, value string) (given, surname, suffix string, err error) {
	records, _, err := decoder.DecodeRecords(strings.NewReader("0 @NAME@ INDI\n1 NAME "+value+"\n"), doc, decoder.DefaultOptions())
	if err != nil {
		return "", "", "", fmt.Errorf("read --value: %w", err)
	}
	ind, ok := records[0].GetIndividual()
	if !ok || len(ind.Names) != 1 {
		return "", "", "", fmt.Errorf("read --value %q: not one name", value)
	}
	n := ind.Names[0]
	return n.Given, n.Surname, n.Suffix, nil
}

// splitBlocks splits tags into runs, each starting at a tag for which starts
// holds (or at the first tag) and holding every tag up to the next.
func splitBlocks(tags []*gedcom.Tag, starts func(*gedcom.Tag) bool) [][]*gedcom.Tag {
	var blocks [][]*gedcom.Tag
	for i, t := range tags {
		if i == 0 || starts(t) {
			blocks = append(blocks, nil)
		}
		blocks[len(blocks)-1] = append(blocks[len(blocks)-1], t)
	}
	return blocks
}

func joinBlocks(blocks [][]*gedcom.Tag) []*gedcom.Tag {
	var tags []*gedcom.Tag
	for _, b := range blocks {
		tags = append(tags, b...)
	}
	return tags
}

// setNameLine sets the value of the first line tagged tag directly under the
// name, the name's first line, and removes the later ones, each with the
// lines under it. An empty value removes them all. With none, add puts a new
// line directly under the name's first line.
func setNameLine(name []*gedcom.Tag, tag, value string, add bool) []*gedcom.Tag {
	level := name[0].Level + 1
	children := splitBlocks(name[1:], func(t *gedcom.Tag) bool { return t.Level <= level })
	kept := [][]*gedcom.Tag{name[:1]}
	found := false
	for _, c := range children {
		if c[0].Level != level || c[0].Tag != tag {
			kept = append(kept, c)
			continue
		}
		if value != "" && !found {
			c[0].Value = value
			kept = append(kept, c)
		}
		found = true
	}
	if !found && add && value != "" {
		line := []*gedcom.Tag{{Level: level, Tag: tag, Value: value}}
		kept = append([][]*gedcom.Tag{name[:1], line}, kept[1:]...)
	}
	return joinBlocks(kept)
}

// moveBlock moves the structure at slots[from] to slots[to] in blocks,
// shifting the structures between them by one slot: the structures in slots
// keep their order otherwise, and the other blocks their places.
func moveBlock(blocks [][]*gedcom.Tag, slots []int, from, to int) {
	order := make([][]*gedcom.Tag, len(slots))
	for i, s := range slots {
		order[i] = blocks[s]
	}
	moved := order[from]
	order = append(order[:from], order[from+1:]...)
	order = append(order[:to], append([][]*gedcom.Tag{moved}, order[to:]...)...)
	for i, s := range slots {
		blocks[s] = order[i]
	}
}

// validateWrite is the validation an edited document is held to: every check
// gedcom-go has but duplicate detection, grammar included, at every severity.
func validateWrite(doc *gedcom.Document) []validator.Issue {
	return gedcomgo.ValidateAllWithOptions(doc, &validator.ValidateOptions{
		Strictness:             validator.StrictnessStrict,
		SkipDuplicateDetection: true,
		CheckGrammar:           true,
	})
}

// issueKey is a validation issue without its line number, which an edit
// moving lines changes.
func issueKey(i validator.Issue) string {
	return strings.Join([]string{i.Severity.String(), i.Code, i.RecordXRef, i.RelatedXRef, i.Message}, "\x00")
}

// validationKeys counts doc's validation issues by issueKey.
func validationKeys(doc *gedcom.Document) map[string]int {
	keys := map[string]int{}
	for _, i := range validateWrite(doc) {
		keys[issueKey(i)]++
	}
	return keys
}

// addedFindings is doc's validation issues beyond those counted in before.
func addedFindings(doc *gedcom.Document, before map[string]int) []finding {
	added := []finding{}
	for _, i := range validateWrite(doc) {
		if key := issueKey(i); before[key] > 0 {
			before[key]--
			continue
		}
		added = append(added, validationFinding(i))
	}
	return added
}

// writeNameText writes a summary line, the name's lines before and after, and
// the findings that stopped the write.
func writeNameText(w io.Writer, r *nameReport) error {
	var b strings.Builder
	if r.Written {
		fmt.Fprintf(&b, "wrote %s: %s name %d", outputName(r.Output), oneLine(r.XRef), r.Name)
		if r.Position != r.Name {
			fmt.Fprintf(&b, ", now name %d", r.Position)
		}
		b.WriteString("\n")
	} else {
		fmt.Fprintf(&b, "%s not written: the edit adds findings\n", outputName(r.Output))
	}
	fmt.Fprintf(&b, "  before\t%s\n  after\t%s\n", strings.Join(r.Before, " | "), strings.Join(r.After, " | "))
	if _, err := io.WriteString(w, b.String()); err != nil {
		return err
	}
	return writeFindingGroups(w, r.Counts, r.Findings)
}
