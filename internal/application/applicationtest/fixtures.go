//go:build !testigo_bootstrap

package applicationtest

import (
	"time"

	"github.com/lautaromei/testigo/fixture"

	"github.com/lautaromei/ai-code-reviewer/internal/application/ports"
	"github.com/lautaromei/ai-code-reviewer/internal/domain"
)

func (b activeRuleBuilder) With(variations ...fixture.Variation[domain.Rule]) activeRuleBuilder {
	b.Builder = b.Builder.With(variations...)
	return b
}

func (b activeRuleBuilder) WithCatalogRule(at time.Time) activeRuleBuilder {
	return b.With(WithCatalogRule(at))
}

func (b activeRuleBuilder) WithActiveRuleIdentity(ruleID string, at time.Time) activeRuleBuilder {
	return b.With(WithActiveRuleIdentity(ruleID, at))
}

func (b activeRuleBuilder) WithRuleLifecycle(ruleID string, status domain.RuleStatus, reason domain.DisabledReason, createdAt, lastUsedAt time.Time, disabledAt *time.Time) activeRuleBuilder {
	return b.With(WithRuleLifecycle(ruleID, status, reason, createdAt, lastUsedAt, disabledAt))
}

func (b activeRuleBuilder) WithPublishRule(ruleID, language string, at time.Time) activeRuleBuilder {
	return b.With(WithPublishRule(ruleID, language, at))
}

func (b activeRuleBuilder) WithSnapshotRule(id, title, body string, status domain.RuleStatus, reason domain.DisabledReason, at time.Time) activeRuleBuilder {
	return b.With(WithSnapshotRule(id, title, body, status, reason, at))
}

type Clock struct{ Time time.Time }

func (c Clock) Now() time.Time { return c.Time }

func WithHumanComment(githubID int64, language, inputHash string, createdAt time.Time) fixture.Variation[domain.Comment] {
	return func(comment domain.Comment) domain.Comment {
		comment.ID = 0
		comment.GitHubCommentID, comment.RepositoryID = githubID, 1
		comment.Author, comment.AuthorKind, comment.Origin = "reviewer", domain.AuthorHuman, domain.OriginHuman
		comment.Body = "group imports"
		comment.Language, comment.InputHash, comment.CreatedAt = language, inputHash, createdAt
		return comment
	}
}

func WithCatalogRule(createdAt time.Time) fixture.Variation[domain.Rule] {
	return func(rule domain.Rule) domain.Rule {
		rule.RuleID, rule.Title, rule.CreatedAt, rule.ActivatedAt = "AIR-GO-001", "Format with gofmt", createdAt, createdAt
		rule.Language, rule.Status, rule.DisabledReason = "go", domain.RuleActive, ""
		return rule
	}
}

func WithSkippedComment(githubID int64, origin domain.CommentOrigin, createdAt time.Time) fixture.Variation[domain.Comment] {
	return func(comment domain.Comment) domain.Comment {
		comment.ID = 0
		comment.GitHubCommentID, comment.RepositoryID = githubID, 1
		comment.Author, comment.AuthorKind = "reviewer", domain.AuthorHuman
		comment.Origin, comment.Language, comment.InputHash, comment.CreatedAt = origin, "go", "hash-skip", createdAt
		return comment
	}
}

func WithCommentRepositoryAndTimes(repositoryID int64, at time.Time) fixture.Variation[domain.Comment] {
	return func(comment domain.Comment) domain.Comment {
		comment.ID = 0
		comment.GitHubCommentID = 10
		comment.RepositoryID, comment.CreatedAt, comment.UpdatedAt = repositoryID, at, at
		comment.Author, comment.AuthorKind, comment.Origin = "ai", domain.AuthorAI, domain.OriginAIRuleUsage
		return comment
	}
}

func WithActiveRuleIdentity(ruleID string, at time.Time) fixture.Variation[domain.Rule] {
	return func(rule domain.Rule) domain.Rule {
		rule.RuleID, rule.CreatedAt, rule.ActivatedAt = ruleID, at, at
		rule.Language, rule.Status, rule.DisabledReason = "go", domain.RuleActive, ""
		return rule
	}
}

func WithRuleLifecycle(ruleID string, status domain.RuleStatus, reason domain.DisabledReason, createdAt, lastUsedAt time.Time, disabledAt *time.Time) fixture.Variation[domain.Rule] {
	return func(rule domain.Rule) domain.Rule {
		rule.RuleID, rule.Status, rule.DisabledReason = ruleID, status, reason
		rule.Language = "go"
		rule.CreatedAt, rule.ActivatedAt, rule.LastUsedAt, rule.DisabledAt = createdAt, createdAt, &lastUsedAt, disabledAt
		return rule
	}
}

func WithReactionDetails(reactionID, commentID int64, content, user string, at time.Time) fixture.Variation[domain.Reaction] {
	return func(reaction domain.Reaction) domain.Reaction {
		reaction.GitHubReactionID, reaction.CommentID = reactionID, commentID
		reaction.User, reaction.Content, reaction.CreatedAt = user, content, at
		return reaction
	}
}

func AsHumanReviewComment(at time.Time) fixture.Variation[ports.ReviewComment] {
	return func(comment ports.ReviewComment) ports.ReviewComment {
		comment.GitHubCommentID, comment.Author, comment.AuthorIsBot = 1, "reviewer", false
		comment.Body, comment.Path, comment.InReplyToID = "please fix this", "main.go", nil
		comment.CreatedAt, comment.UpdatedAt = at, at
		return comment
	}
}

func AsReply(parentID int64) fixture.Variation[ports.ReviewComment] {
	return func(comment ports.ReviewComment) ports.ReviewComment {
		comment.GitHubCommentID, comment.InReplyToID = 2, &parentID
		comment.Author, comment.Body = "reviewer2", "a reply"
		return comment
	}
}

func AsAIRuleUsage(at time.Time) fixture.Variation[ports.ReviewComment] {
	return func(comment ports.ReviewComment) ports.ReviewComment {
		comment.GitHubCommentID, comment.InReplyToID = 1, nil
		comment.Author, comment.AuthorIsBot = "coderabbitai[bot]", true
		comment.Body, comment.Path = "AIR-GO-001 applies here.", "main.go"
		comment.CreatedAt, comment.UpdatedAt = at, at
		return comment
	}
}

func WithPublishRule(ruleID, language string, at time.Time) fixture.Variation[domain.Rule] {
	return func(rule domain.Rule) domain.Rule {
		rule.RuleID, rule.Language, rule.Title, rule.Body = ruleID, language, ruleID, "body de "+ruleID
		rule.Status, rule.DisabledReason = domain.RuleActive, ""
		rule.CreatedAt, rule.ActivatedAt = at, at
		return rule
	}
}

func WithSnapshotRule(id, title, body string, status domain.RuleStatus, reason domain.DisabledReason, at time.Time) fixture.Variation[domain.Rule] {
	return func(rule domain.Rule) domain.Rule {
		rule.RuleID, rule.Title, rule.Body = id, title, body
		rule.Language = "go"
		rule.Status, rule.DisabledReason, rule.CreatedAt, rule.ActivatedAt = status, reason, at, at
		return rule
	}
}
