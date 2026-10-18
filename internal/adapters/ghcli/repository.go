package ghcli

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strings"

	"github.com/lautaromei/ai-code-reviewer/internal/application/ports"
)

func (g *GitHub) RepositoryLanguages(ctx context.Context, repo string) ([]ports.Language, error) {
	if repo == "" {
		return nil, fmt.Errorf("ghcli: RepositoryLanguages: repository cannot be empty")
	}

	endpoint := fmt.Sprintf("repos/%s/languages", repo)
	stdout, err := g.apiGet(ctx, endpoint)
	if err != nil {
		return nil, fmt.Errorf("ghcli: RepositoryLanguages(%s): %w", repo, err)
	}

	var byLanguage map[string]int64
	if err := json.Unmarshal(stdout, &byLanguage); err != nil {
		return nil, fmt.Errorf("ghcli: RepositoryLanguages(%s): invalid response: %w", repo, err)
	}

	languages := make([]ports.Language, 0, len(byLanguage))
	for name, size := range byLanguage {
		languages = append(languages, ports.Language{Name: name, Bytes: size})
	}
	sort.Slice(languages, func(i, j int) bool { return languages[i].Name < languages[j].Name })

	return languages, nil
}

type ghRepository struct {
	DefaultBranch string `json:"default_branch"`
}

func (g *GitHub) DefaultBranch(ctx context.Context, repo string) (string, error) {
	if repo == "" {
		return "", fmt.Errorf("ghcli: DefaultBranch: repository cannot be empty")
	}

	endpoint := fmt.Sprintf("repos/%s", repo)
	stdout, err := g.apiGet(ctx, endpoint)
	if err != nil {
		return "", fmt.Errorf("ghcli: DefaultBranch(%s): %w", repo, err)
	}

	var parsed ghRepository
	if err := json.Unmarshal(stdout, &parsed); err != nil {
		return "", fmt.Errorf("ghcli: DefaultBranch(%s): invalid response: %w", repo, err)
	}
	if parsed.DefaultBranch == "" {
		return "", fmt.Errorf("ghcli: DefaultBranch(%s): response does not include default_branch", repo)
	}

	return parsed.DefaultBranch, nil
}

func (g *GitHub) RepositoryInstalled(ctx context.Context, repo string) (bool, error) {
	if repo == "" {
		return false, errors.New("ghcli: RepositoryInstalled: repository cannot be empty")
	}
	_, err := g.apiGet(ctx, fmt.Sprintf("repos/%s", repo))
	if errors.Is(err, ErrNotFound) || errors.Is(err, ErrPermission) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("ghcli: RepositoryInstalled(%s): %w", repo, err)
	}
	return true, nil
}

type ghContentsReadResponse struct {
	Content  string `json:"content"`
	Encoding string `json:"encoding"`
}

func (g *GitHub) ReadFile(ctx context.Context, repo string, path string, branch string) ([]byte, error) {
	if repo == "" {
		return nil, errors.New("ghcli: ReadFile: repository cannot be empty")
	}
	if path == "" {
		return nil, errors.New("ghcli: ReadFile: path cannot be empty")
	}
	if branch == "" {
		return nil, errors.New("ghcli: ReadFile: branch cannot be empty")
	}

	endpoint := fmt.Sprintf("repos/%s/contents/%s?ref=%s", repo, path, url.QueryEscape(branch))
	stdout, err := g.apiGet(ctx, endpoint)
	if err != nil {
		return nil, fmt.Errorf("ghcli: ReadFile(%s, %s, %s): %w", repo, path, branch, err)
	}

	var response ghContentsReadResponse
	if err := json.Unmarshal(stdout, &response); err != nil {
		return nil, fmt.Errorf("ghcli: ReadFile(%s): invalid response: %w", path, err)
	}
	if response.Encoding != "base64" {
		return nil, fmt.Errorf("ghcli: ReadFile(%s): unsupported encoding %q", path, response.Encoding)
	}
	content, err := base64.StdEncoding.DecodeString(strings.Join(strings.Fields(response.Content), ""))
	if err != nil {
		return nil, fmt.Errorf("ghcli: ReadFile(%s): invalid base64 content: %w", path, err)
	}
	return content, nil
}
