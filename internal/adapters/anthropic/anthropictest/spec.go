//go:generate go tool testigo-gen

package anthropictest

import "github.com/lautaromei/ai-code-reviewer/internal/application/ports"

type testigoSpec struct {
	CommentInput         ports.CommentInput         `testigo:"fixture"`
	CatalogEntry         ports.CatalogEntry         `testigo:"fixture"`
	CandidateEntry       ports.CandidateEntry       `testigo:"fixture"`
	ClassificationInput  ports.ClassificationInput  `testigo:"fixture"`
	CommentVerdict       ports.CommentVerdict       `testigo:"fixture"`
	ClassificationResult ports.ClassificationResult `testigo:"fixture"`
	CompactionInput      ports.CompactionInput      `testigo:"fixture"`
	CompactionMerge      ports.CompactionMerge      `testigo:"fixture"`
	CompactionResult     ports.CompactionResult     `testigo:"fixture"`
	DraftRuleInput       ports.DraftRuleInput       `testigo:"fixture"`
	RuleDraft            ports.RuleDraft            `testigo:"fixture"`
}
