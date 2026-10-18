package sqlite

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestPromptRevisionAndOptimisticConcurrency(t *testing.T) {
	ctx := context.Background()
	db, e := Open(ctx, t.TempDir()+"/prompt.db", time.Second)
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	p := NewPromptStore(db)
	initial, e := p.Effective(ctx, "acme/api")
	if e != nil || initial.EffectiveOrigin != "product default" {
		t.Fatalf("initial=%+v error=%v", initial, e)
	}
	saved, e := p.Save(ctx, "repo", "acme/api", "Review carefully", "web", "tester", 0)
	if e != nil {
		t.Fatal(e)
	}
	if saved.Revision.Text != "Review carefully" || saved.BindingVersion != 1 {
		t.Fatalf("saved=%+v", saved)
	}
	_, e = p.Save(ctx, "repo", "acme/api", "Overwrite", "web", "tester", 0)
	if e == nil || !strings.Contains(e.Error(), "conflict") {
		t.Fatalf("error=%v", e)
	}
	captured, e := p.ByHash(ctx, "acme/api", saved.Revision.Hash)
	if e != nil || captured.Revision.Text != "Review carefully" {
		t.Fatalf("captured=%+v error=%v", captured, e)
	}
}
func TestPromptUseGlobal(t *testing.T) {
	ctx := context.Background()
	db, e := Open(ctx, t.TempDir()+"/prompt.db", time.Second)
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	p := NewPromptStore(db)
	if _, e = p.Save(ctx, "global", "", "Global prompt", "web", "tester", 0); e != nil {
		t.Fatal(e)
	}
	repo, e := p.Save(ctx, "repo", "acme/api", "Repo prompt", "web", "tester", 0)
	if e != nil {
		t.Fatal(e)
	}
	if e = p.UseGlobal(ctx, "acme/api", repo.BindingVersion); e != nil {
		t.Fatal(e)
	}
	effective, e := p.Effective(ctx, "acme/api")
	if e != nil || effective.Revision.Text != "Global prompt" {
		t.Fatalf("effective=%+v error=%v", effective, e)
	}
}

func TestPromptCanUseGlobalWithoutExistingRepositoryBinding(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, t.TempDir()+"/prompt.db", time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	p := NewPromptStore(db)
	if _, err = p.Save(ctx, "global", "", "Global prompt", "web", "tester", 0); err != nil {
		t.Fatal(err)
	}
	if err = p.UseGlobal(ctx, "acme/new", 0); err != nil {
		t.Fatal(err)
	}
	effective, err := p.Effective(ctx, "acme/new")
	if err != nil || effective.Revision.Text != "Global prompt" {
		t.Fatalf("effective=%+v error=%v", effective, err)
	}
}
