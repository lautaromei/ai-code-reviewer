package sqlite

import (
	"context"
	"testing"
	"time"

	"github.com/lautaromei/ai-code-reviewer/internal/domain"
)

func TestRepositoryWebSettingsSurviveConfigSeed(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, "file:repository-settings?mode=memory&cache=shared", time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	store := NewReviewStore(db)
	seed := []domain.RepositorySettings{{FullName: "acme/api", ReviewEnabled: true, MiningEnabled: true, Categories: []string{"GO"}}}
	if err = store.SeedRepositorySettings(ctx, seed); err != nil {
		t.Fatal(err)
	}
	current, ok, err := store.RepositorySettings(ctx, "acme/api")
	if err != nil || !ok {
		t.Fatalf("settings ok=%v err=%v", ok, err)
	}
	current.ReviewEnabled = false
	current.Categories = []string{"COMMON", "SQL"}
	if _, err = store.SaveRepositorySettings(ctx, current, current.Version, "test"); err != nil {
		t.Fatal(err)
	}
	if err = store.SeedRepositorySettings(ctx, seed); err != nil {
		t.Fatal(err)
	}
	got, _, err := store.RepositorySettings(ctx, "acme/api")
	if err != nil {
		t.Fatal(err)
	}
	if got.ReviewEnabled || len(got.Categories) != 2 || got.Categories[0] != "COMMON" || got.Categories[1] != "SQL" {
		t.Fatalf("web settings were overwritten: %+v", got)
	}
}

func TestRepositorySettingsRejectStaleVersion(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, "file:repository-conflict?mode=memory&cache=shared", time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	store := NewReviewStore(db)
	created, err := store.SaveRepositorySettings(ctx, domain.RepositorySettings{FullName: "acme/api", ReviewEnabled: true, Categories: []string{"GO"}}, 0, "test")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.SaveRepositorySettings(ctx, created, 0, "stale"); err == nil {
		t.Fatal("expected version conflict")
	}
}

func TestDeletedRepositoryStaysHiddenUntilManuallyAddedAgain(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, "file:repository-deletion?mode=memory&cache=shared", time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	store := NewReviewStore(db)
	seed := []domain.RepositorySettings{{FullName: "acme/api", ReviewEnabled: true, Categories: []string{"GO"}}}
	if err = store.SeedRepositorySettings(ctx, seed); err != nil {
		t.Fatal(err)
	}
	current, ok, err := store.RepositorySettings(ctx, "acme/api")
	if err != nil || !ok {
		t.Fatalf("settings ok=%v err=%v", ok, err)
	}
	if err = store.DeleteRepository(ctx, current.FullName, current.Version); err != nil {
		t.Fatal(err)
	}
	if _, ok, err = store.RepositorySettings(ctx, "acme/api"); err != nil || ok {
		t.Fatalf("deleted repository visible: ok=%v err=%v", ok, err)
	}
	if err = store.SeedRepositorySettings(ctx, seed); err != nil {
		t.Fatal(err)
	}
	if repositories, err := store.ListRepositorySettings(ctx); err != nil || len(repositories) != 0 {
		t.Fatalf("seed restored deleted repository: repositories=%v err=%v", repositories, err)
	}
	restored, err := store.SaveRepositorySettings(ctx, seed[0], 0, "admin")
	if err != nil || restored.FullName != "acme/api" || !restored.ReviewEnabled {
		t.Fatalf("manual restore failed: settings=%+v err=%v", restored, err)
	}
}

func TestDeleteRepositoryRejectsStaleVersion(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, "file:repository-delete-conflict?mode=memory&cache=shared", time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	store := NewReviewStore(db)
	created, err := store.SaveRepositorySettings(ctx, domain.RepositorySettings{FullName: "acme/api", ReviewEnabled: true, Categories: []string{"GO"}}, 0, "test")
	if err != nil {
		t.Fatal(err)
	}
	if err = store.DeleteRepository(ctx, created.FullName, created.Version+1); err == nil {
		t.Fatal("expected version conflict")
	}
}

func TestGitHubInstallationOnboardsAndDisablesRepository(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, "file:installation-onboarding?mode=memory&cache=shared", time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	store := NewReviewStore(db)
	if err = store.RegisterInstallationRepositories(ctx, 42, []string{"acme/api"}); err != nil {
		t.Fatal(err)
	}
	settings, ok, err := store.RepositorySettings(ctx, "acme/api")
	if err != nil || !ok || !settings.ReviewEnabled || len(settings.Categories) != 1 || settings.Categories[0] != "COMMON" {
		t.Fatalf("unexpected onboarding settings: ok=%v err=%v settings=%+v", ok, err, settings)
	}
	if id, err := store.InstallationID(ctx, "acme/api"); err != nil || id != 42 {
		t.Fatalf("installation id=%d err=%v", id, err)
	}
	if err = store.UnregisterInstallationRepositories(ctx, 42, nil); err != nil {
		t.Fatal(err)
	}
	settings, _, err = store.RepositorySettings(ctx, "acme/api")
	if err != nil || settings.ReviewEnabled {
		t.Fatalf("repository should be disabled: err=%v settings=%+v", err, settings)
	}
}
