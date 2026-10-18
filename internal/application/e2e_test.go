package application

import (
	"context"
	"github.com/lautaromei/ai-code-reviewer/internal/adapters/sqlite"
	"github.com/lautaromei/ai-code-reviewer/internal/application/ports"
	"github.com/lautaromei/ai-code-reviewer/internal/domain"
	"testing"
	"time"
)

type e2eGitHub struct{ published int }

func (g *e2eGitHub) GetPullRequest(context.Context, string, int64) (ports.PullRequest, error) {
	return ports.PullRequest{Number: 7, State: "open", HeadSHA: "head", BaseSHA: "base"}, nil
}
func (g *e2eGitHub) ListChangedFiles(context.Context, string, int64) ([]ports.ChangedFile, error) {
	return []ports.ChangedFile{{Path: "main.go", Patch: "@@ -1 +1 @@\n-old\n+bad()"}}, nil
}
func (g *e2eGitHub) ReadFileAtSHA(context.Context, string, string, string) ([]byte, error) {
	return nil, nil
}
func (g *e2eGitHub) ListPullRequestComments(context.Context, string, int64) ([]ports.ExistingReviewComment, error) {
	return nil, nil
}
func (g *e2eGitHub) PublishReviewComment(context.Context, string, int64, string, ports.ReviewPublishRequest) (ports.PublishedComment, error) {
	g.published++
	return ports.PublishedComment{ID: 99}, nil
}
func (g *e2eGitHub) PublishIssueComment(context.Context, string, int64, string) (ports.PublishedComment, error) {
	return ports.PublishedComment{}, nil
}

type e2eModel struct{}

func (e2eModel) Review(_ context.Context, _ string, p ports.ReviewPayload) (ports.ReviewModelResult, error) {
	if p.Stage == "verification" {
		findings := append([]ports.ModelFinding(nil), p.CandidateFindings...)
		for i := range findings {
			findings[i].Verdict = "accept"
			findings[i].Reason = "confirmed by test verifier"
		}
		return ports.ReviewModelResult{Summary: "verified", Findings: findings}, nil
	}
	return ports.ReviewModelResult{Summary: "reviewed", Findings: []ports.ModelFinding{{Kind: "catalog_rule", RuleID: "AIR-GO-001", Path: "main.go", Line: 1, Side: "RIGHT", Confidence: 90, Severity: "high", Body: "Fix it"}}}, nil
}
func TestReviewPersistsAndPublishesOnce(t *testing.T) {
	ctx := context.Background()
	db, e := sqlite.Open(ctx, t.TempDir()+"/review.db", time.Second)
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	store := sqlite.NewReviewStore(db)
	if e = store.SyncRepositories(ctx, map[string][]string{"acme/api": {"GO"}}); e != nil {
		t.Fatal(e)
	}
	if e = sqlite.NewRuleCatalog(db).Insert(ctx, domain.Rule{RuleID: "AIR-GO-001", Language: "GO", Title: "Rule", Body: "Do the thing", Scope: "**/*.go", Status: domain.RuleActive, CreatedAt: time.Now(), ActivatedAt: time.Now()}); e != nil {
		t.Fatal(e)
	}
	gh := &e2eGitHub{}
	deps := ReviewDeps{GitHub: gh, Model: e2eModel{}, Store: store, Prompts: sqlite.NewPromptStore(db), Provider: "test", ModelName: "test"}
	first, e := ReviewPullRequest(ctx, deps, ReviewOptions{Repository: "acme/api", PullRequest: 7, Categories: []string{"GO"}, MaxComments: 20})
	if e != nil {
		t.Fatal(e)
	}
	second, e := ReviewPullRequest(ctx, deps, ReviewOptions{Repository: "acme/api", PullRequest: 7, Categories: []string{"GO"}})
	if e != nil {
		t.Fatal(e)
	}
	if first.Status != domain.ReviewCompleted || gh.published != 1 || second.Summary != "pull request already has a review job" {
		t.Fatalf("first=%+v second=%+v published=%d", first, second, gh.published)
	}
}
