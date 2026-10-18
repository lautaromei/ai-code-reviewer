package webui

import (
	"context"
	"encoding/json"
	"github.com/lautaromei/ai-code-reviewer/internal/domain"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type repositorySettingsStore struct {
	items   []domain.RepositorySettings
	metrics domain.DashboardMetrics
}
type installationChecker struct{ installed map[string]bool }

func (c installationChecker) RepositoryInstalled(_ context.Context, repository string) (bool, error) {
	return c.installed[repository], nil
}

func (s *repositorySettingsStore) ListRepositorySettings(context.Context) ([]domain.RepositorySettings, error) {
	return s.items, nil
}
func (s *repositorySettingsStore) AvailableCategories(context.Context) ([]string, error) {
	return []string{"COMMON", "GO"}, nil
}
func (s *repositorySettingsStore) ReviewModelSettings(context.Context) (domain.ReviewModelSettings, error) {
	return domain.ReviewModelSettings{GlobalModel: "opus", VerificationModel: "sonnet", GenerationAgents: 1, GenerationModels: []string{"opus"}, Version: 1}, nil
}
func (s *repositorySettingsStore) SaveGlobalReviewModels(_ context.Context, model, verificationModel string, generationModels []string, expected int64, actor string) (domain.ReviewModelSettings, error) {
	return domain.ReviewModelSettings{GlobalModel: model, VerificationModel: verificationModel, GenerationAgents: len(generationModels), GenerationModels: generationModels, Version: expected + 1, Actor: actor}, nil
}
func (s *repositorySettingsStore) ListReviewHistory(context.Context, int) ([]domain.ReviewHistoryEntry, error) {
	return nil, nil
}
func (s *repositorySettingsStore) DashboardMetrics(context.Context) (domain.DashboardMetrics, error) {
	return s.metrics, nil
}
func (s *repositorySettingsStore) SaveRepositorySettings(_ context.Context, x domain.RepositorySettings, expected int64, _ string) (domain.RepositorySettings, error) {
	x.Version = expected + 1
	s.items = []domain.RepositorySettings{x}
	return x, nil
}

type catalog struct {
	rule       domain.Rule
	events     []domain.RuleStatusEvent
	references []domain.RuleReference
}

func (c *catalog) Get(_ context.Context, id string) (domain.Rule, bool, error) {
	return c.rule, c.rule.RuleID == id, nil
}
func (c *catalog) ListByStatus(_ context.Context, s domain.RuleStatus) ([]domain.Rule, error) {
	if c.rule.Status == s {
		return []domain.Rule{c.rule}, nil
	}
	return nil, nil
}
func (c *catalog) ListReferences(context.Context, string) ([]domain.RuleReference, error) {
	return c.references, nil
}
func (c *catalog) UpdateStatus(_ context.Context, e domain.RuleStatusEvent) error {
	c.events = append(c.events, e)
	return nil
}
func (c *catalog) Delete(context.Context, string) error { return nil }

func TestRuleListIncludesCommentAndThumbsDownCounts(t *testing.T) {
	c := &catalog{
		rule:       domain.Rule{RuleID: "AIR-GO-001", Language: "GO", Status: domain.RuleActive},
		references: []domain.RuleReference{{ThumbsDown: 2}, {ThumbsDown: 1}},
	}
	r := httptest.NewRecorder()
	New(c, nil).ServeHTTP(r, httptest.NewRequest(http.MethodGet, "/api/rules", nil))
	if r.Code != http.StatusOK || !strings.Contains(r.Body.String(), `"commentCount":2`) || !strings.Contains(r.Body.String(), `"thumbsDown":3`) {
		t.Fatalf("status=%d response=%s", r.Code, r.Body.String())
	}
}
func TestRemovedMutationRoutesAreNotRegistered(t *testing.T) {
	h := New(&catalog{}, nil)
	for _, p := range []string{"/api/publish", "/api/rules/delete"} {
		r := httptest.NewRecorder()
		h.ServeHTTP(r, httptest.NewRequest(http.MethodPost, p, nil))
		if r.Code != http.StatusNotFound {
			t.Fatalf("%s status=%d", p, r.Code)
		}
	}
}
func TestRuleCanBeDisabledWithoutDeletion(t *testing.T) {
	c := &catalog{rule: domain.Rule{RuleID: "AIR-GO-001", Language: "GO", Status: domain.RuleActive}}
	h := New(c, nil)
	r := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/rules/status", strings.NewReader(`{"id":"AIR-GO-001","status":"disabled"}`))
	req.Header.Set("Content-Type", "application/json")
	h.ServeHTTP(r, req)
	if r.Code != 200 || len(c.events) != 1 || c.events[0].Reason != domain.DisabledManual {
		t.Fatalf("status=%d events=%v", r.Code, c.events)
	}
}
func TestCrossOriginMutationRejected(t *testing.T) {
	c := &catalog{rule: domain.Rule{RuleID: "AIR-GO-001", Status: domain.RuleActive}}
	r := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "http://local/api/rules/status", strings.NewReader(`{"id":"AIR-GO-001","status":"disabled"}`))
	req.Header.Set("Origin", "https://evil.example")
	New(c, nil).ServeHTTP(r, req)
	if r.Code != http.StatusForbidden {
		t.Fatalf("status=%d", r.Code)
	}
}

func TestRepositorySettingsCanBeSavedFromWeb(t *testing.T) {
	repos := &repositorySettingsStore{}
	h := NewAdmin(&catalog{}, nil, repos)
	r := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/repositories", strings.NewReader(`{"name":"acme/api","review_enabled":true,"mining_enabled":false,"review_model":"opus","categories":["GO"],"expected_version":0}`))
	req.Header.Set("Content-Type", "application/json")
	h.ServeHTTP(r, req)
	if r.Code != http.StatusOK || len(repos.items) != 1 || repos.items[0].FullName != "acme/api" || repos.items[0].ReviewModel != "opus" {
		t.Fatalf("status=%d repositories=%v", r.Code, repos.items)
	}
	var got domain.RepositorySettings
	if err := json.NewDecoder(r.Body).Decode(&got); err != nil || got.Version != 1 {
		t.Fatalf("response=%s err=%v", r.Body.String(), err)
	}
}

func TestRepositoryInstallationStatusIsExposed(t *testing.T) {
	repos := &repositorySettingsStore{items: []domain.RepositorySettings{{FullName: "acme/api"}}}
	h := NewAdminWithInstallations(&catalog{}, nil, repos, installationChecker{installed: map[string]bool{"acme/api": true}}, "https://github.com/apps/reviewer/installations/new")
	r := httptest.NewRecorder()
	h.ServeHTTP(r, httptest.NewRequest(http.MethodGet, "/api/repositories/installations", nil))
	if r.Code != http.StatusOK || !strings.Contains(r.Body.String(), `"installed":true`) || !strings.Contains(r.Body.String(), `"install_url":"https://github.com/apps/reviewer/installations/new"`) {
		t.Fatalf("status=%d response=%s", r.Code, r.Body.String())
	}
}

func TestGlobalReviewModelCanBeSavedFromWeb(t *testing.T) {
	repos := &repositorySettingsStore{}
	r := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/models", strings.NewReader(`{"model":"opus","verification_model":"sonnet","generation_models":["haiku_max","opus"],"expected_version":1,"actor":"admin"}`))
	req.Header.Set("Content-Type", "application/json")
	NewAdmin(&catalog{}, nil, repos).ServeHTTP(r, req)
	if r.Code != http.StatusOK || !strings.Contains(r.Body.String(), `"GlobalModel":"opus"`) {
		t.Fatalf("status=%d response=%s", r.Code, r.Body.String())
	}
}

func TestHaikuMaxIsAvailableFromWeb(t *testing.T) {
	repos := &repositorySettingsStore{}
	r := httptest.NewRecorder()
	NewAdmin(&catalog{}, nil, repos).ServeHTTP(r, httptest.NewRequest(http.MethodGet, "/api/models", nil))
	if r.Code != http.StatusOK || !strings.Contains(r.Body.String(), `"haiku_max"`) {
		t.Fatalf("status=%d response=%s", r.Code, r.Body.String())
	}
}

func TestJevIsAvailableOnlyAsVerificationModelFromWeb(t *testing.T) {
	repos := &repositorySettingsStore{}
	r := httptest.NewRecorder()
	NewAdmin(&catalog{}, nil, repos).ServeHTTP(r, httptest.NewRequest(http.MethodGet, "/api/models", nil))
	if r.Code != http.StatusOK || !strings.Contains(r.Body.String(), `"verification_models":["sonnet","opus","haiku_max","jev"]`) {
		t.Fatalf("status=%d response=%s", r.Code, r.Body.String())
	}
}

func TestIndexContainsAdministrationTabs(t *testing.T) {
	r := httptest.NewRecorder()
	New(&catalog{}, nil).ServeHTTP(r, httptest.NewRequest(http.MethodGet, "/", nil))
	for _, label := range []string{"Repositorios", "Prompt general", "Prompts por repositorio"} {
		if !strings.Contains(r.Body.String(), label) {
			t.Fatalf("missing tab %q", label)
		}
	}
}

func TestDashboardMetricsAreExposed(t *testing.T) {
	repos := &repositorySettingsStore{metrics: domain.DashboardMetrics{ReviewsToday: 3, CostTodayMicroUSD: 125000}}
	r := httptest.NewRecorder()
	NewAdmin(&catalog{}, nil, repos).ServeHTTP(r, httptest.NewRequest(http.MethodGet, "/api/dashboard", nil))
	if r.Code != http.StatusOK || !strings.Contains(r.Body.String(), `"ReviewsToday":3`) || !strings.Contains(r.Body.String(), `"CostTodayMicroUSD":125000`) {
		t.Fatalf("status=%d response=%s", r.Code, r.Body.String())
	}
}
