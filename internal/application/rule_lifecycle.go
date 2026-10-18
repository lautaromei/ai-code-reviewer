package application

import (
	"context"
	"fmt"
	"time"

	"github.com/lautaromei/ai-code-reviewer/internal/application/ports"
	"github.com/lautaromei/ai-code-reviewer/internal/domain"
)

const lifecycleExpireAfter = 180 * 24 * time.Hour // retained for historical API compatibility; no expiry is applied

type RuleLifecycleInput struct {
	RunID         int64
	MinThumbsDown int
	ExpireAfter   time.Duration
}

type RuleLifecycleSummary struct {
	RulesDisabled []string
}

type lifecycleRuleRepository interface {
	ListByStatus(context.Context, domain.RuleStatus) ([]domain.Rule, error)
	UpdateStatus(context.Context, domain.RuleStatusEvent) error
}

// LifecycleReactionRepository is the feedback dependency consumed by EnforceRuleLifecycle.
type LifecycleReactionRepository interface {
	CountByRule(context.Context, string, string) (int, error)
}

func EnforceRuleLifecycle(ctx context.Context, rules lifecycleRuleRepository, reactions LifecycleReactionRepository, clock ports.Clock, input RuleLifecycleInput) (RuleLifecycleSummary, error) {
	activeRules, err := rules.ListByStatus(ctx, domain.RuleActive)
	if err != nil {
		return RuleLifecycleSummary{}, err
	}

	now := clock.Now()
	summary := RuleLifecycleSummary{}

	for _, rule := range activeRules {
		downvotes, err := reactions.CountByRule(ctx, rule.RuleID, "-1")
		if err != nil {
			return RuleLifecycleSummary{}, err
		}

		if input.MinThumbsDown > 0 && downvotes >= input.MinThumbsDown {
			event := domain.RuleStatusEvent{
				EventKey:   fmt.Sprintf("%s:disabled:%d", rule.RuleID, input.RunID),
				RuleID:     rule.RuleID,
				FromState:  domain.RuleActive,
				ToState:    domain.RuleDisabled,
				Reason:     domain.DisabledDownvoteThreshold,
				RunID:      input.RunID,
				OccurredAt: now,
			}
			if err := rules.UpdateStatus(ctx, event); err != nil {
				return RuleLifecycleSummary{}, err
			}
			summary.RulesDisabled = append(summary.RulesDisabled, rule.RuleID)
			continue
		}

	}

	return summary, nil
}
