//go:generate go tool testigo-gen

package sqlitetest

import (
	"context"

	"github.com/lautaromei/ai-code-reviewer/internal/domain"
)

type RepositoryDirectory interface {
	Upsert(context.Context, domain.Repository) (domain.Repository, error)
}

type CommentArchive interface {
	Upsert(context.Context, domain.Comment) (domain.Comment, error)
}

type RuleCatalog interface {
	Insert(context.Context, domain.Rule) error
}

type RuleUsageHistory interface {
	RecordUsage(context.Context, domain.RuleUsage) error
}

type ClassificationHistory interface {
	Save(context.Context, domain.Classification) error
}

type CandidateGroups interface {
	Upsert(context.Context, domain.CandidateCluster) (domain.CandidateCluster, error)
}

type SnapshotArchive interface {
	Save(context.Context, domain.Snapshot) error
}

type PublicationJournal interface {
	Save(context.Context, domain.Publication) error
}

type testigoSpec struct {
	Rule               domain.Rule              `testigo:"fixture"`
	Repository         domain.Repository        `testigo:"fixture"`
	AIRuleUsageComment domain.Comment           `testigo:"fixture"`
	Comment            domain.Comment           `testigo:"fixture"`
	Classification     domain.Classification    `testigo:"fixture"`
	Cursor             domain.Cursor            `testigo:"fixture"`
	Reaction           domain.Reaction          `testigo:"fixture"`
	RuleUsage          domain.RuleUsage         `testigo:"fixture"`
	RuleStatusEvent    domain.RuleStatusEvent   `testigo:"fixture"`
	Candidate          domain.CandidateCluster  `testigo:"fixture"`
	CandidateEvidence  domain.CandidateEvidence `testigo:"fixture"`
	Snapshot           domain.Snapshot          `testigo:"fixture"`
	Publication        domain.Publication       `testigo:"fixture"`
	Run                domain.Run               `testigo:"fixture"`

	RepositoryDirectory       RepositoryDirectory   `testigo:"seeder=Upsert,stub=Repository"`
	RepositoryDirectoryErrors RepositoryDirectory   `testigo:"errorstub=Repository"`
	CommentArchive            CommentArchive        `testigo:"seeder=Upsert,stub=Comment"`
	CommentArchiveErrors      CommentArchive        `testigo:"errorstub=Comment"`
	RuleCatalog               RuleCatalog           `testigo:"seeder=Insert"`
	RuleUsageHistory          RuleUsageHistory      `testigo:"seeder=RecordUsage"`
	ClassificationHistory     ClassificationHistory `testigo:"seeder=Save"`
	CandidateGroups           CandidateGroups       `testigo:"seeder=Upsert,stub=Candidate"`
	CandidateGroupsErrors     CandidateGroups       `testigo:"errorstub=Candidate"`
	SnapshotArchive           SnapshotArchive       `testigo:"seeder=Save"`
	PublicationJournal        PublicationJournal    `testigo:"seeder=Save"`
}
