package sqlite

import (
	"context"
	"strings"
	"testing"

	"github.com/lautaromei/ai-code-reviewer/internal/domain"
)

func TestReviewModelSettingsResolveGlobalAndRepositoryOverride(t *testing.T) {
	db, _ := openTestStore(t)
	store := NewReviewStore(db)
	ctx := context.Background()
	if err := store.SeedReviewModel(ctx, "sonnet"); err != nil {
		t.Fatal(err)
	}
	if err := store.SeedRepositorySettings(ctx, []domain.RepositorySettings{{FullName: "acme/api", ReviewEnabled: true, Categories: []string{"GO"}}}); err != nil {
		t.Fatal(err)
	}
	if got, err := store.EffectiveReviewModel(ctx, "acme/api"); err != nil || got != "sonnet" {
		t.Fatalf("global model: got=%q err=%v", got, err)
	}
	if got, err := store.EffectiveVerificationModel(ctx); err != nil || got != "sonnet" {
		t.Fatalf("verification model: got=%q err=%v", got, err)
	}
	settings, found, err := store.RepositorySettings(ctx, "acme/api")
	if err != nil || !found {
		t.Fatalf("repository settings: found=%v err=%v", found, err)
	}
	settings.ReviewModel = "opus"
	if _, err = store.SaveRepositorySettings(ctx, settings, settings.Version, "admin"); err != nil {
		t.Fatal(err)
	}
	if got, err := store.EffectiveReviewModel(ctx, "acme/api"); err != nil || got != "opus" {
		t.Fatalf("repository override: got=%q err=%v", got, err)
	}
}

func TestGlobalReviewModelUsesOptimisticVersion(t *testing.T) {
	db, _ := openTestStore(t)
	store := NewReviewStore(db)
	ctx := context.Background()
	if err := store.SeedReviewModel(ctx, "sonnet"); err != nil {
		t.Fatal(err)
	}
	updated, err := store.SaveGlobalReviewModels(ctx, "opus", "sonnet", []string{"haiku_max", "sonnet", "opus"}, 1, "admin")
	if err != nil || updated.GlobalModel != "opus" || updated.VerificationModel != "sonnet" || updated.GenerationAgents != 3 || strings.Join(updated.GenerationModels, ",") != "haiku_max,sonnet,opus" || updated.Version != 2 {
		t.Fatalf("updated=%#v err=%v", updated, err)
	}
	if _, err = store.SaveGlobalReviewModels(ctx, "sonnet", "sonnet", []string{"sonnet"}, 1, "stale"); err == nil {
		t.Fatal("expected a version conflict")
	}
}

func TestHaikuMaxCanBeSelectedForReviewAndVerification(t *testing.T) {
	db, _ := openTestStore(t)
	store := NewReviewStore(db)
	ctx := context.Background()
	if err := store.SeedReviewModel(ctx, "sonnet"); err != nil {
		t.Fatal(err)
	}
	updated, err := store.SaveGlobalReviewModels(ctx, "haiku_max", "haiku_max", []string{"haiku_max"}, 1, "admin")
	if err != nil || updated.GlobalModel != "haiku_max" || updated.VerificationModel != "haiku_max" {
		t.Fatalf("updated=%#v err=%v", updated, err)
	}
}

func TestJevCanOnlyBeSelectedForVerification(t *testing.T) {
	db, _ := openTestStore(t)
	store := NewReviewStore(db)
	ctx := context.Background()
	if err := store.SeedReviewModel(ctx, "sonnet"); err != nil {
		t.Fatal(err)
	}
	updated, err := store.SaveGlobalReviewModels(ctx, "sonnet", "jev", []string{"sonnet"}, 1, "admin")
	if err != nil || updated.VerificationModel != "jev" {
		t.Fatalf("updated=%#v err=%v", updated, err)
	}
	if _, err = store.SaveGlobalReviewModels(ctx, "jev", "jev", []string{"sonnet"}, 2, "admin"); err == nil {
		t.Fatal("expected Jev generation model to be rejected")
	}
}
