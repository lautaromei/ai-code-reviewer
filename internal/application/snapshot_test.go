package application

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"testing"
	"time"

	"github.com/lautaromei/ai-code-reviewer/internal/adapters/markdown"
	"github.com/lautaromei/ai-code-reviewer/internal/application/applicationtest"
	"github.com/lautaromei/ai-code-reviewer/internal/domain"
	"github.com/lautaromei/testigo"
	"github.com/lautaromei/testigo/assert"
)

func snapshotContentHash(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}

type snapshotScenario struct {
	store    *fakeSnapshotStore
	active   domain.Rule
	wantHash string
}

func newSnapshotScenario(t *testing.T) snapshotScenario {
	t.Helper()
	ctx := context.Background()
	store := newFakeSnapshotStore()
	now := time.Date(2026, 8, 8, 0, 0, 0, 0, time.UTC)
	active := applicationtest.ActiveRule.WithSnapshotRule("AIR-GO-001", "Avoid panics", "Do not panic.", domain.RuleActive, "", now).Bare()
	disabled := applicationtest.ActiveRule.WithSnapshotRule("AIR-GO-002", "Disabled", "body", domain.RuleDisabled, domain.DisabledDownvoteThreshold, now).Bare()
	applicationtest.NewRuleCatalogSeeder(t, store.rules, ctx).Many(active, disabled)
	return snapshotScenario{store: store, active: active, wantHash: snapshotContentHash(markdown.Render("AI review rules", []domain.Rule{active}))}
}

func (scenario snapshotScenario) generate(t *testing.T, runID int64) GenerateSnapshotSummary {
	t.Helper()
	summary, err := GenerateSnapshot(context.Background(), scenario.store.rules, scenario.store.snapshots, GenerateSnapshotInput{RunID: runID, Title: "AI review rules"})
	if err != nil {
		t.Fatal(err)
	}
	return summary
}

func TestSnapshotInputDefaults(t *testing.T) {
	testigo.Run(t, "uses the catalogue title", func(t *testing.T) {
		assert.Equal(t, NewGenerateSnapshotInput(GenerateSnapshotInput{}).Title, "AI review rules")
	})
}

func TestSnapshotConversation(t *testing.T) {
	testigo.Run(t, "addresses content by its hash", func(t *testing.T) {
		scenario := newSnapshotScenario(t)
		assert.Equal(t, scenario.generate(t, 1).Snapshot.Hash, scenario.wantHash)
	})

	testigo.Run(t, "contains only active rule identities", func(t *testing.T) {
		scenario := newSnapshotScenario(t)
		assert.Equal(t, scenario.generate(t, 1).Snapshot.RuleIDs, []string{scenario.active.RuleID})
	})

	testigo.Run(t, "persists the latest snapshot", func(t *testing.T) {
		scenario := newSnapshotScenario(t)
		scenario.generate(t, 1)
		latest, found, err := scenario.store.snapshots.Latest(context.Background())
		assert.NoError(t, err)
		assert.True(t, found, "snapshot should exist")
		assert.Equal(t, latest.Hash, scenario.wantHash)
	})

	testigo.Run(t, "hash is independent of run identity", func(t *testing.T) {
		scenario := newSnapshotScenario(t)
		first := scenario.generate(t, 7)
		second := scenario.generate(t, 8)
		assert.Equal(t, first.Snapshot.Hash, second.Snapshot.Hash)
	})
}
