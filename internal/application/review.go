package application

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/lautaromei/ai-code-reviewer/internal/application/ports"
	"github.com/lautaromei/ai-code-reviewer/internal/domain"
)

type ReviewStore interface {
	CreateJob(context.Context, string, int64, string) (domain.ReviewJob, bool, error)
	Job(context.Context, string, int64) (domain.ReviewJob, error)
	SetStatus(context.Context, int64, domain.ReviewJobStatus, string, string) error
	StartAttempt(context.Context, int64, string, string, string) (int64, error)
	FinishAttempt(context.Context, int64, string, string, int64, int64, int64, int64, int64) error
	Rules(context.Context, string) ([]ports.ReviewRule, error)
	SaveRuleInputs(context.Context, int64, []ports.ReviewRule) error
	SaveFinding(context.Context, int64, domain.ReviewFinding) (int64, error)
	SaveVerificationDecisions(context.Context, int64, []domain.VerificationDecision) error
	Published(context.Context, int64, int64) error
	RecordOwnComment(context.Context, domain.ReviewJob, domain.ReviewFinding, int64) error
	CapturePrompt(context.Context, int64, domain.ReviewPrompt) error
}
type PromptResolver interface {
	Effective(context.Context, string) (domain.ReviewPrompt, error)
	Validation(context.Context) (domain.ReviewPrompt, error)
	ByHash(context.Context, string, string) (domain.ReviewPrompt, error)
}
type ReviewDeps struct {
	GitHub                                     ports.PullRequestGateway
	Checks                                     ports.CheckRunReporter
	Model, VerificationModel                   ports.ReviewModel
	GenerationModels                           []GenerationModel
	Store                                      ReviewStore
	Prompts                                    PromptResolver
	Provider, ModelName, VerificationModelName string
}
type GenerationModel struct {
	Name  string
	Model ports.ReviewModel
}
type ReviewOptions struct {
	Repository                                           string
	PullRequest                                          int64
	DryRun                                               bool
	RuleBaseURL                                          string
	Categories                                           []string
	MaxComments, MaxFiles, MaxFileBytes, MaxContextBytes int
}
type ReviewResult struct {
	JobID       int64                  `json:"job_id"`
	Status      domain.ReviewJobStatus `json:"status"`
	Summary     string                 `json:"summary"`
	Findings    []domain.ReviewFinding `json:"findings"`
	Limitations []string               `json:"limitations"`
}

func ReviewPullRequest(ctx context.Context, d ReviewDeps, o ReviewOptions) (ReviewResult, error) {
	var result ReviewResult
	if o.MaxComments <= 0 {
		o.MaxComments = 20
	}
	pr, e := d.GitHub.GetPullRequest(ctx, o.Repository, o.PullRequest)
	if e != nil {
		return result, e
	}
	if o.DryRun {
		return executeReview(ctx, d, o, domain.ReviewJob{Repository: o.Repository, PullRequestNumber: o.PullRequest, InitialHeadSHA: pr.HeadSHA}, pr, 0, true)
	}
	job, created, e := d.Store.CreateJob(ctx, o.Repository, o.PullRequest, pr.HeadSHA)
	if e != nil {
		return result, e
	}
	result.JobID = job.ID
	if !created {
		result.Status = job.Status
		result.Summary = "pull request already has a review job"
		return result, nil
	}
	return executeReview(ctx, d, o, job, pr, job.ID, false)
}

func ProcessReviewJob(ctx context.Context, d ReviewDeps, o ReviewOptions, job domain.ReviewJob) (ReviewResult, error) {
	pr, e := d.GitHub.GetPullRequest(ctx, job.Repository, job.PullRequestNumber)
	if e != nil {
		return ReviewResult{JobID: job.ID, Status: domain.ReviewFailed}, e
	}
	o.Repository = job.Repository
	o.PullRequest = job.PullRequestNumber
	if pr.HeadSHA != job.InitialHeadSHA {
		_ = d.Store.SetStatus(ctx, job.ID, domain.ReviewSkipped, "head_changed", "")
		return ReviewResult{JobID: job.ID, Status: domain.ReviewSkipped, Summary: "head changed"}, nil
	}
	return executeReview(ctx, d, o, job, pr, job.ID, false)
}
func executeReview(ctx context.Context, d ReviewDeps, o ReviewOptions, job domain.ReviewJob, pr ports.PullRequest, jobID int64, dry bool) (ReviewResult, error) {
	out := ReviewResult{JobID: jobID}
	var check ports.CheckRun
	fail := func(err error) (ReviewResult, error) {
		if !dry && jobID > 0 {
			_ = d.Store.SetStatus(ctx, jobID, domain.ReviewFailed, "", err.Error())
		}
		out.Status = domain.ReviewFailed
		if check.ID > 0 {
			_ = d.Checks.CompleteCheckRun(ctx, o.Repository, check.ID, "failure", "Review failed", err.Error())
		}
		return out, err
	}
	if pr.State != "open" {
		if !dry {
			_ = d.Store.SetStatus(ctx, jobID, domain.ReviewSkipped, "closed", "")
		}
		out.Status = domain.ReviewSkipped
		out.Summary = "pull request is closed"
		return out, nil
	}
	if !dry && d.Checks != nil {
		var checkErr error
		check, checkErr = d.Checks.StartCheckRun(ctx, o.Repository, pr.HeadSHA, "AI Code Review")
		if checkErr != nil {
			return out, fmt.Errorf("start GitHub check: %w", checkErr)
		}
	}
	files, e := d.GitHub.ListChangedFiles(ctx, o.Repository, o.PullRequest)
	if e != nil {
		return fail(e)
	}
	var limitations []string
	contextBytes := 0
	if o.MaxFiles > 0 && len(files) > o.MaxFiles {
		files = files[:o.MaxFiles]
		limitations = append(limitations, "file limit reached; review is incomplete")
	}
	for i := range files {
		if files[i].Truncated {
			if data, x := d.GitHub.ReadFileAtSHA(ctx, o.Repository, files[i].Path, pr.HeadSHA); x == nil {
				if o.MaxFileBytes > 0 && len(data) > o.MaxFileBytes {
					limitations = append(limitations, "context truncated for "+files[i].Path)
				} else {
					files[i].Patch = string(data)
				}
			} else {
				limitations = append(limitations, "diff unavailable for "+files[i].Path)
			}
		}
		contextBytes += len(files[i].Patch)
		if o.MaxContextBytes > 0 && contextBytes > o.MaxContextBytes {
			files = files[:i]
			limitations = append(limitations, "context byte limit reached; review is incomplete")
			break
		}
	}
	rules, e := d.Store.Rules(ctx, o.Repository)
	if e != nil {
		return fail(e)
	}
	rules = filterRules(rules, files)
	var prompt domain.ReviewPrompt
	if job.PromptHash != "" {
		prompt, e = d.Prompts.ByHash(ctx, o.Repository, job.PromptHash)
	} else {
		prompt, e = d.Prompts.Effective(ctx, o.Repository)
	}
	if e != nil {
		return fail(e)
	}
	if !dry {
		if e = d.Store.CapturePrompt(ctx, jobID, prompt); e != nil {
			return fail(e)
		}
	}
	attempt := int64(0)
	if !dry {
		attemptModel := d.ModelName
		if len(d.GenerationModels) > 0 {
			names := make([]string, len(d.GenerationModels))
			for i := range d.GenerationModels {
				names[i] = d.GenerationModels[i].Name
			}
			attemptModel = strings.Join(names, ",")
		}
		attempt, e = d.Store.StartAttempt(ctx, jobID, d.Provider, attemptModel, "review-v1")
		if e != nil {
			return fail(e)
		}
		if e = d.Store.SaveRuleInputs(ctx, attempt, rules); e != nil {
			return fail(e)
		}
	}
	payload := ports.ReviewPayload{Repository: o.Repository, PullRequest: pr, SelectedCategories: o.Categories, Rules: rules, ChangedFiles: files, ContextFiles: map[string]string{}, Limitations: limitations, MaxFindings: o.MaxComments, Stage: "review"}
	generationModels := d.GenerationModels
	if len(generationModels) == 0 {
		generationModels = []GenerationModel{{Name: d.ModelName, Model: d.Model}}
	}
	mr, generationCost, e := generateReviewCandidates(ctx, generationModels, prompt.Revision.Text, payload)
	if e != nil {
		if attempt > 0 {
			_ = d.Store.FinishAttempt(ctx, attempt, "error", e.Error(), 0, 0, 0, 0, 0)
		}
		return fail(e)
	}
	for i := range mr.Findings {
		mr.Findings[i].CandidateID = fmt.Sprintf("candidate-%d", i+1)
	}
	candidateCount := len(mr.Findings)
	inputTokens, outputTokens := mr.InputTokens, mr.OutputTokens
	cacheCreationTokens, cacheReadTokens := mr.CacheCreationTokens, mr.CacheReadTokens
	costMicroUSD := generationCost
	if candidateCount > 0 {
		verificationPayload := payload
		verificationPayload.Stage = "verification"
		verificationPayload.CandidateFindings = mr.Findings
		validationPrompt, validationErr := d.Prompts.Validation(ctx)
		if validationErr != nil {
			return fail(fmt.Errorf("load validation prompt: %w", validationErr))
		}
		verificationModel := d.VerificationModel
		if verificationModel == nil {
			verificationModel = d.Model
		}
		verified, verificationErr := verificationModel.Review(ctx, validationPrompt.Revision.Text, verificationPayload)
		if verificationErr != nil {
			if attempt > 0 {
				_ = d.Store.FinishAttempt(ctx, attempt, "verification_error", verificationErr.Error(), inputTokens, outputTokens, cacheCreationTokens, cacheReadTokens, costMicroUSD)
			}
			return fail(fmt.Errorf("verify review candidates: %w", verificationErr))
		}
		inputTokens += verified.InputTokens
		outputTokens += verified.OutputTokens
		cacheCreationTokens += verified.CacheCreationTokens
		cacheReadTokens += verified.CacheReadTokens
		verificationModelName := d.VerificationModelName
		if verificationModelName == "" {
			verificationModelName = d.ModelName
		}
		costMicroUSD += reviewCostMicroUSD(verificationModelName, verified.InputTokens, verified.OutputTokens, verified.CacheCreationTokens, verified.CacheReadTokens)
		verifiedFindings, decisions, verificationErr := selectVerifiedFindings(mr.Findings, verified.Findings)
		if verificationErr != nil {
			if attempt > 0 {
				_ = d.Store.FinishAttempt(ctx, attempt, "verification_error", verificationErr.Error(), inputTokens, outputTokens, cacheCreationTokens, cacheReadTokens, costMicroUSD)
			}
			return fail(verificationErr)
		}
		if attempt > 0 {
			if verificationErr = d.Store.SaveVerificationDecisions(ctx, attempt, decisions); verificationErr != nil {
				return fail(fmt.Errorf("save verification decisions: %w", verificationErr))
			}
		}
		logVerificationDecisions(o, jobID, decisions)
		mr.Findings = verifiedFindings
		mr.Limitations = append(mr.Limitations, verified.Limitations...)
	}
	valid, rejected := validateFindingsObserved(mr.Findings, rules, files, o.MaxComments, func(f ports.ModelFinding, reason string) {
		logDiscardedFinding(o, jobID, "validation", reason, f)
	})
	out.Summary = finalReviewSummary(mr.Summary, valid, candidateCount)
	out.Limitations = append(limitations, mr.Limitations...)
	out.Limitations = append(out.Limitations, rejected...)
	for _, f := range valid {
		df := toFinding(attempt, f)
		out.Findings = append(out.Findings, df)
		if dry {
			continue
		}
		id, e := d.Store.SaveFinding(ctx, attempt, df)
		if e != nil {
			return fail(e)
		}
		current, e := d.GitHub.GetPullRequest(ctx, o.Repository, o.PullRequest)
		if e != nil {
			return fail(e)
		}
		if current.State != "open" || current.HeadSHA != pr.HeadSHA {
			reason := "head_changed"
			if current.State != "open" {
				reason = "closed"
			}
			_ = d.Store.SetStatus(ctx, jobID, domain.ReviewSkipped, reason, "")
			out.Status = domain.ReviewSkipped
			return out, nil
		}
		marker := fmt.Sprintf("<!-- ai-reviewer:finding:%s -->", df.Fingerprint)
		existing, e := d.GitHub.ListPullRequestComments(ctx, o.Repository, o.PullRequest)
		if e != nil {
			return fail(e)
		}
		var remote int64
		for _, x := range existing {
			if strings.Contains(x.Body, marker) {
				remote = x.ID
				break
			}
		}
		if remote == 0 {
			body := publicationBody(df, marker, o.RuleBaseURL)
			var published ports.PublishedComment
			if df.Path != "" && df.Line > 0 {
				published, e = d.GitHub.PublishReviewComment(ctx, o.Repository, o.PullRequest, pr.HeadSHA, ports.ReviewPublishRequest{Body: body, Path: df.Path, Line: df.Line, Side: df.Side})
			} else {
				published, e = d.GitHub.PublishIssueComment(ctx, o.Repository, o.PullRequest, body)
			}
			if e != nil {
				return fail(e)
			}
			remote = published.ID
		}
		if e = d.Store.Published(ctx, id, remote); e != nil {
			return fail(e)
		}
		if e = d.Store.RecordOwnComment(ctx, job, df, remote); e != nil {
			return fail(e)
		}
	}
	if attempt > 0 {
		summaryBody := publicationSummary(out.Summary, pr.HeadSHA)
		if _, e = d.GitHub.PublishIssueComment(ctx, o.Repository, o.PullRequest, summaryBody); e != nil {
			return fail(e)
		}
		_ = d.Store.FinishAttempt(ctx, attempt, "completed", "", inputTokens, outputTokens, cacheCreationTokens, cacheReadTokens, costMicroUSD)
		_ = d.Store.SetStatus(ctx, jobID, domain.ReviewCompleted, "", "")
		if check.ID > 0 {
			conclusion := "success"
			if len(out.Findings) > 0 {
				conclusion = "neutral"
			}
			if e = d.Checks.CompleteCheckRun(ctx, o.Repository, check.ID, conclusion, "Review completed", fmt.Sprintf("Found %d actionable issue(s).", len(out.Findings))); e != nil {
				return fail(e)
			}
		}
	}
	out.Status = domain.ReviewCompleted
	return out, nil
}

func generateReviewCandidates(ctx context.Context, models []GenerationModel, prompt string, payload ports.ReviewPayload) (ports.ReviewModelResult, int64, error) {
	if len(models) == 0 {
		return ports.ReviewModelResult{}, 0, errors.New("at least one generation model is required")
	}
	results := make([]ports.ReviewModelResult, len(models))
	errs := make([]error, len(models))
	var wg sync.WaitGroup
	for i := range results {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			results[index], errs[index] = models[index].Model.Review(ctx, prompt, payload)
		}(i)
	}
	wg.Wait()
	var out ports.ReviewModelResult
	var cost int64
	for i, result := range results {
		if errs[i] != nil {
			return ports.ReviewModelResult{}, 0, fmt.Errorf("generation agent %d (%s): %w", i+1, models[i].Name, errs[i])
		}
		if out.Summary == "" {
			out.Summary = result.Summary
		}
		out.Findings = append(out.Findings, result.Findings...)
		out.Limitations = append(out.Limitations, result.Limitations...)
		out.InputTokens += result.InputTokens
		out.OutputTokens += result.OutputTokens
		out.CacheCreationTokens += result.CacheCreationTokens
		out.CacheReadTokens += result.CacheReadTokens
		cost += reviewCostMicroUSD(models[i].Name, result.InputTokens, result.OutputTokens, result.CacheCreationTokens, result.CacheReadTokens)
	}
	return out, cost, nil
}

type tokenRatesMicroUSD struct {
	Input, Output, CacheCreation, CacheRead float64
}

// Rates per token from Anthropic's pricing table, checked 2026-10-15.
// https://platform.claude.com/docs/en/about-claude/pricing
var reviewTokenRates = map[string]tokenRatesMicroUSD{
	"sonnet":    {Input: 2, Output: 10, CacheCreation: 2.5, CacheRead: 0.1},
	"opus":      {Input: 4, Output: 20, CacheCreation: 5, CacheRead: 0.2},
	"haiku_max": {Input: 0.1, Output: 0.5, CacheCreation: 0.125, CacheRead: 0.01},
	// Jev billing is not token-priced like the generative models. Do not
	// misreport its decisions at the Sonnet fallback rate.
	"jev": {},
}

func reviewCostMicroUSD(model string, input, output, cacheCreation, cacheRead int64) int64 {
	rates, ok := reviewTokenRates[strings.ToLower(strings.TrimSpace(model))]
	if !ok {
		rates = reviewTokenRates["sonnet"]
	}
	if strings.EqualFold(strings.TrimSpace(model), "haiku_max") && input+cacheCreation+cacheRead > 100000 {
		rates = tokenRatesMicroUSD{Input: 0.5, Output: 2.5, CacheCreation: 0.625, CacheRead: 0.05}
	}
	return int64(float64(input)*rates.Input + float64(output)*rates.Output + float64(cacheCreation)*rates.CacheCreation + float64(cacheRead)*rates.CacheRead + 0.5)
}

func publicationBody(f domain.ReviewFinding, marker, ruleBaseURL string) string {
	body := normalizedFindingBody(f.Body)
	prefix := findingIcon(f.Confidence, f.Severity)
	if f.RuleID != nil {
		rule := *f.RuleID
		if strings.TrimSpace(ruleBaseURL) != "" {
			rule = "[" + rule + "](" + strings.TrimRight(ruleBaseURL, "/") + "/rules/" + url.PathEscape(rule) + ")"
		}
		prefix += " " + rule
	}
	body = fmt.Sprintf("%s · %d%% — %s", prefix, f.Confidence, body)
	if strings.TrimSpace(f.Suggestion) != "" {
		body += "\n\n```suggestion\n" + strings.Trim(f.Suggestion, "\r\n") + "\n```"
	}
	return body + "\n\n" + marker
}

func linkRuleReference(body, ruleID, target string) string {
	linked := "[" + ruleID + "](" + target + ")"
	if strings.Contains(body, target) {
		return body
	}
	if strings.Contains(body, "["+ruleID+"]") {
		return strings.Replace(body, "["+ruleID+"]", linked, 1)
	}
	if strings.Contains(body, "`"+ruleID+"`") {
		return strings.Replace(body, "`"+ruleID+"`", "[`"+ruleID+"`]("+target+")", 1)
	}
	if strings.Contains(body, ruleID) {
		return strings.Replace(body, ruleID, linked, 1)
	}
	return linked + " — " + body
}

func publicationSummary(summary, sha string) string {
	return strings.TrimSpace(summary) + fmt.Sprintf("\n\n<!-- ai-reviewer:summary:%s -->", sha)
}

func finalReviewSummary(original string, findings []ports.ModelFinding, candidateCount int) string {
	if len(findings) == candidateCount {
		return original
	}
	if len(findings) == 0 {
		return "✅ No actionable issues found in the proposed changes."
	}
	var b strings.Builder
	noun := "issues"
	if len(findings) == 1 {
		noun = "issue"
	}
	fmt.Fprintf(&b, "Found %d actionable %s:", len(findings), noun)
	for _, finding := range findings {
		icon := findingIcon(finding.Confidence, finding.Severity)
		location := finding.Path
		if finding.Line > 0 {
			location += ":" + strconv.Itoa(finding.Line)
		}
		detail := firstFindingParagraph(finding.Body)
		fmt.Fprintf(&b, "\n\n%s `%s` · %d%% — %s", icon, location, finding.Confidence, detail)
	}
	return b.String()
}

func findingIcon(confidence int, severity string) string {
	if confidence >= 85 || strings.EqualFold(severity, "high") {
		return "🚨"
	}
	if confidence < 60 {
		return "🤔"
	}
	return "💡"
}

func firstFindingParagraph(body string) string {
	for _, line := range strings.Split(normalizedFindingBody(body), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			return strings.TrimSpace(strings.TrimPrefix(line, "- "))
		}
	}
	return ""
}

func normalizedFindingBody(body string) string {
	var lines []string
	for _, line := range strings.Split(strings.TrimSpace(body), "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "#") {
			continue
		}
		if strings.Contains(trimmed, "% confidence") || strings.Contains(trimmed, "% —") {
			if _, detail, ok := strings.Cut(trimmed, "—"); ok && strings.TrimSpace(detail) != "" {
				lines = append(lines, strings.TrimSpace(detail))
			}
			continue
		}
		lines = append(lines, line)
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

func selectVerifiedFindings(candidates, verdicts []ports.ModelFinding) ([]ports.ModelFinding, []domain.VerificationDecision, error) {
	known := make(map[string]bool, len(candidates))
	for _, candidate := range candidates {
		known[candidate.CandidateID] = true
	}
	decisions := make(map[string]ports.ModelFinding, len(verdicts))
	for _, finding := range verdicts {
		if finding.CandidateID == "" {
			continue
		}
		if !known[finding.CandidateID] {
			return nil, nil, fmt.Errorf("verifier returned unknown candidate ID %q", finding.CandidateID)
		}
		if _, duplicate := decisions[finding.CandidateID]; duplicate {
			return nil, nil, fmt.Errorf("verifier returned duplicate verdict for candidate ID %q", finding.CandidateID)
		}
		decisions[finding.CandidateID] = finding
	}
	out := make([]ports.ModelFinding, 0, len(candidates))
	records := make([]domain.VerificationDecision, 0, len(candidates))
	for _, candidate := range candidates {
		decision, present := decisions[candidate.CandidateID]
		verdict := decision.Verdict
		if !present || verdict == "" {
			verdict = "missing"
		}
		records = append(records, domain.VerificationDecision{
			CandidateID: candidate.CandidateID, Verdict: verdict, Reason: decision.Reason, DuplicateOf: decision.DuplicateOf,
			Kind: candidate.Kind, RuleID: candidate.RuleID, Path: candidate.Path, Line: candidate.Line,
			Confidence: candidate.Confidence, Severity: candidate.Severity, Body: candidate.Body, Suggestion: candidate.Suggestion,
		})
		if verdict == "accept" {
			out = append(out, candidate)
			continue
		}
		if verdict == "missing" || verdict == "uncertain" || verdict == "reject" || verdict == "low_value" {
			continue
		}
		if verdict == "duplicate" {
			if decision.DuplicateOf == "" || decision.DuplicateOf == candidate.CandidateID || !known[decision.DuplicateOf] {
				return nil, nil, fmt.Errorf("verifier returned invalid duplicate target %q for candidate ID %q", decision.DuplicateOf, candidate.CandidateID)
			}
			if target, decided := decisions[decision.DuplicateOf]; !decided || target.Verdict != "accept" {
				return nil, nil, fmt.Errorf("verifier returned non-canonical duplicate target %q for candidate ID %q", decision.DuplicateOf, candidate.CandidateID)
			}
			continue
		}
		return nil, nil, fmt.Errorf("verifier returned invalid verdict %q for candidate ID %q", verdict, candidate.CandidateID)
	}
	return out, records, nil
}

func logVerificationDecisions(o ReviewOptions, jobID int64, decisions []domain.VerificationDecision) {
	for _, decision := range decisions {
		slog.Info("review verification decision",
			"repository", o.Repository, "pull_request", o.PullRequest, "job_id", jobID,
			"candidate_id", decision.CandidateID, "verdict", decision.Verdict,
			"reason", decision.Reason, "duplicate_of", decision.DuplicateOf,
			"kind", decision.Kind, "rule_id", decision.RuleID, "path", decision.Path,
			"line", decision.Line, "confidence", decision.Confidence, "body", decision.Body)
	}
}

func logDiscardedFinding(o ReviewOptions, jobID int64, stage, reason string, f ports.ModelFinding) {
	slog.Warn("review finding discarded",
		"repository", o.Repository,
		"pull_request", o.PullRequest,
		"job_id", jobID,
		"stage", stage,
		"reason", reason,
		"candidate_id", f.CandidateID,
		"kind", f.Kind,
		"rule_id", f.RuleID,
		"path", f.Path,
		"line", f.Line,
		"confidence", f.Confidence,
		"verifier_reason", f.Reason,
		"body", f.Body,
		"suggestion", f.Suggestion,
	)
}

func filterRules(rules []ports.ReviewRule, files []ports.ChangedFile) []ports.ReviewRule {
	var out []ports.ReviewRule
	for _, r := range rules {
		patterns := scopePatterns(r.Scopes)
		if len(patterns) == 0 {
			out = append(out, r)
			continue
		}
		matched := false
		for _, f := range files {
			for _, g := range patterns {
				if glob(g, f.Path) {
					matched = true
				}
			}
		}
		if matched {
			out = append(out, r)
		}
	}
	return out
}
func glob(pattern, name string) bool {
	pattern = filepath.ToSlash(pattern)
	name = filepath.ToSlash(name)
	if strings.HasPrefix(pattern, "**/") {
		if ok, _ := filepath.Match(strings.TrimPrefix(pattern, "**/"), filepath.Base(name)); ok {
			return true
		}
	}
	ok, _ := filepath.Match(pattern, name)
	return ok
}
func validateFindings(in []ports.ModelFinding, rules []ports.ReviewRule, files []ports.ChangedFile, max int) ([]ports.ModelFinding, []string) {
	return validateFindingsObserved(in, rules, files, max, nil)
}

func validateFindingsObserved(in []ports.ModelFinding, rules []ports.ReviewRule, files []ports.ChangedFile, max int, observe func(ports.ModelFinding, string)) ([]ports.ModelFinding, []string) {
	known := map[string]ports.ReviewRule{}
	patches := map[string]map[int]bool{}
	for _, r := range rules {
		known[r.RuleID] = r
	}
	for _, f := range files {
		patches[f.Path] = changedLines(f.Patch)
	}
	seen := map[string]bool{}
	seenLocation := map[string]bool{}
	var out []ports.ModelFinding
	var rejected []string
	reject := func(f ports.ModelFinding, reason, limitation string) {
		if limitation != "" {
			rejected = append(rejected, limitation)
		}
		if observe != nil {
			observe(f, reason)
		}
	}
	for i, f := range in {
		if firstFindingParagraph(f.Body) == "" {
			reject(f, "empty_body", "ignored finding without an actionable body")
			continue
		}
		if f.Confidence < 40 {
			reject(f, "confidence_below_threshold", "")
			continue
		}
		if f.Kind != "catalog_rule" && f.Kind != "general" {
			reject(f, "invalid_kind", fmt.Sprintf("ignored finding with invalid kind %q", f.Kind))
			continue
		}
		if f.Kind == "catalog_rule" {
			r, ok := known[f.RuleID]
			if !ok {
				reject(f, "unavailable_rule", fmt.Sprintf("ignored finding referencing unavailable rule %q", f.RuleID))
				continue
			}
			if !ruleMatches(r, f.Path) {
				reject(f, "outside_rule_scope", fmt.Sprintf("ignored finding %s outside its rule scope", f.RuleID))
				continue
			}
		} else if f.RuleID != "" {
			reject(f, "general_with_rule_id", "ignored general finding carrying a rule ID")
			continue
		}
		if f.Confidence < 0 || f.Confidence > 100 {
			reject(f, "invalid_confidence", "ignored finding with confidence outside 0..100")
			continue
		}
		if f.Side == "" {
			f.Side = "RIGHT"
		}
		if f.Path != "" {
			lines, ok := patches[f.Path]
			if !ok || !lines[f.Line] {
				rejected = append(rejected, fmt.Sprintf("published %s:%d as a general comment because the line is not commentable", f.Path, f.Line))
				f.Path, f.Line, f.Side = "", 0, ""
			}
		}
		key := fmt.Sprintf("%s|%s|%d|%s", f.RuleID, f.Path, f.Line, strings.ToLower(strings.TrimSpace(f.Body)))
		if seen[key] {
			reject(f, "duplicate_finding", "")
			continue
		}
		if f.Path != "" && f.Line > 0 {
			location := fmt.Sprintf("%s:%d", f.Path, f.Line)
			if seenLocation[location] {
				reject(f, "duplicate_location", "ignored an additional finding on "+location+" to avoid duplicate review threads")
				continue
			}
			seenLocation[location] = true
		}
		seen[key] = true
		out = append(out, f)
		if len(out) >= max {
			for _, remaining := range in[i+1:] {
				reject(remaining, "max_findings_reached", "")
			}
			break
		}
	}
	return out, rejected
}
func changedLines(patch string) map[int]bool {
	out := map[int]bool{}
	line := 0
	hunk := regexp.MustCompile(`^@@ -\d+(?:,\d+)? \+(\d+)(?:,\d+)? @@`)
	for _, s := range strings.Split(patch, "\n") {
		if strings.HasPrefix(s, "@@") {
			if m := hunk.FindStringSubmatch(s); len(m) == 2 {
				line, _ = strconv.Atoi(m[1])
			}
			continue
		}
		if strings.HasPrefix(s, "+") && !strings.HasPrefix(s, "+++") {
			out[line] = true
			line++
		} else if !strings.HasPrefix(s, "-") {
			line++
		}
	}
	return out
}
func ruleMatches(r ports.ReviewRule, path string) bool {
	patterns := scopePatterns(r.Scopes)
	if len(patterns) == 0 {
		return true
	}
	for _, g := range patterns {
		if glob(g, path) {
			return true
		}
	}
	return false
}
func scopePatterns(scopes []string) []string {
	var out []string
	for _, scope := range scopes {
		for _, part := range strings.Split(scope, ",") {
			part = strings.Trim(strings.TrimSpace(part), "`")
			if part != "" {
				out = append(out, part)
			}
		}
	}
	return out
}
func toFinding(attempt int64, f ports.ModelFinding) domain.ReviewFinding {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s\x00%s\x00%d\x00%s", f.RuleID, f.Path, f.Line, strings.TrimSpace(f.Body))))
	var rid *string
	if f.RuleID != "" {
		x := f.RuleID
		rid = &x
	}
	return domain.ReviewFinding{AttemptID: attempt, RuleID: rid, Kind: domain.FindingKind(f.Kind), Path: f.Path, Side: f.Side, Line: f.Line, Confidence: f.Confidence, Severity: f.Severity, Body: f.Body, Suggestion: f.Suggestion, Fingerprint: fmt.Sprintf("%x", sum[:16]), PublicationStatus: "pending"}
}

var _ = json.Marshal
var _ = sort.Strings
var _ = time.Now
