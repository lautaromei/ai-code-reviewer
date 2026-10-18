package application

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"sort"

	"github.com/lautaromei/ai-code-reviewer/internal/adapters/markdown"
	"github.com/lautaromei/ai-code-reviewer/internal/domain"
)

type GenerateSnapshotInput struct {
	RunID int64
	Title string
}

const defaultSnapshotTitle = "AI review rules"

// NewGenerateSnapshotInput creates snapshot input with its default title.
func NewGenerateSnapshotInput(input GenerateSnapshotInput) GenerateSnapshotInput {
	if input.Title == "" {
		input.Title = defaultSnapshotTitle
	}
	return input
}

type GenerateSnapshotSummary struct {
	Snapshot domain.Snapshot
}

// SnapshotRuleRepository is the rule dependency consumed by GenerateSnapshot.
type SnapshotRuleRepository interface {
	ListByStatus(context.Context, domain.RuleStatus) ([]domain.Rule, error)
}

// SnapshotRepository is the snapshot persistence dependency consumed by GenerateSnapshot.
type SnapshotRepository interface {
	Save(context.Context, domain.Snapshot) error
}

func GenerateSnapshot(ctx context.Context, rules SnapshotRuleRepository, snapshots SnapshotRepository, input GenerateSnapshotInput) (GenerateSnapshotSummary, error) {
	activeRules, err := rules.ListByStatus(ctx, domain.RuleActive)
	if err != nil {
		return GenerateSnapshotSummary{}, err
	}

	sort.Slice(activeRules, func(i, j int) bool { return activeRules[i].RuleID < activeRules[j].RuleID })

	ruleIDs := make([]string, 0, len(activeRules))
	for _, rule := range activeRules {
		ruleIDs = append(ruleIDs, rule.RuleID)
	}

	rendered := markdown.Render(input.Title, activeRules)
	sum := sha256.Sum256(rendered)
	hash := hex.EncodeToString(sum[:])

	snapshot := domain.Snapshot{
		Hash:    hash,
		RunID:   input.RunID,
		RuleIDs: ruleIDs,
	}

	if err := snapshots.Save(ctx, snapshot); err != nil {
		return GenerateSnapshotSummary{}, err
	}

	return GenerateSnapshotSummary{Snapshot: snapshot}, nil
}
