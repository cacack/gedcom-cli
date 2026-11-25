package cli

import (
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/cacack/gedcom-go/v3/gedcom"
	"github.com/cacack/gedcom-go/v3/merge"
	"github.com/spf13/cobra"
)

// mergeKind is what the merge commands share: the record type they merge,
// the library call that merges it, and the pointer tags the record holds one
// of, so that a second is a conflict.
type mergeKind struct {
	recordType gedcom.RecordType
	noun       string
	merge      func(*gedcom.Document, string, string, merge.RecordMergeOptions) (*gedcom.Document, *merge.RecordMergeReport, error)
	onePointer map[string]bool
	// foldEvents is set for a record with events, which --fold-events folds.
	foldEvents bool
	// dropped is set for a record the library may leave lines of drop's out
	// of (RecordMergeReport.Conflicts), which the report then lists.
	dropped bool
}

var (
	mergePeople       = mergeKind{recordType: gedcom.RecordTypeIndividual, noun: "an individual", merge: merge.Individuals, foldEvents: true}
	mergeFamilies     = mergeKind{recordType: gedcom.RecordTypeFamily, noun: "a family", merge: merge.Families, onePointer: map[string]bool{"HUSB": true, "WIFE": true}, foldEvents: true}
	mergeSources      = mergeKind{recordType: gedcom.RecordTypeSource, noun: "a source", merge: merge.Sources, dropped: true}
	mergeRepositories = mergeKind{recordType: gedcom.RecordTypeRepository, noun: "a repository", merge: merge.Repositories, dropped: true}
)

// mergedPart is one level-1 substructure of the dropped record, as its lines.
type mergedPart struct {
	Tag   string   `json:"tag"`
	Lines []string `json:"lines"`
	// Conflict marks a moved substructure whose tag the kept record already
	// held, with a value that is not a pointer (or a family's HUSB or WIFE).
	// The lines differ, or it would have been deduplicated, but the tag may
	// repeat: it flags one fact stated two ways (two BIRT) and two facts
	// (two OCCU) alike, for the user to tell apart. It is deliberately not
	// narrowed to tags GEDCOM allows once, which would leave out BIRT.
	Conflict bool `json:"conflict,omitempty"`
}

// mergedFold is one substructure of the dropped record folded into the kept
// record's substructure for the same event.
type mergedFold struct {
	Tag string `json:"tag"`
	// Lines are drop's substructure as it was; Into is keep's once every fold
	// into it is done.
	Lines []string `json:"lines"`
	Into  []string `json:"into"`
}

// mergedConflict is one substructure of the dropped record not carried to
// the kept record: its line may occur only once, and keep holds another.
type mergedConflict struct {
	Tag string `json:"tag"`
	// Lines are drop's substructure, which the output lacks; Kept are the
	// lines keep holds instead.
	Lines []string `json:"lines"`
	Kept  []string `json:"kept"`
}

// mergeRef is one pointer that named the dropped record.
type mergeRef struct {
	XRef string `json:"xref"`
	Path string `json:"path"`
}

// familyCandidate is a set of families the kept person is a spouse in with
// the same partner: candidates for merge-families, not merged.
type familyCandidate struct {
	Partner  string   `json:"partner"`
	Families []string `json:"families"`
}

type mergeReport struct {
	Keep         string       `json:"keep"`
	Drop         string       `json:"drop"`
	Moved        []mergedPart `json:"moved"`
	Folded       []mergedFold `json:"folded"`
	Deduplicated []mergedPart `json:"deduplicated"`
	Repointed    []mergeRef   `json:"repointed"`
	Collapsed    []mergeRef   `json:"collapsed"`
	// Candidates is set by merge-people only.
	Candidates *[]familyCandidate `json:"candidates,omitempty"`
	// Dropped is set by merge-sources and merge-repositories only.
	Dropped *[]mergedConflict `json:"dropped,omitempty"`
}

func newMergePeopleCmd() *cobra.Command {
	return newMergeCmd(mergePeople, &cobra.Command{
		Use:   "merge-people <file> (<keep> <drop>... | --batch <tsv>) -o <out>",
		Short: "Fold one individual into another, writing a new file",
		Long: `Merge the individual <drop> into the individual <keep>, and write the result
to --output. The input file is never changed. Deciding that the two are one
person is yours: run it only on a pair you have confirmed, such as one
'gedcom duplicates' found.

gedcom-go does the merge: each of drop's substructures (names, events,
family links, citations, notes) is added under keep, unless keep already
holds one with the same lines; every pointer to drop is rewritten to name
keep, and one that would then repeat a line beside it (a family listing both
as children) is removed; drop is removed. Records not involved are written
exactly as they were read.

With --fold-events, a substructure of drop that records the same event as
one of keep's is folded into it instead of moved beside it: keep's lines
stay, and each line of drop's that keep's lacks (a citation, a note, a place
keep's has none of) is added. Two substructures record the same event, as
gedcom-go decides it, when they have the same tag and value, each has
exactly one DATE and at most one PLAC and one TYPE, and:

  - the dates parse to the same date: "7 DEC 1977" and "07 Dec 1977" are
    the same, "ABT 1977" and "1977" are not; a date that does not parse, is
    invalid ("32 DEC 1977") or is a phrase is the same as none
  - a TIME or PHRASE under both DATEs is equal
  - the places are equal, or only one has a place
  - the TYPEs are equal, or neither has one

Events that differ in any of these are moved and reported as conflicts:
choosing between them is yours.

The report lists, one per line and tab-separated:

  moved         tag, lines    a substructure of drop now under keep
  conflict      tag, lines    moved, but keep already held that tag, with
                              other lines and a value that is not a pointer:
                              one fact stated two ways (two births) or two
                              facts (two occupations); check which
  folded        tag, lines, into
                              with --fold-events, a substructure of drop
                              folded into keep's for the same event; into is
                              keep's once every fold into it is done
  deduplicated  tag, lines    not moved, keep already held the same lines
  repointed     xref, path    a pointer that named drop and now names keep
  collapsed     xref, path    a pointer to drop removed as a repeat
  candidate     partner, families
                              families keep is now a spouse in with the same
                              partner: candidates for merge-families, not merged

A substructure's lines are joined with " | ". With --redact-living, those of a
merge involving a presumed-living person read Living; the file written is not
redacted.

Run 'gedcom validate' on the output to check the result.`,
		Example: `  gedcom merge-people family.ged @I1@ @I7@ -o merged.ged
  gedcom merge-people --fold-events family.ged @I1@ @I7@ -o merged.ged
  gedcom merge-people family.ged @I1@ @I7@ @I9@ -o merged.ged
  gedcom merge-people --format json family.ged I1 I7 -o merged.ged | jq '.moved[] | select(.conflict)'
  gedcom merge-families merged.ged @F1@ @F4@ -o merged2.ged`,
	})
}

func newMergeFamiliesCmd() *cobra.Command {
	return newMergeCmd(mergeFamilies, &cobra.Command{
		Use:   "merge-families <file> (<keep> <drop>... | --batch <tsv>) -o <out>",
		Short: "Fold one family into another, writing a new file",
		Long: `Merge the family <drop> into the family <keep>, and write the result to
--output. The input file is never changed. Deciding that the two are one
family is yours; merge-people reports candidates.

gedcom-go does the merge: each of drop's substructures (HUSB, WIFE, CHIL,
events, citations, notes) is added under keep, unless keep already holds one
with the same lines, so a child both list is listed once; every pointer to
drop (the spouses' FAMS, the children's FAMC) is rewritten to name keep, and
one that would then repeat a line beside it is removed; drop is removed.
Records not involved are written exactly as they were read.

With --fold-events, an event of drop (a marriage, say) that records the same
event as one of keep's is folded into it, by the rule merge-people's help
gives.

The report is merge-people's, without candidates: moved, conflict, folded,
deduplicated, repointed and collapsed lines. A HUSB or WIFE that differs from
keep's is moved, and reported as a conflict. Run 'gedcom validate' on the
output to check the result.`,
		Example: `  gedcom merge-families family.ged @F1@ @F4@ -o merged.ged
  gedcom merge-families family.ged --batch pairs.tsv -o merged.ged
  gedcom merge-families --format json family.ged F1 F4 -o merged.ged`,
	})
}

func newMergeSourcesCmd() *cobra.Command {
	return newMergeCmd(mergeSources, &cobra.Command{
		Use:   "merge-sources <file> (<keep> <drop>... | --batch <tsv>) -o <out>",
		Short: "Fold one source into another, writing a new file",
		Long: `Merge the source record <drop> into the source record <keep>, and write the
result to --output. The input file is never changed. Deciding that the two
are one source is yours: a file combined from two exports of one site often
holds the same source twice, under two xrefs.

gedcom-go does the merge. Every citation of drop, anywhere in the file, is
rewritten to cite keep; no citation is removed, and the lines under it
(PAGE, DATA, QUAY, vendor tags) are not touched. drop is removed. Records
not involved are written exactly as they were read. drop's own lines are
carried to keep this way:

  - a line a source may hold only once (TITL, AUTH, PUBL, ABBR, TEXT, DATA,
    CHAN, and the others the file's GEDCOM version allows once) is carried
    when keep has none, deduplicated when keep's is the same, and otherwise
    not carried: keep's stays, and drop's is reported as dropped
  - every other line (NOTE, OBJE, REFN, vendor tags such as _APID, and REPO
    except in GEDCOM 5.5) is carried, unless keep already holds the same lines

The report lists, one per line and tab-separated:

  moved         tag, lines    a substructure of drop now under keep
  conflict      tag, lines    moved, but keep already held that tag, with
                              other lines and a value that is not a pointer
                              (two _APID, two NOTE); check which
  dropped       tag, lines, kept
                              not carried: the tag may occur once and keep
                              holds other lines, kept; the output lacks
                              drop's lines, so copy what you need from here
  deduplicated  tag, lines    not moved, keep already held the same lines
  repointed     xref, path    a citation that named drop and now names keep

A substructure's lines are joined with " | ". Run 'gedcom validate' on the
output to check the result.`,
		Example: `  gedcom merge-sources family.ged @S1@ @S7@ -o merged.ged
  gedcom merge-sources family.ged @S1@ @S7@ @S9@ -o merged.ged
  gedcom merge-sources family.ged --batch pairs.tsv -o merged.ged
  gedcom merge-sources --format json family.ged S1 S7 -o merged.ged | jq '.dropped'
  gedcom merge-repositories merged.ged @R1@ @R2@ -o merged2.ged`,
	})
}

func newMergeRepositoriesCmd() *cobra.Command {
	return newMergeCmd(mergeRepositories, &cobra.Command{
		Use:   "merge-repositories <file> (<keep> <drop>... | --batch <tsv>) -o <out>",
		Short: "Fold one repository into another, writing a new file",
		Long: `Merge the repository record <drop> into the repository record <keep>, and
write the result to --output. The input file is never changed. Deciding that
the two are one repository is yours.

gedcom-go does the merge, by merge-sources' rule: a line a repository may
hold only once (NAME, ADDR, CHAN, and the others the file's GEDCOM version
allows once) is carried when keep has none, deduplicated when keep's is the
same, and otherwise reported as dropped; every other line (PHON, EMAIL, FAX,
WWW, NOTE, REFN, vendor tags) is carried unless keep holds the same lines.
Every REPO line of a source that named drop is rewritten to name keep, and
one that would then repeat a line beside it (a source that named both, with
the same call number) is removed. drop is removed. Records not involved are
written exactly as they were read.

The report is merge-sources', with one more kind of line:

  collapsed     xref, path    a pointer to drop removed as a repeat

Run 'gedcom validate' on the output to check the result.`,
		Example: `  gedcom merge-repositories family.ged @R1@ @R2@ -o merged.ged
  gedcom merge-repositories family.ged @R1@ @R2@ @R5@ -o merged.ged
  gedcom merge-repositories family.ged --batch pairs.tsv -o merged.ged
  gedcom merge-repositories --format json family.ged R1 R2 -o merged.ged`,
	})
}

// mergeManyHelp is the merge commands' help on merging several pairs in one
// run.
const mergeManyHelp = `

Several merges in one run:

  <keep> <drop>...  merge each drop into keep, in the order given
  --batch <tsv>     merge the pairs of a tab-separated file, one per line:
                    keep, then drop; blank lines and lines starting with #
                    are skipped

The merges run in the order given, each on the result of the one before, so
the output is the file that merging the pairs one per run, each run on the
last one's output, would give. A record an earlier pair kept may be dropped
by a later one, and goes on to the new keep with what was merged into it.
Every pair is checked before anything is
merged: a pair that names no such record or a record of another type, merges
a record into itself, or names a record an earlier pair dropped (a drop named
twice) is reported, by argument or by line, and nothing is written.

The report then lists each pair's lines in turn, each line starting with the
pair's keep and drop; --format json gives an array of reports, one per pair.`

// mergePair is one merge of a run: drop into keep, as typed, with where the
// pair came from for an error.
type mergePair struct {
	label      string
	keep, drop string
}

// mergePairs is the pairs the command line names: one per drop after keep,
// or one per row of --batch.
func mergePairs(cmd *cobra.Command, args []string) ([]mergePair, error) {
	batch, _ := cmd.Flags().GetString("batch")
	if batch == "" {
		pairs := make([]mergePair, 0, len(args)-2)
		for i, drop := range args[2:] {
			pairs = append(pairs, mergePair{label: fmt.Sprintf("argument %d (%s)", i+3, drop), keep: args[1], drop: drop})
		}
		return pairs, nil
	}
	rows, err := readBatch(batch, 2, 2, "keep and drop")
	if err != nil {
		return nil, err
	}
	pairs := make([]mergePair, 0, len(rows))
	for _, row := range rows {
		pairs = append(pairs, mergePair{label: fmt.Sprintf("%s line %d", batch, row.line), keep: row.fields[0], drop: row.fields[1]})
	}
	return pairs, nil
}

// checkPairs checks every pair against doc, as it will be once the pairs
// before it are merged, and returns each pair's keep and drop xrefs. With
// labeled, each error names its pair. The error is a usageError when every
// failure is one.
func checkPairs(doc *gedcom.Document, kind mergeKind, pairs []mergePair, labeled bool) ([][2]string, error) {
	xrefs := make([][2]string, len(pairs))
	droppedBy := map[string]mergePair{} // a dropped xref to the pair that dropped it
	var errs []string
	usage := true
	fail := func(pair mergePair, err error) {
		if _, ok := err.(usageError); !ok {
			usage = false
		}
		msg := err.Error()
		if labeled {
			msg = pair.label + ": " + msg
		}
		errs = append(errs, msg)
	}
	for i, pair := range pairs {
		records, err := findRecords(doc, []string{pair.keep, pair.drop})
		if err != nil {
			fail(pair, err)
			continue
		}
		keep, drop := records[0], records[1]
		if keep == drop {
			fail(pair, usageError{fmt.Errorf("keep and drop are both %s; name two records", keep.XRef)})
			continue
		}
		if by, ok := droppedBy[keep.XRef]; ok {
			fail(pair, usageError{fmt.Errorf("%s was merged into %s by %s", keep.XRef, by.keep, by.label)})
			continue
		}
		if by, ok := droppedBy[drop.XRef]; ok {
			fail(pair, usageError{fmt.Errorf("%s was merged into %s by %s", drop.XRef, by.keep, by.label)})
			continue
		}
		if err := checkMergeType(kind, records); err != nil {
			fail(pair, err)
			continue
		}
		droppedBy[drop.XRef] = mergePair{label: pair.label, keep: keep.XRef}
		xrefs[i] = [2]string{keep.XRef, drop.XRef}
	}
	if len(errs) == 0 {
		return xrefs, nil
	}
	err := errors.New(strings.Join(errs, "\n"))
	if usage {
		return nil, usageError{err}
	}
	return nil, err
}

// checkMergeType returns an error for the first of records that is not one of
// kind's.
func checkMergeType(kind mergeKind, records []*gedcom.Record) error {
	for _, record := range records {
		if record.Type != kind.recordType {
			return fmt.Errorf("%s is a %s record; want %s", record.XRef, record.Type, kind.noun)
		}
	}
	return nil
}

// newMergeCmd completes cmd, which holds the command's help, as a merge of
// kind's records.
func newMergeCmd(kind mergeKind, cmd *cobra.Command) *cobra.Command {
	cmd.Long += mergeManyHelp
	cmd.Args = func(cmd *cobra.Command, args []string) error {
		if batch, _ := cmd.Flags().GetString("batch"); batch != "" {
			if len(args) != 1 {
				return usageError{errors.New("--batch takes the pairs from its file: give <file> alone, without <keep> or <drop>")}
			}
			return nil
		}
		return minArgs(3)(cmd, args)
	}
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
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
		pairs, err := mergePairs(cmd, args)
		if err != nil {
			return err
		}
		// One <keep> <drop> prints one report, as it always has; several
		// pairs, or any --batch, print one per pair, labeled.
		batch, _ := cmd.Flags().GetString("batch")
		many := batch != "" || len(pairs) > 1

		res, err := decodeForWrite(args[0])
		if err != nil {
			return err
		}
		doc := res.Document
		xrefs, err := checkPairs(doc, kind, pairs, many)
		if err != nil {
			return err
		}

		fold := false
		if kind.foldEvents {
			fold, _ = cmd.Flags().GetBool("fold-events")
		}
		reports := make([]*mergeReport, 0, len(xrefs))
		for i, pair := range xrefs {
			keep, drop := doc.GetRecord(pair[0]), doc.GetRecord(pair[1])
			merged, libReport, err := kind.merge(doc, keep.XRef, drop.XRef, merge.RecordMergeOptions{FoldEvents: fold})
			if err != nil {
				if many {
					return fmt.Errorf("%s: %w", pairs[i].label, err)
				}
				return err
			}
			hidden := hiddenRecords(doc, presumedLiving(doc, opts.cfg))
			report := newMergeReport(doc, kind, keep, libReport, opts.redact && (hidden[keep] || hidden[drop]))
			if kind.recordType == gedcom.RecordTypeIndividual {
				candidates := familyCandidates(merged, keep.XRef)
				report.Candidates = &candidates
			}
			reports = append(reports, report)
			doc = merged
		}

		if err := writeOutput(cmd, out, doc); err != nil {
			return err
		}
		w := reportWriter(cmd, out)
		if !many {
			if format == formatJSON {
				return writeJSON(w, reports[0])
			}
			return writeMergeText(w, reports[0], "")
		}
		if format == formatJSON {
			return writeJSON(w, reports)
		}
		for _, report := range reports {
			if err := writeMergeText(w, report, report.Keep+"\t"+report.Drop+"\t"); err != nil {
				return err
			}
		}
		return nil
	}
	addOutputFlag(cmd)
	cmd.Flags().String("batch", "", "tab-separated file of pairs to merge, one per line: keep, drop")
	if kind.foldEvents {
		cmd.Flags().Bool("fold-events", false, "fold an event of drop into keep's record of the same event, instead of adding it beside it")
	}
	return cmd
}

// newMergeReport is the library's report as the merge commands print it.
// keep is the kept record as it was before the merge, in doc: the merge
// functions never modify doc, they merge a copy. With redact, the
// substructures' lines are hidden.
func newMergeReport(doc *gedcom.Document, kind mergeKind, keep *gedcom.Record, r *merge.RecordMergeReport, redact bool) *mergeReport {
	held := map[string]bool{}
	for _, tag := range keep.Tags {
		if tag.Level == 1 {
			held[tag.Tag] = true
		}
	}
	report := &mergeReport{
		Keep:         r.Keep,
		Drop:         r.Drop,
		Moved:        make([]mergedPart, 0, len(r.Moved)),
		Folded:       make([]mergedFold, 0, len(r.Folded)),
		Deduplicated: make([]mergedPart, 0, len(r.Deduplicated)),
		Repointed:    mergeRefs(r.Repointed),
		Collapsed:    mergeRefs(r.Collapsed),
	}
	for _, s := range r.Moved {
		part := newMergedPart(s, redact)
		first := s.Tags[0]
		part.Conflict = held[first.Tag] && (!doc.IsPointerXRef(first.Value) || kind.onePointer[first.Tag])
		report.Moved = append(report.Moved, part)
	}
	for _, f := range r.Folded {
		report.Folded = append(report.Folded, mergedFold{
			Tag:   f.Drop.Tags[0].Tag,
			Lines: newMergedPart(f.Drop, redact).Lines,
			Into:  newMergedPart(f.Into, redact).Lines,
		})
	}
	for _, s := range r.Deduplicated {
		report.Deduplicated = append(report.Deduplicated, newMergedPart(s, redact))
	}
	if kind.dropped {
		dropped := make([]mergedConflict, 0, len(r.Conflicts))
		for _, c := range r.Conflicts {
			dropped = append(dropped, mergedConflict{
				Tag:   c.Drop.Tags[0].Tag,
				Lines: newMergedPart(c.Drop, redact).Lines,
				Kept:  newMergedPart(c.Keep, redact).Lines,
			})
		}
		report.Dropped = &dropped
	}
	return report
}

func newMergedPart(s merge.Substructure, redact bool) mergedPart {
	part := mergedPart{Tag: s.Tags[0].Tag}
	if redact {
		part.Lines = []string{redactedValue}
		return part
	}
	for _, tag := range s.Tags {
		line := strconv.Itoa(tag.Level)
		for _, field := range []string{tag.XRef, tag.Tag, tag.Value} {
			if field != "" {
				line += " " + field
			}
		}
		part.Lines = append(part.Lines, oneLine(line))
	}
	return part
}

func mergeRefs(refs []gedcom.Reference) []mergeRef {
	out := make([]mergeRef, 0, len(refs))
	for _, ref := range refs {
		out = append(out, mergeRef{XRef: ref.XRef, Path: strings.Join(ref.Path, ".")})
	}
	return out
}

// familyCandidates groups the families the individual xref is a spouse in,
// in doc, by the other spouse, keeping each partner with two or more.
func familyCandidates(doc *gedcom.Document, xref string) []familyCandidate {
	candidates := []familyCandidate{}
	ind := doc.GetIndividual(xref)
	if ind == nil {
		return candidates
	}
	byPartner := map[string]int{} // partner to index in all
	var all []familyCandidate
	seen := map[string]bool{}
	for _, link := range ind.SpouseInFamilies {
		fam := doc.GetFamily(link.FamilyXRef)
		if fam == nil || seen[fam.XRef] {
			continue
		}
		seen[fam.XRef] = true
		partner := fam.Husband
		if partner == xref {
			partner = fam.Wife
		}
		if partner == "" || partner == xref {
			continue
		}
		i, ok := byPartner[partner]
		if !ok {
			i = len(all)
			byPartner[partner] = i
			all = append(all, familyCandidate{Partner: partner})
		}
		all[i].Families = append(all[i].Families, fam.XRef)
	}
	for _, c := range all {
		if len(c.Families) > 1 {
			candidates = append(candidates, c)
		}
	}
	return candidates
}

// writeMergeText writes r's lines, each starting with prefix.
func writeMergeText(w io.Writer, r *mergeReport, prefix string) error {
	var lines []string
	for _, p := range r.Moved {
		kind := "moved"
		if p.Conflict {
			kind = "conflict"
		}
		lines = append(lines, kind+"\t"+p.Tag+"\t"+strings.Join(p.Lines, " | "))
	}
	for _, f := range r.Folded {
		lines = append(lines, "folded\t"+f.Tag+"\t"+strings.Join(f.Lines, " | ")+"\t"+strings.Join(f.Into, " | "))
	}
	if r.Dropped != nil {
		for _, c := range *r.Dropped {
			lines = append(lines, "dropped\t"+c.Tag+"\t"+strings.Join(c.Lines, " | ")+"\t"+strings.Join(c.Kept, " | "))
		}
	}
	for _, p := range r.Deduplicated {
		lines = append(lines, "deduplicated\t"+p.Tag+"\t"+strings.Join(p.Lines, " | "))
	}
	for _, ref := range r.Repointed {
		lines = append(lines, "repointed\t"+ref.XRef+"\t"+ref.Path)
	}
	for _, ref := range r.Collapsed {
		lines = append(lines, "collapsed\t"+ref.XRef+"\t"+ref.Path)
	}
	if r.Candidates != nil {
		for _, c := range *r.Candidates {
			lines = append(lines, "candidate\t"+c.Partner+"\t"+strings.Join(c.Families, "\t"))
		}
	}
	for _, line := range lines {
		if _, err := fmt.Fprintln(w, prefix+line); err != nil {
			return err
		}
	}
	return nil
}
