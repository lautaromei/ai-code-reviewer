package application

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/lautaromei/ai-code-reviewer/internal/domain"
)

type jsonlComment struct {
	Repository  string   `json:"repository"`
	PullRequest int64    `json:"pull_request"`
	CommentID   int64    `json:"comment_id"`
	Author      string   `json:"author"`
	AuthorKind  string   `json:"author_kind"`
	CreatedAt   string   `json:"created_at"`
	Path        string   `json:"path"`
	Language    string   `json:"language"`
	Body        string   `json:"body"`
	DiffHunk    string   `json:"diff_hunk"`
	RuleIDs     []string `json:"rule_ids"`
	ThumbsUp    int      `json:"thumbs_up"`
	ThumbsDown  int      `json:"thumbs_down"`
}

// ExportRepositoryRepository is the repository dependency consumed by ExportCommentsJSONL.
type ExportRepositoryRepository interface {
	List(context.Context) ([]domain.Repository, error)
}

// ExportCommentRepository is the comment dependency consumed by ExportCommentsJSONL.
type ExportCommentRepository interface {
	ListAllOrdered(context.Context) ([]domain.Comment, error)
}

type temporaryCommentFile interface {
	io.Writer
	Name() string
	Sync() error
	Close() error
}

type commentDestination interface {
	CreateDirectory(string) error
	CreateTemporary(string) (temporaryCommentFile, error)
	Replace(string, string) error
	Remove(string) error
}

type localCommentDestination struct{}

func (localCommentDestination) CreateDirectory(path string) error { return os.MkdirAll(path, 0o755) }
func (localCommentDestination) CreateTemporary(dir string) (temporaryCommentFile, error) {
	return os.CreateTemp(dir, ".comments-*.jsonl.tmp")
}
func (localCommentDestination) Replace(source, target string) error { return os.Rename(source, target) }
func (localCommentDestination) Remove(path string) error            { return os.Remove(path) }

func ExportCommentsJSONL(ctx context.Context, repositories ExportRepositoryRepository, commentsRepository ExportCommentRepository, path string) error {
	return exportCommentsJSONL(ctx, repositories, commentsRepository, path, localCommentDestination{})
}

func exportCommentsJSONL(ctx context.Context, repositories ExportRepositoryRepository, commentsRepository ExportCommentRepository, path string, destination commentDestination) error {
	if ctx == nil {
		return fmt.Errorf("application: export: ctx is nil")
	}
	if path == "" {
		return fmt.Errorf("application: export: path is empty")
	}

	repositoryList, err := repositories.List(ctx)
	if err != nil {
		return fmt.Errorf("application: export: list repositories: %w", err)
	}
	fullNameByID := make(map[int64]string, len(repositoryList))
	for _, repo := range repositoryList {
		fullNameByID[repo.ID] = repo.FullName
	}

	comments, err := commentsRepository.ListAllOrdered(ctx)
	if err != nil {
		return fmt.Errorf("application: export: list comments: %w", err)
	}

	dir := filepath.Dir(path)
	if err := destination.CreateDirectory(dir); err != nil {
		return fmt.Errorf("application: export: create directory %q: %w", dir, err)
	}

	tmp, err := destination.CreateTemporary(dir)
	if err != nil {
		return fmt.Errorf("application: export: create temporary file in %q: %w", dir, err)
	}
	tmpPath := tmp.Name()
	defer func() {
		_ = destination.Remove(tmpPath)
	}()

	encoder := json.NewEncoder(tmp)
	for _, comment := range comments {
		ruleIDs := comment.RuleIDs
		if ruleIDs == nil {
			ruleIDs = []string{}
		}
		line := jsonlComment{
			Repository:  fullNameByID[comment.RepositoryID],
			PullRequest: comment.PullRequestNumber,
			CommentID:   comment.GitHubCommentID,
			Author:      comment.Author,
			AuthorKind:  string(comment.AuthorKind),
			CreatedAt:   comment.CreatedAt.UTC().Format("2006-01-02T15:04:05Z07:00"),
			Path:        comment.Path,
			Language:    comment.Language,
			Body:        comment.Body,
			DiffHunk:    comment.DiffHunk,
			RuleIDs:     ruleIDs,
			ThumbsUp:    0,
			ThumbsDown:  0,
		}
		if err := encoder.Encode(line); err != nil {
			tmp.Close()
			return fmt.Errorf("application: export: serialize comment %d: %w", comment.GitHubCommentID, err)
		}
	}

	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("application: export: sync temporary file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("application: export: close temporary file: %w", err)
	}

	if err := destination.Replace(tmpPath, path); err != nil {
		return fmt.Errorf("application: export: renombrar %q a %q: %w", tmpPath, path, err)
	}

	return nil
}
