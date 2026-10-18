//go:build !testigo_bootstrap

package anthropictest

import (
	"github.com/lautaromei/ai-code-reviewer/internal/application/ports"
	"github.com/lautaromei/ai-code-reviewer/internal/domain"
)

func GoComment(id int64, body string) ports.CommentInput {
	return CommentInput.WithCommentID(id).WithLanguage("go").WithBody(body).Bare()
}

func ActiveCatalogEntry(ruleID, title string) ports.CatalogEntry {
	return CatalogEntry.WithRuleID(ruleID).WithTitle(title).WithStatus(domain.RuleActive).Bare()
}

func ClassificationRequest(comments []ports.CommentInput, catalog []ports.CatalogEntry) ports.ClassificationInput {
	return ClassificationInput.WithPromptVersion("v1").WithComments(comments).WithCatalog(catalog).Bare()
}

func CompactionRequest(candidates []ports.CandidateEntry) ports.CompactionInput {
	return CompactionInput.WithLanguage("go").WithCandidates(candidates).Bare()
}

func Candidate(id, label string) ports.CandidateEntry {
	return CandidateEntry.WithCandidateID(id).WithLabel(label).Bare()
}

func RuleDraftRequest(evidence []ports.CommentInput) ports.DraftRuleInput {
	return DraftRuleInput.WithLanguage("go").WithEvidence(evidence).Bare()
}
