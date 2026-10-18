package application

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/lautaromei/ai-code-reviewer/internal/adapters/fake"
	"github.com/lautaromei/ai-code-reviewer/internal/application/applicationtest"
	"github.com/lautaromei/ai-code-reviewer/internal/application/ports"
	"github.com/lautaromei/ai-code-reviewer/internal/domain"
	"github.com/lautaromei/testigo"
	"github.com/lautaromei/testigo/assert"
)

func syncFixtureDir(t *testing.T) string {
	t.Helper()
	directory := t.TempDir()
	writeFixture(t, directory, "go.md",
		"## AIR-GO-001 — Grouped and ordered imports\n"+
			"- **Scope:** `**/*.go`\n- **Status:** active · high · 2026-07\n- **Rule:** Group imports.\n\n"+
			"## AIR-GO-002 — Idiomatic naming\n"+
			"- **Scope:** `**/*.go`\n- **Status:** active · high · 2026-07\n- **Rule:** Avoid repeated names.\n")
	return directory
}

func syncDeps(store *fakeSyncStore, clock ports.Clock) SyncDeps {
	return SyncDeps{
		GitHub: fake.NewGitHub(), Classifier: fake.NewClassifier(),
		Repositories: store.repositories, Cursors: store.cursors, Comments: store.comments,
		Reactions: store.feedback, Rules: store.rules, Candidates: store.candidates,
		Classifications: store.classifications, Runs: store.runs, Snapshots: store.snapshots,
		Publications: store.publications, Clock: clock,
	}
}

func TestSyncValidation(t *testing.T) {
	testigo.Run(t, "rejects missing dependencies", func(t *testing.T) {
		_, err := Sync(context.Background(), SyncDeps{}, SyncOptions{})
		assert.Error(t, err)
	})

	testigo.Run(t, "rejects a nil context", func(t *testing.T) {
		store := newFakeSyncStore()
		var nilContext context.Context
		_, err := Sync(nilContext, syncDeps(store, applicationtest.Clock{Time: time.Now()}), SyncOptions{})
		assert.Error(t, err)
	})

	testigo.Run(t, "an empty seed directory reports sync", func(t *testing.T) {
		store := newFakeSyncStore()
		summary, err := Sync(context.Background(), syncDeps(store, applicationtest.Clock{Time: time.Now()}), SyncOptions{})
		assert.NoError(t, err)
		assert.Equal(t, summary.Command, "sync")
		assert.False(t, summary.DryRun, "sync should apply changes")
	})

	testigo.Run(t, "an empty seed directory stores no rules", func(t *testing.T) {
		store := newFakeSyncStore()
		if _, err := Sync(context.Background(), syncDeps(store, applicationtest.Clock{Time: time.Now()}), SyncOptions{}); err != nil {
			t.Fatal(err)
		}
		rules, err := store.rules.ListByStatus(context.Background(), domain.RuleActive)
		assert.NoError(t, err)
		assert.Empty(t, rules)
	})
}

type repeatedSyncScenario struct {
	store  *fakeSyncStore
	first  Summary
	second Summary
	rules  []domain.Rule
}

func runRepeatedSync(t *testing.T) repeatedSyncScenario {
	t.Helper()
	store := newFakeSyncStore()
	deps := syncDeps(store, applicationtest.Clock{Time: time.Date(2026, 8, 6, 0, 0, 0, 0, time.UTC)})
	options := SyncOptions{SeedDir: syncFixtureDir(t)}
	first, err := Sync(context.Background(), deps, options)
	if err != nil {
		t.Fatal(err)
	}
	rules, err := store.rules.ListByStatus(context.Background(), domain.RuleActive)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Sync(context.Background(), deps, options)
	if err != nil {
		t.Fatal(err)
	}
	return repeatedSyncScenario{store: store, first: first, second: second, rules: rules}
}

func TestRepeatedSyncConversation(t *testing.T) {
	testigo.Run(t, "imports every seed once", func(t *testing.T) {
		assert.Len(t, runRepeatedSync(t).rules, 2)
	})

	testigo.Run(t, "keeps the same number of rules", func(t *testing.T) {
		scenario := runRepeatedSync(t)
		rules, err := scenario.store.rules.ListByStatus(context.Background(), domain.RuleActive)
		assert.NoError(t, err)
		assert.Len(t, rules, 2)
	})

	testigo.Run(t, "preserves existing rule content", func(t *testing.T) {
		scenario := runRepeatedSync(t)
		stored, found, err := scenario.store.rules.Get(context.Background(), scenario.rules[0].RuleID)
		assert.NoError(t, err)
		assert.True(t, found, "rule should exist")
		assert.Equal(t, stored.ContentHash, scenario.rules[0].ContentHash)
	})

	testigo.Run(t, "reports a different second-run outcome", func(t *testing.T) {
		scenario := runRepeatedSync(t)
		assert.NotEqual(t, scenario.first.Message, scenario.second.Message)
	})
}

func TestDryRunSyncConversation(t *testing.T) {
	testigo.Run(t, "reports dry-run mode", func(t *testing.T) {
		store := newFakeSyncStore()
		summary, err := Sync(context.Background(), syncDeps(store, applicationtest.Clock{Time: time.Now()}), SyncOptions{DryRun: true, SeedDir: syncFixtureDir(t)})
		assert.NoError(t, err)
		assert.True(t, summary.DryRun, "summary should report dry-run")
	})

	testigo.Run(t, "does not store seed rules", func(t *testing.T) {
		store := newFakeSyncStore()
		if _, err := Sync(context.Background(), syncDeps(store, applicationtest.Clock{Time: time.Now()}), SyncOptions{DryRun: true, SeedDir: syncFixtureDir(t)}); err != nil {
			t.Fatal(err)
		}
		rules, err := store.rules.ListByStatus(context.Background(), domain.RuleActive)
		assert.NoError(t, err)
		assert.Empty(t, rules)
	})
}

func TestSyncReportsMissingSeedDirectory(t *testing.T) {
	testigo.Run(t, "as a failed run", func(t *testing.T) {
		store := newFakeSyncStore()
		missing := filepath.Join(t.TempDir(), "does-not-exist")
		_, err := Sync(context.Background(), syncDeps(store, applicationtest.Clock{Time: time.Now()}), SyncOptions{SeedDir: missing})
		assert.Error(t, err)
	})
}
