package cli

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/cacack/gedcom-go/v3/gedcom"
	"github.com/cacack/gedcom-go/v3/merge"
	"github.com/spf13/cobra"
)

// mergeKind is what merge-people and merge-families share: the record type
// they merge, the library call that merges it, and the pointer tags the
// record holds one of, so that a second is a conflict.
type mergeKind struct {
	recordType gedcom.RecordType
	noun       string
	merge      func(*gedcom.Document, string, string, merge.RecordMergeOptions) (*gedcom.Document, *merge.RecordMergeReport, error)
	onePointer map[string]bool
}

var (
	mergePeople   = mergeKind{gedcom.RecordTypeIndividual, "an individual", merge.Individuals, nil}
	mergeFamilies = mergeKind{gedcom.RecordTypeFamily, "a family", merge.Families, map[string]bool{"HUSB": true, "WIFE": true}}
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
	Deduplicated []mergedPart `json:"deduplicated"`
	Repointed    []mergeRef   `json:"repointed"`
	Collapsed    []mergeRef   `json:"collapsed"`
	// Candidates is set by merge-people only.
	Candidates *[]familyCandidate `json:"candidates,omitempty"`
}

func newMergePeopleCmd() *cobra.Command {
	return newMergeCmd(mergePeople, &cobra.Command{
		Use:   "merge-people <file> <keep> <drop> -o <out>",
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

The report lists, one per line and tab-separated:

  moved         tag, lines    a substructure of drop now under keep
  conflict      tag, lines    moved, but keep already held that tag, with
                              other lines and a value that is not a pointer:
                              one fact stated two ways (two births) or two
                              facts (two occupations); check which
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
  gedcom merge-people --format json family.ged I1 I7 -o merged.ged | jq '.moved[] | select(.conflict)'
  gedcom merge-families merged.ged @F1@ @F4@ -o merged2.ged`,
	})
}

func newMergeFamiliesCmd() *cobra.Command {
	return newMergeCmd(mergeFamilies, &cobra.Command{
		Use:   "merge-families <file> <keep> <drop> -o <out>",
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

The report is merge-people's, without candidates: moved, conflict,
deduplicated, repointed and collapsed lines. A HUSB or WIFE that differs from
keep's is moved, and reported as a conflict. Run 'gedcom validate' on the
output to check the result.`,
		Example: `  gedcom merge-families family.ged @F1@ @F4@ -o merged.ged
  gedcom merge-families --format json family.ged F1 F4 -o merged.ged`,
	})
}

// newMergeCmd completes cmd, which holds the command's help, as a merge of
// kind's records.
func newMergeCmd(kind mergeKind, cmd *cobra.Command) *cobra.Command {
	cmd.Args = exactArgs(3)
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		format, err := outputFormat(cmd)
		if err != nil {
			return err
		}
		opts, err := readLivingOptions(cmd)
		if err != nil {
			return err
		}
		out, _ := cmd.Flags().GetString("output")
		if out == "" {
			return usageError{errors.New("--output is required")}
		}
		if err := checkNotInput(args[0], out); err != nil {
			return err
		}

		res, err := decodeForWrite(args[0])
		if err != nil {
			return err
		}
		doc := res.Document
		records, err := findRecords(doc, args[1:])
		if err != nil {
			return err
		}
		keep, drop := records[0], records[1]
		if keep == drop {
			return usageError{fmt.Errorf("keep and drop are both %s; name two records", keep.XRef)}
		}
		for _, record := range records {
			if record.Type != kind.recordType {
				return fmt.Errorf("%s is a %s record; want %s", record.XRef, record.Type, kind.noun)
			}
		}

		merged, libReport, err := kind.merge(doc, keep.XRef, drop.XRef, merge.RecordMergeOptions{})
		if err != nil {
			return err
		}
		hidden := hiddenRecords(doc, presumedLiving(doc, opts.cfg))
		report := newMergeReport(doc, kind, keep, libReport, opts.redact && (hidden[keep] || hidden[drop]))
		if kind.recordType == gedcom.RecordTypeIndividual {
			candidates := familyCandidates(merged, keep.XRef)
			report.Candidates = &candidates
		}

		var buf bytes.Buffer
		if err := encodeDocument(&buf, merged); err != nil {
			return err
		}
		if err := os.WriteFile(out, buf.Bytes(), 0o644); err != nil {
			return err
		}
		if format == formatJSON {
			return writeJSON(cmd.OutOrStdout(), report)
		}
		return writeMergeText(cmd.OutOrStdout(), report)
	}
	cmd.Flags().StringP("output", "o", "", "file to write the result to (required; never the input)")
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
	for _, s := range r.Deduplicated {
		report.Deduplicated = append(report.Deduplicated, newMergedPart(s, redact))
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

func writeMergeText(w io.Writer, r *mergeReport) error {
	var lines []string
	for _, p := range r.Moved {
		kind := "moved"
		if p.Conflict {
			kind = "conflict"
		}
		lines = append(lines, kind+"\t"+p.Tag+"\t"+strings.Join(p.Lines, " | "))
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
		if _, err := fmt.Fprintln(w, line); err != nil {
			return err
		}
	}
	return nil
}
