package ghcli

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/lautaromei/ai-code-reviewer/internal/application/ports"
	"github.com/lautaromei/ai-code-reviewer/internal/domain"
)

type ghGitRef struct {
	Object struct {
		SHA string `json:"sha"`
	} `json:"object"`
}

type ghContentsFile struct {
	SHA string `json:"sha"`
}

type ghContentsResponse struct {
	Commit struct {
		SHA string `json:"sha"`
	} `json:"commit"`
}

type ghPullRequestHead struct {
	Ref string `json:"ref"`
}

type ghPullRequest struct {
	HTMLURL string            `json:"html_url"`
	Head    ghPullRequestHead `json:"head"`
}

func (g *GitHub) UpsertRulesPullRequest(ctx context.Context, request ports.PublicationRequest) (ports.PublicationResult, error) {
	if request.Repository == "" {
		return ports.PublicationResult{}, fmt.Errorf("ghcli: UpsertRulesPullRequest: repository cannot be empty")
	}
	if request.Branch == "" {
		return ports.PublicationResult{}, fmt.Errorf("ghcli: UpsertRulesPullRequest: branch cannot be empty")
	}
	if request.DefaultBranch == "" {
		return ports.PublicationResult{}, fmt.Errorf("ghcli: UpsertRulesPullRequest: defaultBranch cannot be empty")
	}
	if request.FilePath == "" {
		return ports.PublicationResult{}, fmt.Errorf("ghcli: UpsertRulesPullRequest: filePath cannot be empty")
	}

	if _, branchExists, err := g.refSHA(ctx, request.Repository, request.Branch); err != nil {
		return ports.PublicationResult{}, fmt.Errorf("ghcli: UpsertRulesPullRequest(%s): %w", request.Repository, err)
	} else if !branchExists {
		defaultSHA, defaultExists, err := g.refSHA(ctx, request.Repository, request.DefaultBranch)
		if err != nil {
			return ports.PublicationResult{}, fmt.Errorf("ghcli: UpsertRulesPullRequest(%s): default branch: %w", request.Repository, err)
		}
		if !defaultExists {
			return ports.PublicationResult{}, fmt.Errorf("ghcli: UpsertRulesPullRequest(%s): default branch %q does not exist", request.Repository, request.DefaultBranch)
		}
		if err := g.createRef(ctx, request.Repository, request.Branch, defaultSHA); err != nil {
			return ports.PublicationResult{}, fmt.Errorf("ghcli: UpsertRulesPullRequest(%s): create branch %q: %w", request.Repository, request.Branch, err)
		}
	}

	existingSHA, err := g.fileSHA(ctx, request.Repository, request.FilePath, request.Branch)
	if err != nil {
		return ports.PublicationResult{}, fmt.Errorf("ghcli: UpsertRulesPullRequest(%s): %w", request.Repository, err)
	}

	commitSHA, err := g.putContents(ctx, request, existingSHA)
	if err != nil {
		return ports.PublicationResult{}, fmt.Errorf("ghcli: UpsertRulesPullRequest(%s): %w", request.Repository, err)
	}

	prURL, alreadyOpen, err := g.findOpenPullRequest(ctx, request.Repository, request.Branch)
	if err != nil {
		return ports.PublicationResult{}, fmt.Errorf("ghcli: UpsertRulesPullRequest(%s): %w", request.Repository, err)
	}
	if alreadyOpen {
		return ports.PublicationResult{Status: domain.PublicationUpdated, CommitSHA: commitSHA, PullRequestURL: prURL}, nil
	}

	prURL, err = g.createPullRequest(ctx, request)
	if err != nil {
		return ports.PublicationResult{}, fmt.Errorf("ghcli: UpsertRulesPullRequest(%s): create pull request: %w", request.Repository, err)
	}

	return ports.PublicationResult{Status: domain.PublicationCreated, CommitSHA: commitSHA, PullRequestURL: prURL}, nil
}

func (g *GitHub) refSHA(ctx context.Context, repo string, branch string) (sha string, exists bool, err error) {
	endpoint := fmt.Sprintf("repos/%s/git/ref/heads/%s", repo, branch)
	stdout, err := g.apiGet(ctx, endpoint)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return "", false, nil
		}
		return "", false, fmt.Errorf("look up branch %q: %w", branch, err)
	}

	var ref ghGitRef
	if err := json.Unmarshal(stdout, &ref); err != nil {
		return "", false, fmt.Errorf("invalid response while looking up branch %q: %w", branch, err)
	}
	if ref.Object.SHA == "" {
		return "", false, fmt.Errorf("branch lookup response for %q does not include sha", branch)
	}
	return ref.Object.SHA, true, nil
}

func (g *GitHub) createRef(ctx context.Context, repo string, branch string, sha string) error {
	endpoint := fmt.Sprintf("repos/%s/git/refs", repo)
	body := map[string]string{
		"ref": "refs/heads/" + branch,
		"sha": sha,
	}
	if _, err := g.apiMutate(ctx, "POST", endpoint, body); err != nil {
		return fmt.Errorf("create ref refs/heads/%s: %w", branch, err)
	}
	return nil
}

func (g *GitHub) fileSHA(ctx context.Context, repo string, path string, branch string) (string, error) {
	endpoint := fmt.Sprintf("repos/%s/contents/%s?ref=%s", repo, path, url.QueryEscape(branch))
	stdout, err := g.apiGet(ctx, endpoint)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return "", nil
		}
		return "", fmt.Errorf("look up file %q on branch %q: %w", path, branch, err)
	}

	var file ghContentsFile
	if err := json.Unmarshal(stdout, &file); err != nil {
		return "", fmt.Errorf("invalid response while looking up file %q: %w", path, err)
	}
	return file.SHA, nil
}

func (g *GitHub) putContents(ctx context.Context, request ports.PublicationRequest, existingSHA string) (string, error) {
	endpoint := fmt.Sprintf("repos/%s/contents/%s", request.Repository, request.FilePath)
	body := map[string]string{
		"message": request.CommitMessage,
		"content": base64.StdEncoding.EncodeToString(request.Content),
		"branch":  request.Branch,
	}
	if existingSHA != "" {
		body["sha"] = existingSHA
	}

	stdout, err := g.apiMutate(ctx, "PUT", endpoint, body)
	if err != nil {
		return "", fmt.Errorf("write file %q on branch %q: %w", request.FilePath, request.Branch, err)
	}

	var resp ghContentsResponse
	if err := json.Unmarshal(stdout, &resp); err != nil {
		return "", fmt.Errorf("invalid response while writing file %q: %w", request.FilePath, err)
	}
	return resp.Commit.SHA, nil
}

func (g *GitHub) findOpenPullRequest(ctx context.Context, repo string, branch string) (prURL string, found bool, err error) {
	owner := repo
	if idx := strings.IndexByte(repo, '/'); idx >= 0 {
		owner = repo[:idx]
	}

	endpoint := fmt.Sprintf("repos/%s/pulls?state=open&head=%s", repo, url.QueryEscape(owner+":"+branch))
	stdout, err := g.apiGet(ctx, endpoint)
	if err != nil {
		return "", false, fmt.Errorf("find open pull request for branch %q: %w", branch, err)
	}

	var prs []ghPullRequest
	if err := json.Unmarshal(stdout, &prs); err != nil {
		return "", false, fmt.Errorf("invalid response while finding open pull request for branch %q: %w", branch, err)
	}
	if len(prs) == 0 {
		return "", false, nil
	}
	return prs[0].HTMLURL, true, nil
}

func (g *GitHub) createPullRequest(ctx context.Context, request ports.PublicationRequest) (string, error) {
	endpoint := fmt.Sprintf("repos/%s/pulls", request.Repository)
	body := map[string]any{
		"title": request.PRTitle,
		"head":  request.Branch,
		"base":  request.DefaultBranch,
		"body":  request.PRBodyMarker,
		"draft": request.Draft,
	}

	stdout, err := g.apiMutate(ctx, "POST", endpoint, body)
	if err != nil {
		return "", err
	}

	var pr ghPullRequest
	if err := json.Unmarshal(stdout, &pr); err != nil {
		return "", fmt.Errorf("invalid response while creating pull request: %w", err)
	}
	if pr.HTMLURL == "" {
		return "", fmt.Errorf("pull request creation response does not include html_url")
	}
	return pr.HTMLURL, nil
}
