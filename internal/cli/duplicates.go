package cli

import (
	"cmp"
	"fmt"
	"io"
	"maps"
	"math"
	"slices"
	"strconv"
	"strings"

	"github.com/cacack/gedcom-go/v3/gedcom"
	"github.com/cacack/gedcom-go/v3/validator"
	"github.com/spf13/cobra"
)

// duplicatePair is one candidate pair: an individual from each file, with the
// library's confidence score rounded to two decimals.
type duplicatePair struct {
	Score float64 `json:"score"`
	A     indiRow `json:"a"`
	B     indiRow `json:"b"`

	orderA, orderB int // each individual's position in its file, for sorting
}

func (p duplicatePair) fields() []string {
	return append(append([]string{strconv.FormatFloat(p.Score, 'f', 2, 64)}, p.A.fields()...), p.B.fields()...)
}

// duplicateGroup is a connected component of the pairs: everyone in A and B
// that the pairs join, directly or through another member, with those pairs.
type duplicateGroup struct {
	A     []string        `json:"a"`
	B     []string        `json:"b"`
	Pairs []duplicatePair `json:"pairs"`
}

func newDuplicatesCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "duplicates <file-a> <file-b>",
		Short: "Report people in one file who may be the same as people in another",
		Long: `Report candidate pairs of individuals, one from each file, that gedcom-go's
duplicate detection scores as likely the same person. The pairs are candidates
for a researcher to review, not conclusions: nothing is merged, neither file is
changed and no combined file is written.

Text output is one tab-separated line per pair with no header: score, then
xref, name, birth, death and living from file A, then the same from file B.
Xrefs are each file's own, so @I1@ on both sides can be two different people.
In JSON, each pair is an object with score, a and b, where a and b have list
indi's keys. Detection sees both files whole; --redact-living hides what the
pairs print about a presumed-living person.

The score runs from 0 to 1: the sum of the weights of what the two records
agree on. Surname 0.3; given name up to 0.3, by similarity; birth year 0.2,
or 0.1 within two years; sex 0.1, also when either side has none; full birth
date 0.04; death year 0.03; birth place 0.02; death place 0.01. Sex aside,
a value one record lacks earns nothing, just as when both lack it. Nothing
is subtracted: two different known sexes, or exact birth or death years more
than two apart, rule a pair out instead. By default only people with the same
surname are compared.

--relatives adds the people's relatives, read through the families they are a
spouse in: a spouse's name on both sides adds 0.2 and each child's name both
share adds 0.1, up to two, with the score capped at 1. People who share a
spouse's name are compared whatever their surnames, so a woman under her
married name in one file and her maiden name in the other is found, though
without the surname's weight.

Pairs are sorted by score, highest first, then by file order in A and in B.
The score is rounded to two decimals; --min-score drops pairs whose rounded
score is below it. The library bounds its work on large surname groups and long
given names; when it skipped or shortened any comparison, a warning on stderr
says so, and some pairs may be missing.

--groups reports groups instead of pairs: the connected components of the
pairs reported, so a person held twice in A and twice in B is one group of
four joined by up to four pairs, not four separate pairs. Each group lists
its members' xrefs in A and in B, in file order, and the pairs that join
them, in pair order. Groups are in the order of their first pair. In text, a
group is a line "group N: a <xrefs>; b <xrefs>" followed by its pairs, each
indented by two spaces and printed as without --groups. In JSON, each group is
an object with "a" and "b", arrays of xrefs, and "pairs", an array of pair
objects as without --groups.`,
		Example: `  gedcom duplicates old-export.ged family.ged
  gedcom duplicates --min-score 0.9 old-export.ged family.ged
  gedcom duplicates --format json old-export.ged family.ged | jq '.[] | [.a.xref, .b.xref]'
  gedcom duplicates --relatives old-export.ged family.ged
  gedcom duplicates --groups old-export.ged family.ged
  gedcom duplicates --groups --format json old-export.ged family.ged | jq -r '.[0].b[]' | gedcom relatives family.ged -`,
		Args: exactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			format, err := outputFormat(cmd)
			if err != nil {
				return err
			}
			minScore, err := cmd.Flags().GetFloat64("min-score")
			if err != nil {
				return err
			}
			if !(minScore >= 0 && minScore <= 1) {
				return usageError{fmt.Errorf("invalid --min-score %v: want a number from 0 to 1", minScore)}
			}
			groups, err := cmd.Flags().GetBool("groups")
			if err != nil {
				return err
			}
			relatives, err := cmd.Flags().GetBool("relatives")
			if err != nil {
				return err
			}
			opts, err := readLivingOptions(cmd)
			if err != nil {
				return err
			}
			a, err := decodeFile(args[0])
			if err != nil {
				return err
			}
			b, err := decodeFile(args[1])
			if err != nil {
				return err
			}
			// Detection sees the unredacted files; --redact-living then
			// redacts the individuals in place, so the pairs print redacted.
			report := findDuplicatesBetween(a, b, minScore, relatives)
			for _, issue := range report.LimitIssues {
				fmt.Fprintf(cmd.ErrOrStderr(), "gedcom: warning: %s (%s)\n", oneLine(issue.Message), issue.Code)
			}
			livingA, livingB := presumedLiving(a, opts.cfg), presumedLiving(b, opts.cfg)
			if opts.redact {
				if err := redactDocument(hiddenRecords(a, livingA)); err != nil {
					return err
				}
				if err := redactDocument(hiddenRecords(b, livingB)); err != nil {
					return err
				}
			}
			pairs := duplicatePairs(a, b, livingA, livingB, report.Pairs, minScore)
			if groups {
				if format == formatJSON {
					return writeJSON(cmd.OutOrStdout(), duplicateGroups(pairs))
				}
				return writeGroupsText(cmd.OutOrStdout(), duplicateGroups(pairs))
			}
			if format == formatJSON {
				return writeJSON(cmd.OutOrStdout(), pairs)
			}
			rows := make([]row, len(pairs))
			for i, p := range pairs {
				rows[i] = p
			}
			return writeRowsText(cmd.OutOrStdout(), rows)
		},
	}
	cmd.Flags().Bool("groups", false, "report groups of people joined by the pairs, instead of the pairs")
	cmd.Flags().Bool("relatives", false, "also score spouses' and children's names, and compare people who share a spouse's name")
	cmd.Flags().Float64("min-score", validator.DefaultDuplicateConfig().MinConfidence, "report only pairs scoring at least this, from 0 to 1")
	return cmd
}

// findDuplicatesBetween runs the library's cross-document duplicate detection
// with its default configuration, reading relatives when asked. The library
// gates on the unrounded score, so it is asked for a little below minScore;
// duplicatePairs then applies minScore to the rounded score that is printed.
func findDuplicatesBetween(a, b *gedcom.Document, minScore float64, relatives bool) validator.DuplicateReport {
	config := validator.DefaultDuplicateConfig()
	config.CompareRelatives = relatives
	config.MinConfidence = minScore - scoreSlack
	if config.MinConfidence <= 0 {
		// The library reads 0 as "use the default"; ZeroThreshold means 0.
		config.MinConfidence = validator.ZeroThreshold
	}
	return validator.NewDuplicateDetector(config).FindDuplicatesBetweenReport(a, b)
}

// scoreSlack is more than the most that rounding to two decimals raises a
// score, so every pair that rounds up to minScore reaches duplicatePairs.
const scoreSlack = 0.01

// roundScore is the score as printed: rounded to two decimals.
func roundScore(confidence float64) float64 { return math.Round(confidence*100) / 100 }

// duplicatePairs keeps the library's pairs whose rounded score is at least
// minScore and sorts them by that score and then by file order, so that output
// is stable and agrees with the printed scores. The result is never nil, so
// that no pairs is [] in JSON. livingA and livingB are the individuals presumed
// living in each file.
func duplicatePairs(a, b *gedcom.Document, livingA, livingB map[*gedcom.Individual]bool, found []validator.DuplicatePair, minScore float64) []duplicatePair {
	orderA, orderB := fileOrder(a), fileOrder(b)
	pairs := make([]duplicatePair, 0, len(found))
	for _, p := range found {
		score := roundScore(p.Confidence)
		if score < minScore {
			continue
		}
		pairs = append(pairs, duplicatePair{
			Score:  score,
			A:      indiRowOf(p.Individual1, livingA[p.Individual1]),
			B:      indiRowOf(p.Individual2, livingB[p.Individual2]),
			orderA: orderA[p.Individual1],
			orderB: orderB[p.Individual2],
		})
	}
	slices.SortStableFunc(pairs, func(x, y duplicatePair) int {
		return cmp.Or(
			cmp.Compare(y.Score, x.Score),
			cmp.Compare(x.orderA, y.orderA),
			cmp.Compare(x.orderB, y.orderB),
		)
	})
	return pairs
}

// fileOrder maps each individual in doc to its position in the file.
func fileOrder(doc *gedcom.Document) map[*gedcom.Individual]int {
	individuals := doc.Individuals()
	order := make(map[*gedcom.Individual]int, len(individuals))
	for i, ind := range individuals {
		order[ind] = i
	}
	return order
}

// duplicateGroups gathers pairs, in their sorted order, into the connected
// components they form. A group's members are in file order and its pairs in
// the order given; groups are in the order of their first pair. The result is
// never nil, so that no groups is [] in JSON.
func duplicateGroups(pairs []duplicatePair) []duplicateGroup {
	// Union-find over the members, keyed by side and position in the file.
	type member struct {
		b     bool
		order int
	}
	parent := map[member]member{}
	var find func(m member) member
	find = func(m member) member {
		p, ok := parent[m]
		if !ok || p == m {
			return m
		}
		root := find(p)
		parent[m] = root
		return root
	}
	for _, p := range pairs {
		ra, rb := find(member{false, p.orderA}), find(member{true, p.orderB})
		parent[ra], parent[rb] = ra, ra
	}
	groups := []duplicateGroup{}
	index := map[member]int{} // root to its group in groups
	membersA, membersB := map[int]map[int]string{}, map[int]map[int]string{}
	for _, p := range pairs {
		root := find(member{false, p.orderA})
		i, ok := index[root]
		if !ok {
			i = len(groups)
			index[root] = i
			groups = append(groups, duplicateGroup{})
			membersA[i], membersB[i] = map[int]string{}, map[int]string{}
		}
		groups[i].Pairs = append(groups[i].Pairs, p)
		membersA[i][p.orderA] = p.A.XRef
		membersB[i][p.orderB] = p.B.XRef
	}
	inFileOrder := func(members map[int]string) []string {
		orders := slices.Sorted(maps.Keys(members))
		xrefs := make([]string, len(orders))
		for k, order := range orders {
			xrefs[k] = members[order]
		}
		return xrefs
	}
	for i := range groups {
		groups[i].A, groups[i].B = inFileOrder(membersA[i]), inFileOrder(membersB[i])
	}
	return groups
}

// writeGroupsText writes each group's header line and then its pairs,
// indented.
func writeGroupsText(w io.Writer, groups []duplicateGroup) error {
	for n, g := range groups {
		if _, err := fmt.Fprintf(w, "group %d: a %s; b %s\n", n+1, oneLine(strings.Join(g.A, " ")), oneLine(strings.Join(g.B, " "))); err != nil {
			return err
		}
		for _, p := range g.Pairs {
			if _, err := io.WriteString(w, "  "); err != nil {
				return err
			}
			if err := writeRowsText(w, []row{p}); err != nil {
				return err
			}
		}
	}
	return nil
}
