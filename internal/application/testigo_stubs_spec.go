//go:build testigo_bootstrap

package application

import "github.com/lautaromei/ai-code-reviewer/internal/domain"

//go:generate go tool testigo-gen -output zz_testigo_stubs_gen_test.go -type testigoStubsSpec
type testigoStubsSpec struct {
	Repository domain.Repository `testigo:"fixture"`
	Comment    domain.Comment    `testigo:"fixture"`
	ActiveRule domain.Rule       `testigo:"fixture"`

	ExportRepositories       ExportRepositoryRepository  `testigo:"stub=Repository,collections=one"`
	ExportRepositoriesErrors ExportRepositoryRepository  `testigo:"errorstub=Repository,collections=one"`
	ExportComments           ExportCommentRepository     `testigo:"stub=Comment,collections=one"`
	ExportCommentsErrors     ExportCommentRepository     `testigo:"errorstub=Comment,collections=one"`
	TrackedComments          TrackedCommentsRepository   `testigo:"stub=Comment,collections=one"`
	TrackedCommentsErrors    TrackedCommentsRepository   `testigo:"errorstub=Comment,collections=one"`
	ReactionSinkErrors       ReactionSinkRepository      `testigo:"errorstub"`
	SnapshotRules            SnapshotRuleRepository      `testigo:"stub=ActiveRule,collections=one"`
	SnapshotRulesErrors      SnapshotRuleRepository      `testigo:"errorstub=ActiveRule,collections=one"`
	SnapshotArchiveErrors    SnapshotRepository          `testigo:"errorstub"`
	LifecycleFeedbackErrors  LifecycleReactionRepository `testigo:"errorstub"`
}
