// Package webui provides the local catalogue browser served by ai-rules serve.
package webui

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/lautaromei/ai-code-reviewer/internal/application"
	"github.com/lautaromei/ai-code-reviewer/internal/domain"
)

//go:embed static/index.html
var indexHTML []byte

type ruleCatalog interface {
	Get(context.Context, string) (domain.Rule, bool, error)
	ListByStatus(context.Context, domain.RuleStatus) ([]domain.Rule, error)
	ListReferences(context.Context, string) ([]domain.RuleReference, error)
	UpdateStatus(context.Context, domain.RuleStatusEvent) error
	Delete(context.Context, string) error
}

type publishFunc func(context.Context) (application.Summary, error)
type promptStore interface {
	Effective(context.Context, string) (domain.ReviewPrompt, error)
	Validation(context.Context) (domain.ReviewPrompt, error)
	List(context.Context, string, string) ([]domain.ReviewPromptRevision, error)
	Save(context.Context, string, string, string, string, string, int64) (domain.ReviewPrompt, error)
	UseGlobal(context.Context, string, int64) error
}
type repositoryStore interface {
	ListRepositorySettings(context.Context) ([]domain.RepositorySettings, error)
	SaveRepositorySettings(context.Context, domain.RepositorySettings, int64, string) (domain.RepositorySettings, error)
	DeleteRepository(context.Context, string, int64) error
	ReviewModelSettings(context.Context) (domain.ReviewModelSettings, error)
	SaveGlobalReviewModels(context.Context, string, string, []string, int64, string) (domain.ReviewModelSettings, error)
	ListReviewHistory(context.Context, int) ([]domain.ReviewHistoryEntry, error)
	ReviewDetail(context.Context, int64) (domain.ReviewDetail, bool, error)
	DashboardMetrics(context.Context) (domain.DashboardMetrics, error)
	AvailableCategories(context.Context) ([]string, error)
}
type repositoryInstallationChecker interface {
	RepositoryInstalled(context.Context, string) (bool, error)
}

// New returns a handler for browsing, downloading, and publishing the rule catalogue.
func New(rules ruleCatalog, publish publishFunc) http.Handler {
	return NewWithPrompts(rules, nil)
}
func NewWithPrompts(rules ruleCatalog, prompts promptStore) http.Handler {
	return NewAdmin(rules, prompts, nil)
}
func NewAdmin(rules ruleCatalog, prompts promptStore, repositories repositoryStore) http.Handler {
	return NewAdminWithInstallations(rules, prompts, repositories, nil, "")
}
func NewAdminWithInstallations(rules ruleCatalog, prompts promptStore, repositories repositoryStore, installations repositoryInstallationChecker, installURL string) http.Handler {
	s := &server{rules: rules, prompts: prompts, repositories: repositories, installations: installations, installURL: installURL}
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.index)
	mux.HandleFunc("/api/rules", s.listRules)
	mux.HandleFunc("/api/rules/status", s.updateRuleStatus)
	mux.HandleFunc("/api/rules/download", s.downloadRules)
	mux.HandleFunc("/api/prompts/effective", s.effectivePrompt)
	mux.HandleFunc("/api/prompts/revisions", s.promptRevisions)
	mux.HandleFunc("/api/prompts/save", s.savePrompt)
	mux.HandleFunc("/api/prompts/use-global", s.useGlobalPrompt)
	mux.HandleFunc("/api/repositories", s.repositoriesAPI)
	mux.HandleFunc("/api/repositories/installations", s.repositoryInstallationsAPI)
	mux.HandleFunc("/api/models", s.modelsAPI)
	mux.HandleFunc("/api/reviews", s.reviewsAPI)
	mux.HandleFunc("/api/reviews/", s.reviewDetailAPI)
	mux.HandleFunc("/api/dashboard", s.dashboardAPI)
	mux.HandleFunc("/api/categories", s.categoriesAPI)
	return mux
}

func (s *server) reviewDetailAPI(w http.ResponseWriter, r *http.Request) {
	if s.repositories == nil {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	id, err := strconv.ParseInt(strings.TrimPrefix(r.URL.Path, "/api/reviews/"), 10, 64)
	if err != nil || id <= 0 {
		http.NotFound(w, r)
		return
	}
	x, found, err := s.repositories.ReviewDetail(r.Context(), id)
	if err != nil {
		writeError(w, err)
		return
	}
	if !found {
		http.NotFound(w, r)
		return
	}
	writeJSON(w, http.StatusOK, x)
}

func (s *server) dashboardAPI(w http.ResponseWriter, r *http.Request) {
	if s.repositories == nil {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	x, err := s.repositories.DashboardMetrics(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, x)
}

func (s *server) reviewsAPI(w http.ResponseWriter, r *http.Request) {
	if s.repositories == nil {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	x, e := s.repositories.ListReviewHistory(r.Context(), 100)
	if e != nil {
		writeError(w, e)
		return
	}
	writeJSON(w, 200, x)
}

func (s *server) modelsAPI(w http.ResponseWriter, r *http.Request) {
	if s.repositories == nil {
		http.NotFound(w, r)
		return
	}
	if r.Method == http.MethodGet {
		x, e := s.repositories.ReviewModelSettings(r.Context())
		if e != nil {
			writeError(w, e)
			return
		}
		writeJSON(w, 200, map[string]any{"settings": x, "models": []string{"sonnet", "opus", "haiku_max"}, "verification_models": []string{"sonnet", "opus", "haiku_max", "jev"}})
		return
	}
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if !sameOrigin(r) {
		writeJSON(w, 403, map[string]string{"error": "cross-origin mutation rejected"})
		return
	}
	var x struct {
		Model             string   `json:"model"`
		VerificationModel string   `json:"verification_model"`
		GenerationModels  []string `json:"generation_models"`
		Actor             string   `json:"actor"`
		ExpectedVersion   int64    `json:"expected_version"`
	}
	if e := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&x); e != nil {
		writeJSON(w, 400, map[string]string{"error": e.Error()})
		return
	}
	out, e := s.repositories.SaveGlobalReviewModels(r.Context(), x.Model, x.VerificationModel, x.GenerationModels, x.ExpectedVersion, x.Actor)
	if e != nil {
		code := 400
		if strings.Contains(e.Error(), "conflict") {
			code = 409
		}
		writeJSON(w, code, map[string]string{"error": e.Error()})
		return
	}
	writeJSON(w, 200, out)
}

type server struct {
	rules         ruleCatalog
	publish       publishFunc
	prompts       promptStore
	repositories  repositoryStore
	installations repositoryInstallationChecker
	installURL    string
}

func (s *server) repositoryInstallationsAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if s.repositories == nil || s.installations == nil {
		writeJSON(w, 200, []any{})
		return
	}
	repositories, err := s.repositories.ListRepositorySettings(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	type status struct {
		Name       string `json:"name"`
		Installed  bool   `json:"installed"`
		InstallURL string `json:"install_url"`
		Error      string `json:"error,omitempty"`
	}
	out := make([]status, 0, len(repositories))
	for _, repository := range repositories {
		installed, checkErr := s.installations.RepositoryInstalled(r.Context(), repository.FullName)
		x := status{Name: repository.FullName, Installed: installed, InstallURL: s.installURL}
		if checkErr != nil {
			x.Error = checkErr.Error()
		}
		out = append(out, x)
	}
	writeJSON(w, 200, out)
}

func (s *server) repositoriesAPI(w http.ResponseWriter, r *http.Request) {
	if s.repositories == nil {
		http.NotFound(w, r)
		return
	}
	if r.Method == http.MethodGet {
		x, e := s.repositories.ListRepositorySettings(r.Context())
		if e != nil {
			writeError(w, e)
			return
		}
		writeJSON(w, 200, x)
		return
	}
	if r.Method == http.MethodDelete {
		if !sameOrigin(r) {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "cross-origin mutation rejected"})
			return
		}
		var x struct {
			Name            string `json:"name"`
			ExpectedVersion int64  `json:"expected_version"`
		}
		if e := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&x); e != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": e.Error()})
			return
		}
		if e := s.repositories.DeleteRepository(r.Context(), x.Name, x.ExpectedVersion); e != nil {
			code := http.StatusBadRequest
			if strings.Contains(e.Error(), "not found") {
				code = http.StatusNotFound
			} else if strings.Contains(e.Error(), "conflict") {
				code = http.StatusConflict
			}
			writeJSON(w, code, map[string]string{"error": e.Error()})
			return
		}
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if !sameOrigin(r) {
		writeJSON(w, 403, map[string]string{"error": "cross-origin mutation rejected"})
		return
	}
	var x struct {
		Name            string   `json:"name"`
		ReviewEnabled   bool     `json:"review_enabled"`
		MiningEnabled   bool     `json:"mining_enabled"`
		ReviewModel     string   `json:"review_model"`
		Categories      []string `json:"categories"`
		ExpectedVersion int64    `json:"expected_version"`
		Actor           string   `json:"actor"`
	}
	if e := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&x); e != nil {
		writeJSON(w, 400, map[string]string{"error": e.Error()})
		return
	}
	out, e := s.repositories.SaveRepositorySettings(r.Context(), domain.RepositorySettings{FullName: x.Name, ReviewEnabled: x.ReviewEnabled, MiningEnabled: x.MiningEnabled, ReviewModel: x.ReviewModel, Categories: x.Categories}, x.ExpectedVersion, x.Actor)
	if e != nil {
		code := 400
		if strings.Contains(e.Error(), "conflict") {
			code = 409
		}
		writeJSON(w, code, map[string]string{"error": e.Error()})
		return
	}
	writeJSON(w, 200, out)
}

func (s *server) categoriesAPI(w http.ResponseWriter, r *http.Request) {
	if s.repositories == nil {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	x, e := s.repositories.AvailableCategories(r.Context())
	if e != nil {
		writeError(w, e)
		return
	}
	writeJSON(w, 200, x)
}

func (s *server) effectivePrompt(w http.ResponseWriter, r *http.Request) {
	if s.prompts == nil {
		http.NotFound(w, r)
		return
	}
	var p domain.ReviewPrompt
	var e error
	if r.URL.Query().Get("scope") == "validation" {
		p, e = s.prompts.Validation(r.Context())
	} else {
		p, e = s.prompts.Effective(r.Context(), r.URL.Query().Get("repository"))
	}
	if e != nil {
		writeError(w, e)
		return
	}
	writeJSON(w, 200, p)
}
func (s *server) promptRevisions(w http.ResponseWriter, r *http.Request) {
	if s.prompts == nil {
		http.NotFound(w, r)
		return
	}
	x, e := s.prompts.List(r.Context(), r.URL.Query().Get("scope"), r.URL.Query().Get("repository"))
	if e != nil {
		writeError(w, e)
		return
	}
	writeJSON(w, 200, x)
}
func (s *server) savePrompt(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(405)
		return
	}
	if !sameOrigin(r) {
		w.WriteHeader(403)
		return
	}
	var x struct {
		Scope, Repository, Text, Author string
		ExpectedVersion                 int64 `json:"expected_version"`
	}
	if e := json.NewDecoder(http.MaxBytesReader(w, r.Body, 256<<10)).Decode(&x); e != nil {
		writeJSON(w, 400, map[string]string{"error": e.Error()})
		return
	}
	p, e := s.prompts.Save(r.Context(), x.Scope, x.Repository, x.Text, "web", x.Author, x.ExpectedVersion)
	if e != nil {
		if strings.Contains(e.Error(), "conflict") {
			writeJSON(w, 409, map[string]string{"error": e.Error()})
			return
		}
		writeError(w, e)
		return
	}
	writeJSON(w, 200, p)
}
func (s *server) useGlobalPrompt(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(405)
		return
	}
	if !sameOrigin(r) {
		w.WriteHeader(403)
		return
	}
	var x struct {
		Repository      string
		ExpectedVersion int64 `json:"expected_version"`
	}
	if e := json.NewDecoder(r.Body).Decode(&x); e != nil {
		writeJSON(w, 400, map[string]string{"error": e.Error()})
		return
	}
	if e := s.prompts.UseGlobal(r.Context(), x.Repository, x.ExpectedVersion); e != nil {
		writeJSON(w, 409, map[string]string{"error": e.Error()})
		return
	}
	writeJSON(w, 200, map[string]string{"message": "using global prompt"})
}

type ruleDTO struct {
	ID             string `json:"id"`
	Language       string `json:"language"`
	Title          string `json:"title"`
	Scope          string `json:"scope"`
	Body           string `json:"body"`
	Status         string `json:"status"`
	CreatedAt      string `json:"createdAt"`
	ActivatedAt    string `json:"activatedAt"`
	LastActivity   string `json:"lastActivity"`
	LastUsedAt     string `json:"lastUsedAt,omitempty"`
	DisabledAt     string `json:"disabledAt,omitempty"`
	DisabledReason string `json:"disabledReason,omitempty"`
	Source         string `json:"source"`
	ContentHash    string `json:"contentHash"`
	CommentCount   int    `json:"commentCount"`
	ThumbsDown     int    `json:"thumbsDown"`
}

func (s *server) index(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" && !strings.HasPrefix(r.URL.Path, "/reviews/") {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(indexHTML)
}

func (s *server) listRules(w http.ResponseWriter, r *http.Request) {
	rules, err := s.filteredRules(r.Context(), r.URL.Query().Get("status"), r.URL.Query().Get("language"), r.URL.Query().Get("q"))
	if err != nil {
		writeError(w, err)
		return
	}
	languages := make(map[string]struct{})
	counts := make(map[string]int)
	for _, rule := range rules {
		counts[rule.Status]++
	}
	for _, status := range []domain.RuleStatus{domain.RuleActive, domain.RuleDisabled, domain.RuleExpired} {
		catalog, err := s.rules.ListByStatus(r.Context(), status)
		if err != nil {
			writeError(w, err)
			return
		}
		for _, rule := range catalog {
			languages[rule.Language] = struct{}{}
		}
	}
	langs := make([]string, 0, len(languages))
	for language := range languages {
		langs = append(langs, language)
	}
	sort.Strings(langs)
	writeJSON(w, http.StatusOK, map[string]any{"rules": rules, "languages": langs, "counts": counts})
}

func (s *server) updateRuleStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if !sameOrigin(r) {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "cross-origin mutation rejected"})
		return
	}
	var request struct {
		RuleID string `json:"id"`
		Status string `json:"status"`
	}
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeError(w, fmt.Errorf("invalid status request: %w", err))
		return
	}
	if request.RuleID == "" {
		writeError(w, fmt.Errorf("rule id is required"))
		return
	}
	if request.Status != string(domain.RuleActive) && request.Status != string(domain.RuleDisabled) {
		writeError(w, fmt.Errorf("status must be active or disabled"))
		return
	}

	rule, ok, err := s.rules.Get(r.Context(), request.RuleID)
	if err != nil {
		writeError(w, err)
		return
	}
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "rule not found"})
		return
	}
	target := domain.RuleStatus(request.Status)
	if rule.Status == target {
		writeJSON(w, http.StatusOK, map[string]string{"message": "rule already has that status"})
		return
	}
	reason := domain.DisabledReason("")
	if target == domain.RuleDisabled {
		reason = domain.DisabledManual
	}
	event := domain.RuleStatusEvent{
		EventKey:   fmt.Sprintf("webui:%s:%s:%d", rule.RuleID, target, time.Now().UnixNano()),
		RuleID:     rule.RuleID,
		FromState:  rule.Status,
		ToState:    target,
		Reason:     reason,
		OccurredAt: time.Now().UTC(),
	}
	if err := s.rules.UpdateStatus(r.Context(), event); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"message": fmt.Sprintf("rule %s is now %s", rule.RuleID, target)})
}

func sameOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	return origin == "" || origin == "http://"+r.Host || origin == "https://"+r.Host
}

func (s *server) deleteRule(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var request struct {
		RuleID string `json:"id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeError(w, fmt.Errorf("invalid delete request: %w", err))
		return
	}
	if request.RuleID == "" {
		writeError(w, fmt.Errorf("rule id is required"))
		return
	}
	if _, ok, err := s.rules.Get(r.Context(), request.RuleID); err != nil {
		writeError(w, err)
		return
	} else if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "rule not found"})
		return
	}
	if err := s.rules.Delete(r.Context(), request.RuleID); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"message": fmt.Sprintf("rule %s deleted", request.RuleID)})
}

func (s *server) downloadRules(w http.ResponseWriter, r *http.Request) {
	rules, err := s.filteredRules(r.Context(), r.URL.Query().Get("status"), r.URL.Query().Get("language"), r.URL.Query().Get("q"))
	if err != nil {
		writeError(w, err)
		return
	}
	w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
	w.Header().Set("Content-Disposition", "attachment; filename=ai-review-rules.md")
	fmt.Fprintln(w, "# AI review rules")
	fmt.Fprintln(w)
	for _, rule := range rules {
		fmt.Fprintf(w, "## %s — %s\n\n", rule.ID, rule.Title)
		fmt.Fprintf(w, "- **Language:** %s\n- **Scope:** `%s`\n- **Status:** %s\n", rule.Language, rule.Scope, rule.Status)
		if rule.DisabledReason != "" {
			fmt.Fprintf(w, "- **Reason:** %s\n", rule.DisabledReason)
		}
		fmt.Fprintf(w, "- **Rule:** %s\n\n", rule.Body)
	}
}

func (s *server) publishRules(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if s.publish == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "Publishing is not configured"})
		return
	}
	summary, err := s.publish(r.Context())
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]any{"error": err.Error(), "summary": summary})
		return
	}
	writeJSON(w, http.StatusOK, summary)
}

func (s *server) filteredRules(ctx context.Context, requestedStatus, language, query string) ([]ruleDTO, error) {
	statuses := []domain.RuleStatus{domain.RuleActive, domain.RuleDisabled, domain.RuleExpired}
	if requestedStatus != "" && requestedStatus != "all" {
		status := domain.RuleStatus(requestedStatus)
		if status != domain.RuleActive && status != domain.RuleDisabled && status != domain.RuleExpired {
			return nil, fmt.Errorf("invalid status %q", requestedStatus)
		}
		statuses = []domain.RuleStatus{status}
	}
	needle, language := strings.ToLower(strings.TrimSpace(query)), strings.ToLower(strings.TrimSpace(language))
	var out []ruleDTO
	for _, status := range statuses {
		rules, err := s.rules.ListByStatus(ctx, status)
		if err != nil {
			return nil, err
		}
		for _, rule := range rules {
			if language != "" && language != "all" && strings.ToLower(rule.Language) != language {
				continue
			}
			haystack := strings.ToLower(strings.Join([]string{rule.RuleID, rule.Title, rule.Scope, rule.Body, rule.Source}, " "))
			if needle != "" && !strings.Contains(haystack, needle) {
				continue
			}
			references, err := s.rules.ListReferences(ctx, rule.RuleID)
			if err != nil {
				return nil, err
			}
			dto := toDTO(rule)
			dto.CommentCount = len(references)
			for _, reference := range references {
				dto.ThumbsDown += reference.ThumbsDown
			}
			out = append(out, dto)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func toDTO(rule domain.Rule) ruleDTO {
	lastActivity := rule.CreatedAt
	if rule.ActivatedAt.After(lastActivity) {
		lastActivity = rule.ActivatedAt
	}
	if rule.LastUsedAt != nil && rule.LastUsedAt.After(lastActivity) {
		lastActivity = *rule.LastUsedAt
	}
	if rule.DisabledAt != nil && rule.DisabledAt.After(lastActivity) {
		lastActivity = *rule.DisabledAt
	}
	dto := ruleDTO{ID: rule.RuleID, Language: rule.Language, Title: rule.Title, Scope: rule.Scope, Body: rule.Body, Status: string(rule.Status), CreatedAt: displayTime(rule.CreatedAt), ActivatedAt: displayTime(rule.ActivatedAt), LastActivity: displayTime(lastActivity), DisabledReason: string(rule.DisabledReason), Source: rule.Source, ContentHash: rule.ContentHash}
	if rule.LastUsedAt != nil {
		dto.LastUsedAt = displayTime(*rule.LastUsedAt)
	}
	if rule.DisabledAt != nil {
		dto.DisabledAt = displayTime(*rule.DisabledAt)
	}
	return dto
}

func displayTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format(time.RFC3339)
}
func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
func writeError(w http.ResponseWriter, err error) {
	writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
}
