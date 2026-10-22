package application

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/lautaromei/ai-code-reviewer/internal/application/ports"
	"github.com/lautaromei/ai-code-reviewer/internal/domain"
)

type contextGitHub struct {
	e2eGitHub
	files        map[string]string
	directories  map[string][]string
	reads, lists []string
	changedFiles []ports.ChangedFile
}

func (g *contextGitHub) ListChangedFiles(ctx context.Context, repo string, pr int64) ([]ports.ChangedFile, error) {
	if g.changedFiles != nil {
		return g.changedFiles, nil
	}
	return g.e2eGitHub.ListChangedFiles(ctx, repo, pr)
}

func (g *contextGitHub) ReadFileAtSHA(_ context.Context, _ string, file, sha string) ([]byte, error) {
	key := sha + ":" + file
	g.reads = append(g.reads, key)
	if content, ok := g.files[key]; ok {
		return []byte(content), nil
	}
	return nil, errors.New("missing source")
}

func (g *contextGitHub) ListDirectoryAtSHA(_ context.Context, _ string, directory, _ string) ([]string, error) {
	g.lists = append(g.lists, directory)
	return append([]string(nil), g.directories[directory]...), nil
}

func TestReviewContextKeepsVersionsLineNumbersAndRelatedCodeWithinBudget(t *testing.T) {
	gh := &contextGitHub{
		files: map[string]string{
			"head:app/clock.go":     "package app\nfunc ExecuteTask() {\n ValidateTask()\n}\n",
			"base:app/old.go":       "package app\nfunc ExecuteTask() {\n if Ready() { ValidateTask() }\n}\n",
			"head:app/validate.go":  "package app\nfunc ValidateTask() {\n panic(\"requires Ready\")\n}\n",
			"head:app/unrelated.go": "package app\nconst unrelated = 42\n",
		},
		directories: map[string][]string{"app": {"app/logo.svg", "app/unrelated.go", "app/validate.go", "app/clock.go", "app/validate_test.go"}},
	}
	files := []ports.ChangedFile{{Path: "app/clock.go", PreviousPath: "app/old.go", Status: "renamed", Deletions: 1, Patch: "@@ -2,3 +2,3 @@\n func ExecuteTask() {\n- if Ready() { ValidateTask() }\n+ ValidateTask()\n }"}}
	patch := files[0].Patch
	got, limitations := collectReviewContext(context.Background(), gh, ReviewOptions{Repository: "a/b", MaxSourceContextBytes: 1600}, ports.PullRequest{HeadSHA: "head", BaseSHA: "base"}, files, len(patch))
	encoded, _ := json.Marshal(got)
	if len(encoded) > 1600 || len(got) != 3 {
		t.Fatalf("expected bounded HEAD/BASE/helper excerpts: %s", encoded)
	}
	if got[0].Revision != "HEAD" || !strings.Contains(got[0].Content, "3:  ValidateTask()") || got[1].Revision != "BASE" || got[1].Path != "app/old.go" {
		t.Fatalf("lost source revisions or line numbers: %#v", got)
	}
	if got[2].Path != "app/validate.go" || len(got[2].RelatedTo) != 1 || got[2].RelatedTo[0] != "app/clock.go" {
		t.Fatalf("missing helper provenance: %#v", got[2])
	}
	if files[0].Patch != patch || len(limitations) == 0 {
		t.Fatalf("patch changed or sampling was not disclosed: %#v", files)
	}
	for _, read := range gh.reads {
		if strings.Contains(read, "logo") || strings.Contains(read, "_test.go") {
			t.Fatalf("unexpected source read: %s", read)
		}
	}
}

func TestReviewContextHonorsCombinedBudgetAndDisableSetting(t *testing.T) {
	for _, limit := range []int{-1, 1024} {
		gh := &contextGitHub{files: map[string]string{"head:main.go": strings.Repeat("callSomething(\"<&>\")\n", 100)}}
		got, _ := collectReviewContext(context.Background(), gh, ReviewOptions{MaxSourceContextBytes: limit, MaxContextBytes: 800}, ports.PullRequest{HeadSHA: "head"}, []ports.ChangedFile{{Path: "main.go", Status: "added", Patch: "@@ -0,0 +50,1 @@\n+callSomething()"}}, 300)
		if limit < 0 {
			if len(got) != 0 || len(gh.reads) != 0 {
				t.Fatal("disabled context performed reads")
			}
		} else {
			encoded, _ := json.Marshal(got)
			if len(got) == 0 || len(encoded) > 500 {
				t.Fatalf("combined context budget exceeded: %d bytes: %#v", len(encoded), got)
			}
		}
	}
}

func TestReviewContextBoundsDiscoveryAndSurvivesUnavailableSources(t *testing.T) {
	gh := &contextGitHub{files: map[string]string{}, directories: map[string][]string{}}
	var files []ports.ChangedFile
	for i := 0; i < 8; i++ {
		file := fmt.Sprintf("pkg%d/main.go", i)
		files = append(files, ports.ChangedFile{Path: file, Deletions: 1, Patch: "@@ -1 +1 @@\n-ValidateTask()\n+ExecuteTask()"})
		gh.files["head:"+file] = "func ExecuteTask() { ValidateTask() }"
		for j := 0; j < 10; j++ {
			helper := fmt.Sprintf("pkg%d/helper%d.go", i, j)
			gh.directories[fmt.Sprintf("pkg%d", i)] = append(gh.directories[fmt.Sprintf("pkg%d", i)], helper)
			gh.files["head:"+helper] = "func ValidateTask() {}"
		}
	}
	got, limitations := collectReviewContext(context.Background(), gh, ReviewOptions{}, ports.PullRequest{HeadSHA: "head", BaseSHA: "base"}, files, 0)
	if len(gh.reads) > 12 || len(gh.lists) > 2 {
		t.Fatalf("unbounded discovery: %d reads, %d listings", len(gh.reads), len(gh.lists))
	}
	if len(got) == 0 || !strings.Contains(strings.Join(limitations, "\n"), "source context unavailable") {
		t.Fatalf("unavailable BASE sources must not abort useful context: %#v %v", got, limitations)
	}
}

func TestReviewContextLinksOtherChangedFilesForVerification(t *testing.T) {
	gh := &contextGitHub{files: map[string]string{
		"head:service.go": "func ExecuteTask() {}",
		"head:handler.go": "func HandleRequest() { ExecuteTask() }",
	}}
	files := []ports.ChangedFile{
		{Path: "service.go", Status: "added", Patch: "@@ -0,0 +1 @@\n+func ExecuteTask() {}"},
		{Path: "handler.go", Status: "added", Patch: "@@ -0,0 +1 @@\n+func HandleRequest() { ExecuteTask() }"},
	}
	got, _ := collectReviewContext(context.Background(), gh, ReviewOptions{}, ports.PullRequest{HeadSHA: "head"}, files, 0)
	if len(got) != 2 || len(got[1].RelatedTo) != 1 || got[1].RelatedTo[0] != "service.go" {
		t.Fatalf("changed caller lost its relation: %#v", got)
	}
}

type recordingGenerationModel struct {
	mu       sync.Mutex
	payloads []ports.ReviewPayload
	findings []ports.ModelFinding
}

func (m *recordingGenerationModel) Review(_ context.Context, _ string, payload ports.ReviewPayload) (ports.ReviewModelResult, error) {
	m.mu.Lock()
	m.payloads = append(m.payloads, payload)
	m.mu.Unlock()
	return ports.ReviewModelResult{Findings: m.findings}, nil
}

func TestGenerationCapsOversizedModelOutputAfterRanking(t *testing.T) {
	m := &recordingGenerationModel{}
	for i := 0; i < 9; i++ {
		m.findings = append(m.findings, ports.ModelFinding{Body: "low", Severity: "low"})
	}
	m.findings = append(m.findings, ports.ModelFinding{Body: "critical", Severity: "high"})
	got, _, err := generateReviewCandidates(context.Background(), []GenerationModel{{Name: "sonnet", Model: m}}, "", ports.ReviewPayload{MaxFindings: 1})
	if err != nil || len(got.Findings) != 8 || got.Findings[0].Body != "critical" {
		t.Fatalf("oversized response lost severe finding or exceeded budget: %#v, %v", got, err)
	}
}

type contextReviewStore struct{ ReviewStore }

func (contextReviewStore) Rules(context.Context, string) ([]ports.ReviewRule, error) { return nil, nil }

type contextReviewPrompts struct{ PromptResolver }

func (contextReviewPrompts) Effective(context.Context, string) (domain.ReviewPrompt, error) {
	return domain.ReviewPrompt{Revision: domain.ReviewPromptRevision{Text: "repository review policy"}}, nil
}
func (contextReviewPrompts) Validation(context.Context) (domain.ReviewPrompt, error) {
	return domain.ReviewPrompt{Revision: domain.ReviewPromptRevision{Text: "verification policy"}}, nil
}

type contextPipelineModel struct{ payloads []ports.ReviewPayload }

func (m *contextPipelineModel) Review(_ context.Context, _ string, p ports.ReviewPayload) (ports.ReviewModelResult, error) {
	m.payloads = append(m.payloads, p)
	if p.Stage == "verification" {
		return ports.ReviewModelResult{Findings: []ports.ModelFinding{{CandidateID: p.CandidateFindings[0].CandidateID, Verdict: "accept"}}}, nil
	}
	return ports.ReviewModelResult{Findings: []ports.ModelFinding{{Kind: "general", Path: "main.go", Line: 2, Confidence: 90, Severity: "high", Body: "Concrete failure"}}}, nil
}

func TestReviewPipelineKeepsUnavailableDiffSeparateFromSourceAndUsesExistingCalls(t *testing.T) {
	gh := &contextGitHub{files: map[string]string{"head:main.go": "package main\nfunc ExecuteTask() {}", "base:main.go": "package main\nfunc ExecuteTask() { guard() }"}, changedFiles: []ports.ChangedFile{{Path: "main.go", Truncated: true, Status: "modified"}}}
	m := &contextPipelineModel{}
	got, err := ReviewPullRequest(context.Background(), ReviewDeps{GitHub: gh, Model: m, Store: contextReviewStore{}, Prompts: contextReviewPrompts{}}, ReviewOptions{Repository: "a/b", PullRequest: 7, DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(m.payloads) != 2 || m.payloads[0].Stage != "review" || m.payloads[1].Stage != "verification" {
		t.Fatalf("expected one generation and one judge call: %#v", m.payloads)
	}
	for _, p := range m.payloads {
		if p.ChangedFiles[0].Patch != "" || len(p.ContextFiles) != 2 {
			t.Fatalf("source replaced diff or was lost before verification: %#v", p)
		}
	}
	if m.payloads[1].ReviewPolicy != "repository review policy" {
		t.Fatal("judge lost repository policy")
	}
	if len(got.Findings) != 1 || got.Findings[0].Path != "" || !strings.Contains(strings.Join(got.Limitations, "\n"), "diff unavailable") {
		t.Fatalf("missing diff should be explicit and cannot produce an inline location: %#v", got)
	}
}

func TestGenerationSpecializesExistingCallsAndSharesCandidateBudget(t *testing.T) {
	m := &recordingGenerationModel{}
	models := []GenerationModel{{Name: "sonnet", Model: m}, {Name: "sonnet", Model: m}, {Name: "opus", Model: m}}
	_, _, err := generateReviewCandidates(context.Background(), models, "policy", ports.ReviewPayload{MaxFindings: 10})
	if err != nil {
		t.Fatal(err)
	}
	budget, focuses := 0, map[string]bool{}
	for _, payload := range m.payloads {
		budget += payload.MaxFindings
		focuses[payload.ReviewFocus] = true
	}
	if len(m.payloads) != 3 || len(focuses) != 3 || budget != 10 {
		t.Fatalf("expected three specialized calls sharing 10 candidates: %#v", m.payloads)
	}
}
