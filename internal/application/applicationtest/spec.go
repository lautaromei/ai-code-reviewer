//go:generate go tool testigo-gen

package applicationtest

import (
	"context"

	"github.com/lautaromei/ai-code-reviewer/internal/application/ports"
	"github.com/lautaromei/ai-code-reviewer/internal/domain"
)

type RepositoryDirectory interface {
	Upsert(context.Context, domain.Repository) (domain.Repository, error)
}

type CommentArchive interface {
	Upsert(context.Context, domain.Comment) (domain.Comment, error)
}

type CommentCursor interface {
	Advance(context.Context, domain.Cursor) error
}

type RuleCatalog interface {
	Insert(context.Context, domain.Rule) error
}

type SeedCatalog interface {
	Get(context.Context, string) (domain.Rule, bool, error)
	Insert(context.Context, domain.Rule) error
}

type RuleUsageHistory interface {
	RecordUsage(context.Context, domain.RuleUsage) error
}

type PublicationJournal interface {
	Save(context.Context, domain.Publication) error
}

type ClassificationQueue interface {
	GetByID(context.Context, int64) (domain.Comment, bool, error)
	ListPendingClassification(context.Context, string) ([]domain.Comment, error)
}

type ClassificationHistory interface {
	Save(context.Context, domain.Classification) error
}

type CandidateGroups interface {
	Upsert(context.Context, domain.CandidateCluster) (domain.CandidateCluster, error)
	Get(context.Context, string) (domain.CandidateCluster, bool, error)
	ListOpen(context.Context, string) ([]domain.CandidateCluster, error)
	ListMergedInto(context.Context, string) ([]domain.CandidateCluster, error)
	ListEvidence(context.Context, string) ([]domain.CandidateEvidence, error)
	AttachEvidence(context.Context, domain.CandidateEvidence) error
	Merge(context.Context, string, []string) error
	Promote(context.Context, string, string) error
}

type RuleFeedback interface {
	CountByComment(context.Context, int64, string) (int, error)
}

type PromotionRuleCatalog interface {
	Insert(context.Context, domain.Rule) error
	Get(context.Context, string) (domain.Rule, bool, error)
	ListByStatus(context.Context, domain.RuleStatus) ([]domain.Rule, error)
	NextSequence(context.Context, string) (int, error)
}

type testigoSpec struct {
	ActiveRule           domain.Rule                `testigo:"fixture,memdb=RuleID"`
	Repository           domain.Repository          `testigo:"fixture,memdb=FullName"`
	Comment              domain.Comment             `testigo:"fixture,memdb=ID"`
	TrackedAIComment     domain.Comment             `testigo:"fixture"`
	Reaction             domain.Reaction            `testigo:"fixture,memdb=ID"`
	Cursor               domain.Cursor              `testigo:"fixture"`
	RuleStatusEvent      domain.RuleStatusEvent     `testigo:"fixture"`
	Candidate            domain.CandidateCluster    `testigo:"fixture"`
	CandidateEvidence    domain.CandidateEvidence   `testigo:"fixture"`
	Classification       domain.Classification      `testigo:"fixture"`
	RuleUsage            domain.RuleUsage           `testigo:"fixture"`
	Snapshot             domain.Snapshot            `testigo:"fixture"`
	Publication          domain.Publication         `testigo:"fixture"`
	Run                  domain.Run                 `testigo:"fixture"`
	ReviewComment        ports.ReviewComment        `testigo:"fixture"`
	ReactionRecord       ports.ReactionRecord       `testigo:"fixture"`
	ReactionPage         ports.ReactionPage         `testigo:"fixture"`
	Language             ports.Language             `testigo:"fixture"`
	PublicationRequest   ports.PublicationRequest   `testigo:"fixture"`
	PublicationResult    ports.PublicationResult    `testigo:"fixture"`
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
	GitHub               ports.CodeReviewGateway    `testigo:"spy"`
	Classifier           ports.Classifier           `testigo:"spy"`

	RepositoryDirectory         RepositoryDirectory   `testigo:"spy,seeder=Upsert,stub=Repository"`
	RepositoryDirectoryErrors   RepositoryDirectory   `testigo:"errorstub=Repository"`
	CommentArchive              CommentArchive        `testigo:"spy,seeder=Upsert,stub=Comment"`
	CommentArchiveErrors        CommentArchive        `testigo:"errorstub=Comment"`
	CommentCursor               CommentCursor         `testigo:"seeder=Advance"`
	CommentCursorErrors         CommentCursor         `testigo:"errorstub"`
	RuleCatalog                 RuleCatalog           `testigo:"seeder=Insert"`
	RuleCatalogErrors           RuleCatalog           `testigo:"errorstub"`
	SeedCatalog                 SeedCatalog           `testigo:"spy,stub=ActiveRule,found=true"`
	SeedCatalogErrors           SeedCatalog           `testigo:"errorstub=ActiveRule,found=true"`
	RuleUsageHistory            RuleUsageHistory      `testigo:"seeder=RecordUsage"`
	RuleUsageHistoryErrors      RuleUsageHistory      `testigo:"errorstub"`
	PublicationJournal          PublicationJournal    `testigo:"seeder=Save"`
	PublicationJournalErrors    PublicationJournal    `testigo:"errorstub"`
	ClassificationQueue         ClassificationQueue   `testigo:"spy,stub=Comment,collections=one,found=true"`
	ClassificationQueueErrors   ClassificationQueue   `testigo:"errorstub=Comment,collections=one,found=true"`
	ClassificationHistory       ClassificationHistory `testigo:"spy"`
	ClassificationHistoryErrors ClassificationHistory `testigo:"errorstub"`
	CandidateGroups             CandidateGroups       `testigo:"spy,stub=Candidate,collections=one,found=true"`
	CandidateGroupsErrors       CandidateGroups       `testigo:"errorstub=Candidate,collections=one,found=true"`
	RuleFeedback                RuleFeedback          `testigo:"spy"`
	PromotionRuleCatalog        PromotionRuleCatalog  `testigo:"spy,stub=ActiveRule,collections=one,found=true"`
	PromotionRuleCatalogErrors  PromotionRuleCatalog  `testigo:"errorstub=ActiveRule,collections=one,found=true"`
}
