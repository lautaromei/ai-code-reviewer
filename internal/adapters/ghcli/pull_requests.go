package ghcli

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"github.com/lautaromei/ai-code-reviewer/internal/application/ports"
	"net/url"
)

func (g *GitHub) GetPullRequest(ctx context.Context, repo string, n int64) (ports.PullRequest, error) {
	b, e := g.apiGet(ctx, fmt.Sprintf("repos/%s/pulls/%d", repo, n))
	if e != nil {
		return ports.PullRequest{}, e
	}
	var x struct {
		Number             int64 `json:"number"`
		State, Title, Body string
		Draft              bool
		Base, Head         struct {
			SHA string `json:"sha"`
		}
	}
	if e = json.Unmarshal(b, &x); e != nil {
		return ports.PullRequest{}, e
	}
	return ports.PullRequest{Number: x.Number, State: x.State, Title: x.Title, Body: x.Body, Draft: x.Draft, BaseSHA: x.Base.SHA, HeadSHA: x.Head.SHA}, nil
}
func (g *GitHub) ListChangedFiles(ctx context.Context, repo string, n int64) ([]ports.ChangedFile, error) {
	b, e := g.apiGetPaginated(ctx, fmt.Sprintf("repos/%s/pulls/%d/files?per_page=100", repo, n))
	if e != nil {
		return nil, e
	}
	raw, e := decodeJSONArrayStream(b)
	if e != nil {
		return nil, e
	}
	out := make([]ports.ChangedFile, 0, len(raw))
	for _, r := range raw {
		var x struct {
			Filename, Status, Patch string
			Additions, Deletions    int
		}
		if e = json.Unmarshal(r, &x); e != nil {
			return nil, e
		}
		out = append(out, ports.ChangedFile{Path: x.Filename, Status: x.Status, Patch: x.Patch, Additions: x.Additions, Deletions: x.Deletions, Truncated: x.Patch == "" && x.Status != "removed"})
	}
	return out, nil
}
func (g *GitHub) ReadFileAtSHA(ctx context.Context, repo, path, sha string) ([]byte, error) {
	b, e := g.apiGet(ctx, fmt.Sprintf("repos/%s/contents/%s?ref=%s", repo, url.PathEscape(path), url.QueryEscape(sha)))
	if e != nil {
		return nil, e
	}
	var x struct{ Content, Encoding string }
	if e = json.Unmarshal(b, &x); e != nil {
		return nil, e
	}
	if x.Encoding == "base64" {
		return base64.StdEncoding.DecodeString(x.Content)
	}
	return []byte(x.Content), nil
}
func (g *GitHub) ListPullRequestComments(ctx context.Context, repo string, n int64) ([]ports.ExistingReviewComment, error) {
	b, e := g.apiGetPaginated(ctx, fmt.Sprintf("repos/%s/pulls/%d/comments?per_page=100", repo, n))
	if e != nil {
		return nil, e
	}
	raw, e := decodeJSONArrayStream(b)
	if e != nil {
		return nil, e
	}
	var out []ports.ExistingReviewComment
	for _, r := range raw {
		var x struct {
			ID         int64
			Body, Path string
			Line       int
		}
		if e = json.Unmarshal(r, &x); e != nil {
			return nil, e
		}
		out = append(out, ports.ExistingReviewComment{ID: x.ID, Body: x.Body, Path: x.Path, Line: x.Line})
	}
	return out, nil
}
func (g *GitHub) PublishReviewComment(ctx context.Context, repo string, n int64, sha string, r ports.ReviewPublishRequest) (ports.PublishedComment, error) {
	b, e := g.apiMutate(ctx, "POST", fmt.Sprintf("repos/%s/pulls/%d/comments", repo, n), map[string]any{"body": r.Body, "commit_id": sha, "path": r.Path, "line": r.Line, "side": r.Side})
	if e != nil {
		return ports.PublishedComment{}, e
	}
	var x struct {
		ID      int64
		HTMLURL string `json:"html_url"`
	}
	e = json.Unmarshal(b, &x)
	return ports.PublishedComment{ID: x.ID, URL: x.HTMLURL}, e
}
func (g *GitHub) PublishIssueComment(ctx context.Context, repo string, n int64, body string) (ports.PublishedComment, error) {
	b, e := g.apiMutate(ctx, "POST", fmt.Sprintf("repos/%s/issues/%d/comments", repo, n), map[string]string{"body": body})
	if e != nil {
		return ports.PublishedComment{}, e
	}
	var x struct {
		ID      int64
		HTMLURL string `json:"html_url"`
	}
	e = json.Unmarshal(b, &x)
	return ports.PublishedComment{ID: x.ID, URL: x.HTMLURL}, e
}
