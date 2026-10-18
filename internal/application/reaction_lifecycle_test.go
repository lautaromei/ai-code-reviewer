package application

import (
	"context"
	"github.com/lautaromei/ai-code-reviewer/internal/domain"
	"testing"
	"time"
)

type lifecycleRules struct {
	rules  []domain.Rule
	events []domain.RuleStatusEvent
}

func (r *lifecycleRules) ListByStatus(context.Context, domain.RuleStatus) ([]domain.Rule, error) {
	return r.rules, nil
}
func (r *lifecycleRules) UpdateStatus(_ context.Context, e domain.RuleStatusEvent) error {
	r.events = append(r.events, e)
	return nil
}

type votes int

func (v votes) CountByRule(context.Context, string, string) (int, error) { return int(v), nil }

type fixedClock struct{}

func (fixedClock) Now() time.Time { return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC) }
func TestThreeDownvotesDisableGlobally(t *testing.T) {
	r := &lifecycleRules{rules: []domain.Rule{{RuleID: "AIR-GO-001", Status: domain.RuleActive}}}
	s, e := EnforceRuleLifecycle(context.Background(), r, votes(3), fixedClock{}, RuleLifecycleInput{RunID: 1, MinThumbsDown: 3})
	if e != nil || len(s.RulesDisabled) != 1 || len(r.events) != 1 || r.events[0].Reason != domain.DisabledDownvoteThreshold {
		t.Fatalf("summary=%v events=%v error=%v", s, r.events, e)
	}
}
func TestUnusedRulesDoNotExpire(t *testing.T) {
	old := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	r := &lifecycleRules{rules: []domain.Rule{{RuleID: "AIR-GO-001", Status: domain.RuleActive, ActivatedAt: old}}}
	_, e := EnforceRuleLifecycle(context.Background(), r, votes(0), fixedClock{}, RuleLifecycleInput{ExpireAfter: time.Hour})
	if e != nil || len(r.events) != 0 {
		t.Fatalf("events=%v error=%v", r.events, e)
	}
}
