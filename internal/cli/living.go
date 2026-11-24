package cli

import (
	"fmt"
	"slices"

	"github.com/cacack/gedcom-go/v3/gedcom"
	"github.com/spf13/cobra"
)

// Living people. Who is presumed living is the library's rule
// (gedcom.Individual.PresumedLiving), configured by --living-years and
// --reference-year. --redact-living hides what the output would say about
// them. Every command reads its input through openDocument, or, when it reports
// on the unredacted document as validate does, redacts its own results with
// hiddenRecords and redactFindings.

// redactedValue replaces whatever --redact-living hides.
const redactedValue = "Living"

func addLivingFlags(root *cobra.Command) {
	flags := root.PersistentFlags()
	flags.Bool("redact-living", false, "replace a presumed-living person's name, dates, places, notes and addresses with "+redactedValue+", keeping the xref")
	flags.Int("living-years", gedcom.DefaultLivingMaxAge, "presume a person living unless they died or a date of theirs is more than this many years before the reference year; affects list indi and --redact-living")
	flags.Int("reference-year", 0, "the year the living test is made in; 0 means the current year; affects list indi and --redact-living")
}

// livingOptions are the living flags of one invocation.
type livingOptions struct {
	cfg    gedcom.LivingConfig
	redact bool
}

// readLivingOptions reads and checks the living flags.
func readLivingOptions(cmd *cobra.Command) (livingOptions, error) {
	flags := cmd.Flags()
	redact, err := flags.GetBool("redact-living")
	if err != nil {
		return livingOptions{}, err
	}
	years, err := flags.GetInt("living-years")
	if err != nil {
		return livingOptions{}, err
	}
	if years < 1 {
		return livingOptions{}, usageError{fmt.Errorf("invalid --living-years %d: want 1 or more", years)}
	}
	year, err := flags.GetInt("reference-year")
	if err != nil {
		return livingOptions{}, err
	}
	if year < 0 {
		return livingOptions{}, usageError{fmt.Errorf("invalid --reference-year %d: want a year, or 0 for the current year", year)}
	}
	return livingOptions{cfg: gedcom.LivingConfig{MaxAge: years, ReferenceYear: year}, redact: redact}, nil
}

// openDocument decodes the file at path and reports who in it is presumed
// living. With --redact-living, the document is redacted before it is
// returned, so a command cannot print what it hides.
func openDocument(cmd *cobra.Command, path string) (*gedcom.Document, map[*gedcom.Individual]bool, error) {
	opts, err := readLivingOptions(cmd)
	if err != nil {
		return nil, nil, err
	}
	doc, err := decodeFile(path)
	if err != nil {
		return nil, nil, err
	}
	living := presumedLiving(doc, opts.cfg)
	if opts.redact {
		if err := redactDocument(hiddenRecords(doc, living)); err != nil {
			return nil, nil, err
		}
	}
	return doc, living, nil
}

// presumedLiving is the set of individuals the library presumes living.
func presumedLiving(doc *gedcom.Document, cfg gedcom.LivingConfig) map[*gedcom.Individual]bool {
	living := make(map[*gedcom.Individual]bool)
	for _, ind := range doc.Individuals() {
		if ok, _ := ind.PresumedLiving(doc, cfg); ok {
			living[ind] = true
		}
	}
	return living
}

// hiddenRecords is the set of records --redact-living hides: each living
// individual's, each family's with a living spouse (its events are theirs),
// and each note record either of those points to.
func hiddenRecords(doc *gedcom.Document, living map[*gedcom.Individual]bool) map[*gedcom.Record]bool {
	hidden := make(map[*gedcom.Record]bool)
	for _, record := range doc.Records {
		if ind, ok := record.GetIndividual(); ok && living[ind] {
			hidden[record] = true
		}
		if fam, ok := record.GetFamily(); ok && (living[doc.GetIndividual(fam.Husband)] || living[doc.GetIndividual(fam.Wife)]) {
			hidden[record] = true
		}
	}
	for record := range hidden {
		for _, tag := range record.Tags {
			if note := doc.GetRecord(tag.Value); note != nil && isNoteRecord(note) {
				hidden[note] = true
			}
		}
	}
	return hidden
}

func isNoteRecord(r *gedcom.Record) bool {
	return r.Type == gedcom.RecordTypeNote || r.Type == gedcom.RecordTypeSharedNote
}

// redactDocument replaces each hidden record, in place, with what may be shown
// of it: an individual keeps its xref, sex and family links and is named
// Living; a family keeps its spouses and children; a note reads Living. The
// record's tags are dropped, so the library writes it from the redacted entity.
// A hidden record of any other type is an error, never passed through as it is.
func redactDocument(hidden map[*gedcom.Record]bool) error {
	for record := range hidden {
		switch e := record.Entity.(type) {
		case *gedcom.Individual:
			*e = gedcom.Individual{
				XRef:             e.XRef,
				Names:            []*gedcom.PersonalName{{Full: redactedValue}},
				Sex:              e.Sex,
				ChildInFamilies:  familyLinks(e.ChildInFamilies),
				SpouseInFamilies: familyLinks(e.SpouseInFamilies),
			}
		case *gedcom.Family:
			*e = gedcom.Family{XRef: e.XRef, Husband: e.Husband, Wife: e.Wife, Children: e.Children}
		case *gedcom.Note:
			*e = gedcom.Note{XRef: e.XRef, Text: redactedValue}
		case *gedcom.SharedNote:
			*e = gedcom.SharedNote{XRef: e.XRef, Text: redactedValue}
		default:
			return fmt.Errorf("--redact-living cannot redact %s, a %s record", record.XRef, record.Type)
		}
		record.Tags, record.Value = nil, ""
	}
	return nil
}

// familyLinks keeps only the family each link points to.
func familyLinks(links []gedcom.FamilyLink) []gedcom.FamilyLink {
	kept := make([]gedcom.FamilyLink, 0, len(links))
	for _, link := range links {
		kept = append(kept, gedcom.FamilyLink{FamilyXRef: link.FamilyXRef})
	}
	return kept
}

// redactFindings hides the message and context of each finding about a hidden
// record: one that names it, or whose line falls inside it.
func redactFindings(all []finding, doc *gedcom.Document, hidden map[*gedcom.Record]bool) {
	inHidden := hiddenLines(doc, hidden)
	for i, f := range all {
		if hidden[doc.GetRecord(f.XRef)] || hidden[doc.GetRecord(f.RelatedXRef)] || inHidden(f.Line) {
			all[i].Message, all[i].Context = redactedValue, ""
		}
	}
}

// hiddenLines reports whether a line number falls inside a hidden record: from
// its level-0 line up to the line before the next record's.
func hiddenLines(doc *gedcom.Document, hidden map[*gedcom.Record]bool) func(line int) bool {
	type span struct {
		start  int
		hidden bool
	}
	var spans []span
	for _, record := range doc.Records {
		if record.LineNumber > 0 {
			spans = append(spans, span{record.LineNumber, hidden[record]})
		}
	}
	slices.SortFunc(spans, func(a, b span) int { return a.start - b.start })
	return func(line int) bool {
		i, found := slices.BinarySearchFunc(spans, line, func(s span, line int) int { return s.start - line })
		if !found {
			i--
		}
		return line > 0 && i >= 0 && spans[i].hidden
	}
}
