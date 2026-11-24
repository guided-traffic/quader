package vuln

import (
	"context"

	"github.com/guided-traffic/quader/internal/qname"
)

// The vulnerability verdicts of versions a remote repository has not stored (remote-curation.md R-CUR-009, R-CUR-014): what the upstream
// catalogue shows of a version before anything is fetched. Each verdict is the block the miss gate would compute for the version's
// download (R-VULN-015, Evaluator.EvaluateMiss): nothing is fetched and nothing is written.

// MissVerdict is the verdict of one version.
type MissVerdict struct {
	Version string
	// Block is the block the download would carry; the zero Block is none.
	Block Block
}

// MissVerdicts computes the verdicts with the Evaluator of the replica. Covers reports whether the scanning evaluates a format at all; a
// format it does not, or a replica without an Evaluator, is not covered.
type MissVerdicts struct {
	Evaluator Evaluator
	Covers    func(format string) bool
}

// Covered reports whether the scanning evaluates versions of the format.
func (m MissVerdicts) Covered(format string) bool {
	return m.Evaluator != nil && m.Covers != nil && m.Covers(format)
}

// Verdicts evaluates each version of the package name in the remote repository repo as a miss. covered is false for a format the
// scanning does not evaluate; then nothing is evaluated.
func (m MissVerdicts) Verdicts(ctx context.Context, repo qname.Repo, format, name string, versions []string) (out []MissVerdict, covered bool, err error) {
	if !m.Covered(format) {
		return nil, false, nil
	}
	out = make([]MissVerdict, 0, len(versions))
	for _, v := range versions {
		b, err := m.Evaluator.EvaluateMiss(ctx, Miss{Repo: repo, Format: format, Name: name, Version: v})
		if err != nil {
			return nil, true, err
		}
		out = append(out, MissVerdict{Version: v, Block: b})
	}
	return out, true, nil
}
