package cli

import (
	"cmp"
	"fmt"
	"math"
	"slices"
	"strconv"

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

Pairs are sorted by score, highest first, then by file order in A and in B.
The score runs from 0 to 1 and is rounded to two decimals; --min-score drops
pairs whose rounded score is below it. The library bounds its work on large surname groups and long
given names; when it skipped or shortened any comparison, a warning on stderr
says so, and some pairs may be missing.`,
		Example: `  gedcom duplicates old-export.ged family.ged
  gedcom duplicates --min-score 0.9 old-export.ged family.ged
  gedcom duplicates --format json old-export.ged family.ged | jq '.[] | [.a.xref, .b.xref]'`,
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
			report := findDuplicatesBetween(a, b, minScore)
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
	cmd.Flags().Float64("min-score", validator.DefaultDuplicateConfig().MinConfidence, "report only pairs scoring at least this, from 0 to 1")
	return cmd
}

// findDuplicatesBetween runs the library's cross-document duplicate detection
// with its default configuration. The library gates on the unrounded score, so
// it is asked for a little below minScore; duplicatePairs then applies minScore
// to the rounded score that is printed.
func findDuplicatesBetween(a, b *gedcom.Document, minScore float64) validator.DuplicateReport {
	config := validator.DefaultDuplicateConfig()
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
