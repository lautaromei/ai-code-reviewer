package application

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/lautaromei/ai-code-reviewer/internal/application/applicationtest"
	"github.com/lautaromei/ai-code-reviewer/internal/domain"
	"github.com/lautaromei/testigo"
	"github.com/lautaromei/testigo/assert"
)

func writeFixture(t *testing.T, directory, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(directory, name), []byte(content), 0o644); err != nil {
		t.Fatalf("write seed fixture: %v", err)
	}
}

const goFixture = "## AIR-GO-001 — Grouped and ordered imports\n\n" +
	"- **Scope:** `**/*.go`\n" +
	"- **Status:** active · high · 2026-07 · fix rate last month: — (source: cluster GO-01)\n" +
	"- **Rule:** Group imports as stdlib, then external, then internal, sorted\n" +
	"  alphabetically within each group; use a descriptive alias.\n\n" +
	"## AIR-GO-002 — Idiomatic naming\n\n" +
	"- **Scope:** `**/*.go`\n" +
	"- **Status:** active · high · 2026-07 · fix rate last month: — (source: cluster GO-02)\n" +
	"- **Rule:** Do not repeat the package name within the type name.\n"

const commonFixture = "<!-- Language-agnostic rules. -->\n\n" +
	"## AIR-COMMON-003 — TODOs without an associated ticket\n" +
	"- **Scope:** `**/*`\n" +
	"- **Status:** active · high · 2026-07 · fix rate last month: — (source: cluster COMMON-03)\n" +
	"- **Rule:** Every TODO references a ticket.\n"

const noDateFixture = "## AIR-PHP-099 — Rule without a date\n" +
	"- **Scope:** `**/*.php`\n" +
	"- **Status:** active · high · fix rate last month: — (source: manual)\n" +
	"- **Rule:** Test rule without a parseable date.\n"

func newFixtureDir(t *testing.T) string {
	t.Helper()
	directory := t.TempDir()
	writeFixture(t, directory, "go.md", goFixture)
	writeFixture(t, directory, "common.md", commonFixture)
	return directory
}

var seedImportTime = time.Date(2026, 8, 6, 0, 0, 0, 0, time.UTC)

func importFixtureSeeds(t *testing.T) (SeedImportSummary, *fakeRulesStore) {
	t.Helper()
	store := newFakeRulesStore()
	summary, err := ImportSeeds(context.Background(), store.rules, applicationtest.Clock{Time: seedImportTime}, newFixtureDir(t))
	if err != nil {
		t.Fatal(err)
	}
	return summary, store
}

func seededRule(t *testing.T, store *fakeRulesStore, ruleID string) domain.Rule {
	t.Helper()
	rule, found, err := store.rules.Get(context.Background(), ruleID)
	if err != nil || !found {
		t.Fatalf("get seeded rule %q: found=%t err=%v", ruleID, found, err)
	}
	return rule
}

func TestSeedImportSummary(t *testing.T) {
	testigo.Run(t, "scans both Markdown files", func(t *testing.T) {
		summary, _ := importFixtureSeeds(t)
		assert.Equal(t, summary.FilesScanned, 2)
	})
	testigo.Run(t, "counts every rule in the input", func(t *testing.T) {
		summary, _ := importFixtureSeeds(t)
		assert.Equal(t, summary.RulesFound, 3)
	})
	testigo.Run(t, "inserts every new rule", func(t *testing.T) {
		summary, _ := importFixtureSeeds(t)
		assert.Equal(t, summary.Inserted, 3)
	})
	testigo.Run(t, "does not skip rules on the first import", func(t *testing.T) {
		summary, _ := importFixtureSeeds(t)
		assert.Zero(t, summary.SkippedExisting)
	})
	testigo.Run(t, "reports no parse errors", func(t *testing.T) {
		summary, _ := importFixtureSeeds(t)
		assert.Empty(t, summary.ParseErrors)
	})
}

func repeatedSeedImports(t *testing.T) (SeedImportSummary, SeedImportSummary) {
	t.Helper()
	directory := newFixtureDir(t)
	store := newFakeRulesStore()
	clock := applicationtest.Clock{Time: seedImportTime}
	first, err := ImportSeeds(context.Background(), store.rules, clock, directory)
	if err != nil {
		t.Fatal(err)
	}
	second, err := ImportSeeds(context.Background(), store.rules, clock, directory)
	if err != nil {
		t.Fatal(err)
	}
	return first, second
}

func TestRepeatedSeedImport(t *testing.T) {
	testigo.Run(t, "first import inserts every rule", func(t *testing.T) {
		first, _ := repeatedSeedImports(t)
		assert.Equal(t, first.Inserted, 3)
	})
	testigo.Run(t, "second import inserts no rule", func(t *testing.T) {
		_, second := repeatedSeedImports(t)
		assert.Zero(t, second.Inserted)
	})
	testigo.Run(t, "second import skips every existing rule", func(t *testing.T) {
		_, second := repeatedSeedImports(t)
		assert.Equal(t, second.SkippedExisting, 3)
	})
}

func TestSeedLanguageInference(t *testing.T) {
	testigo.Run(t, "infers Go", func(t *testing.T) {
		_, store := importFixtureSeeds(t)
		assert.Equal(t, seededRule(t, store, "AIR-GO-001").Language, "go")
	})
	testigo.Run(t, "infers common", func(t *testing.T) {
		_, store := importFixtureSeeds(t)
		assert.Equal(t, seededRule(t, store, "AIR-COMMON-003").Language, "common")
	})
	testigo.Run(t, "infers a multi-segment language", func(t *testing.T) {
		directory := t.TempDir()
		writeFixture(t, directory, "search-engine.md", "## AIR-SEARCH-ENGINE-001 — Deterministic ranking\n- **Rule:** Test rule.\n")
		store := newFakeRulesStore()
		if _, err := ImportSeeds(context.Background(), store.rules, applicationtest.Clock{Time: seedImportTime}, directory); err != nil {
			t.Fatal(err)
		}
		assert.Equal(t, seededRule(t, store, "AIR-SEARCH-ENGINE-001").Language, "search-engine")
	})
}

func TestSeedActivationDate(t *testing.T) {
	testigo.Run(t, "uses the status year and month", func(t *testing.T) {
		_, store := importFixtureSeeds(t)
		assert.Equal(t, seededRule(t, store, "AIR-GO-001").ActivatedAt, time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC))
	})

	testigo.Run(t, "falls back to import time", func(t *testing.T) {
		directory := t.TempDir()
		writeFixture(t, directory, "php.md", noDateFixture)
		store := newFakeRulesStore()
		if _, err := ImportSeeds(context.Background(), store.rules, applicationtest.Clock{Time: seedImportTime}, directory); err != nil {
			t.Fatal(err)
		}
		assert.Equal(t, seededRule(t, store, "AIR-PHP-099").ActivatedAt, seedImportTime)
	})
}

func rewrittenSeed(t *testing.T) (domain.Rule, domain.Rule) {
	t.Helper()
	directory := newFixtureDir(t)
	store := newFakeRulesStore()
	clock := applicationtest.Clock{Time: seedImportTime}
	if _, err := ImportSeeds(context.Background(), store.rules, clock, directory); err != nil {
		t.Fatal(err)
	}
	original := seededRule(t, store, "AIR-GO-001")
	writeFixture(t, directory, "go.md", "## AIR-GO-001 — Rewritten\n- **Rule:** Completely different.\n")
	if _, err := ImportSeeds(context.Background(), store.rules, clock, directory); err != nil {
		t.Fatal(err)
	}
	return original, seededRule(t, store, "AIR-GO-001")
}

func TestExistingSeedRule(t *testing.T) {
	testigo.Run(t, "is never overwritten", func(t *testing.T) {
		original, after := rewrittenSeed(t)
		assert.Equal(t, after, original)
	})
}

func TestSeedBodyRoundTrip(t *testing.T) {
	testigo.Run(t, "preserves the title", func(t *testing.T) {
		_, store := importFixtureSeeds(t)
		assert.Equal(t, seededRule(t, store, "AIR-GO-001").Title, "Grouped and ordered imports")
	})
	testigo.Run(t, "preserves a non-empty body", func(t *testing.T) {
		_, store := importFixtureSeeds(t)
		assert.NotEmpty(t, seededRule(t, store, "AIR-GO-001").Body)
	})
	testigo.Run(t, "removes leading blank lines", func(t *testing.T) {
		_, store := importFixtureSeeds(t)
		assert.NotEqual(t, seededRule(t, store, "AIR-GO-001").Body[0], byte('\n'))
	})
	testigo.Run(t, "removes trailing blank lines", func(t *testing.T) {
		_, store := importFixtureSeeds(t)
		body := seededRule(t, store, "AIR-GO-001").Body
		assert.NotEqual(t, body[len(body)-1], byte('\n'))
	})
	testigo.Run(t, "excludes the Markdown header", func(t *testing.T) {
		_, store := importFixtureSeeds(t)
		assert.NotContains(t, seededRule(t, store, "AIR-GO-001").Body, "## AIR-GO-001")
	})
}

type failingSeedCatalog struct {
	getErr    error
	insertErr error
}

func (catalog failingSeedCatalog) Get(context.Context, string) (domain.Rule, bool, error) {
	return domain.Rule{}, false, catalog.getErr
}
func (catalog failingSeedCatalog) Insert(context.Context, domain.Rule) error {
	return catalog.insertErr
}

func importMixedSeedFixture(t *testing.T) (SeedImportSummary, *fakeRulesStore) {
	t.Helper()
	directory := t.TempDir()
	if err := os.Mkdir(filepath.Join(directory, "ignored"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFixture(t, directory, "ignored.txt", "not markdown")
	if err := os.Symlink(filepath.Join(directory, "missing-target"), filepath.Join(directory, "unreadable.md")); err != nil {
		t.Fatal(err)
	}
	writeFixture(t, directory, "rules.md", "## malformed header\n"+
		"## AIR-NUMBERLESS — Missing language and sequence\nBody\n"+
		"## AIR-GO-X — Invalid sequence\nBody\n"+
		"## AIR-GO-001 — Disabled\n- **Status:** disabled · 2026-13\n"+
		"## AIR-GO-002 — Expired\n- **Status:** expired · 2026-07\n"+
		"## AIR-GO-003 — Unknown status\n- **Status:** experimental\n")
	store := newFakeRulesStore()
	summary, err := ImportSeeds(context.Background(), store.rules, applicationtest.Clock{Time: seedImportTime}, directory)
	if err != nil {
		t.Fatal(err)
	}
	return summary, store
}

func TestSeedInputAndCollaboratorFailures(t *testing.T) {
	testigo.Run(t, "rejects a missing rule catalog", func(t *testing.T) {
		directory := t.TempDir()
		writeFixture(t, directory, "go.md", "## AIR-GO-001 — Rule\nBody\n")
		_, err := ImportSeeds(context.Background(), nil, applicationtest.Clock{Time: seedImportTime}, directory)
		assert.Error(t, err)
	})

	testigo.Run(t, "parses before reporting the missing catalog", func(t *testing.T) {
		directory := t.TempDir()
		writeFixture(t, directory, "go.md", "## AIR-GO-001 — Rule\nBody\n")
		summary, _ := ImportSeeds(context.Background(), nil, applicationtest.Clock{Time: seedImportTime}, directory)
		assert.Equal(t, summary.RulesFound, 1)
	})

	testigo.Run(t, "scans only Markdown files", func(t *testing.T) {
		summary, _ := importMixedSeedFixture(t)
		assert.Equal(t, summary.FilesScanned, 2)
	})

	testigo.Run(t, "reports every malformed seed", func(t *testing.T) {
		summary, _ := importMixedSeedFixture(t)
		assert.Len(t, summary.ParseErrors, 4)
	})

	testigo.Run(t, "inserts every valid seed", func(t *testing.T) {
		summary, _ := importMixedSeedFixture(t)
		assert.Equal(t, summary.Inserted, 3)
	})

	testigo.Run(t, "imports disabled status", func(t *testing.T) {
		_, store := importMixedSeedFixture(t)
		assert.Equal(t, seededRule(t, store, "AIR-GO-001").Status, domain.RuleDisabled)
	})

	testigo.Run(t, "imports expired status", func(t *testing.T) {
		_, store := importMixedSeedFixture(t)
		assert.Equal(t, seededRule(t, store, "AIR-GO-002").Status, domain.RuleExpired)
	})

	testigo.Run(t, "unknown status remains active", func(t *testing.T) {
		_, store := importMixedSeedFixture(t)
		assert.Equal(t, seededRule(t, store, "AIR-GO-003").Status, domain.RuleActive)
	})

	testigo.Run(t, "invalid activation month falls back to import time", func(t *testing.T) {
		_, store := importMixedSeedFixture(t)
		assert.Equal(t, seededRule(t, store, "AIR-GO-001").ActivatedAt, seedImportTime)
	})

	testigo.Run(t, "propagates catalog lookup failure", func(t *testing.T) {
		directory := t.TempDir()
		writeFixture(t, directory, "go.md", "## AIR-GO-001 — Rule\nBody\n")
		failure := errors.New("lookup failed")
		_, err := ImportSeeds(context.Background(), failingSeedCatalog{getErr: failure}, applicationtest.Clock{Time: seedImportTime}, directory)
		assert.ErrorIs(t, err, failure)
	})

	testigo.Run(t, "propagates catalog insertion failure", func(t *testing.T) {
		directory := t.TempDir()
		writeFixture(t, directory, "go.md", "## AIR-GO-001 — Rule\nBody\n")
		failure := errors.New("insert failed")
		_, err := ImportSeeds(context.Background(), failingSeedCatalog{insertErr: failure}, applicationtest.Clock{Time: seedImportTime}, directory)
		assert.ErrorIs(t, err, failure)
	})
}
