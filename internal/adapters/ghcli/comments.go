package ghcli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/lautaromei/ai-code-reviewer/internal/application/ports"
)

type ghUser struct {
	Login string `json:"login"`
	Type  string `json:"type"`
}

type ghReviewComment struct {
	ID                  int64     `json:"id"`
	PullRequestURL      string    `json:"pull_request_url"`
	PullRequestReviewID *int64    `json:"pull_request_review_id"`
	InReplyToID         *int64    `json:"in_reply_to_id"`
	User                ghUser    `json:"user"`
	Body                string    `json:"body"`
	Path                string    `json:"path"`
	DiffHunk            string    `json:"diff_hunk"`
	HTMLURL             string    `json:"html_url"`
	CreatedAt           time.Time `json:"created_at"`
	UpdatedAt           time.Time `json:"updated_at"`
}

func (g *GitHub) ListReviewComments(ctx context.Context, repo string, since ports.Cursor) ([]ports.ReviewComment, ports.Cursor, error) {
	endpoint, err := reviewCommentsEndpoint(repo, since)
	if err != nil {
		return nil, since, err
	}

	stdout, err := g.apiGetPaginated(ctx, endpoint)
	if err != nil {
		return nil, since, fmt.Errorf("ghcli: ListReviewComments(%s): %w", repo, err)
	}

	rawComments, _ := decodeJSONArrayStream(stdout)

	comments := make([]ports.ReviewComment, 0, len(rawComments))
	for i, raw := range rawComments {
		var gc ghReviewComment
		if err := json.Unmarshal(raw, &gc); err != nil {
			return nil, since, fmt.Errorf("ghcli: ListReviewComments(%s): invalid comment #%d: %w", repo, i, err)
		}

		prNumber, err := parsePullRequestNumber(gc.PullRequestURL)
		if err != nil {
			return nil, since, fmt.Errorf("ghcli: ListReviewComments(%s): comment %d: %w", repo, gc.ID, err)
		}

		var reviewID int64
		if gc.PullRequestReviewID != nil {
			reviewID = *gc.PullRequestReviewID
		}

		comments = append(comments, ports.ReviewComment{
			GitHubCommentID:   gc.ID,
			PullRequestNumber: prNumber,
			ReviewID:          reviewID,
			InReplyToID:       gc.InReplyToID,
			Author:            gc.User.Login,
			AuthorIsBot:       gc.User.Type == "Bot",
			Body:              gc.Body,
			Path:              gc.Path,
			DiffHunk:          gc.DiffHunk,
			URL:               gc.HTMLURL,
			CreatedAt:         gc.CreatedAt,
			UpdatedAt:         gc.UpdatedAt,
		})
	}

	newCursor := advanceCursor(since, comments)
	return comments, newCursor, nil
}

func reviewCommentsEndpoint(repo string, since ports.Cursor) (string, error) {
	if repo == "" {
		return "", fmt.Errorf("ghcli: repository cannot be empty")
	}

	query := url.Values{}
	query.Set("sort", "updated")
	query.Set("direction", "asc")
	query.Set("per_page", "100")
	if !since.Since.IsZero() {
		query.Set("since", since.Since.UTC().Format(time.RFC3339))
	}

	return fmt.Sprintf("repos/%s/pulls/comments?%s", repo, query.Encode()), nil
}

func parsePullRequestNumber(pullRequestURL string) (int64, error) {
	idx := strings.LastIndex(pullRequestURL, "/")
	if idx == -1 || idx == len(pullRequestURL)-1 {
		return 0, fmt.Errorf("pull_request_url %q has an unexpected format", pullRequestURL)
	}
	n, err := strconv.ParseInt(pullRequestURL[idx+1:], 10, 64)
	if err != nil {
		return 0, fmt.Errorf("pull_request_url %q: invalid PR number: %w", pullRequestURL, err)
	}
	return n, nil
}

func advanceCursor(current ports.Cursor, comments []ports.ReviewComment) ports.Cursor {
	newest := current
	for _, c := range comments {
		if isNewerCursorPosition(c.UpdatedAt, c.GitHubCommentID, newest) {
			newest = ports.Cursor{Since: c.UpdatedAt, LastCommentID: c.GitHubCommentID}
		}
	}
	return newest
}

func isNewerCursorPosition(updatedAt time.Time, commentID int64, cursor ports.Cursor) bool {
	if updatedAt.After(cursor.Since) {
		return true
	}
	if updatedAt.Equal(cursor.Since) && commentID > cursor.LastCommentID {
		return true
	}
	return false
}

func decodeJSONArrayStream(stdout []byte) ([]json.RawMessage, error) {
	trimmed := bytes.TrimSpace(stdout)
	if len(trimmed) == 0 {
		return nil, nil
	}

	decoder := json.NewDecoder(bytes.NewReader(trimmed))

	var out []json.RawMessage
	for {
		var doc json.RawMessage
		err := decoder.Decode(&doc)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("invalid top-level JSON: %w", err)
		}

		var page []json.RawMessage
		if err := json.Unmarshal(doc, &page); err != nil {
			return nil, fmt.Errorf("expected a JSON array per page, got: %s", string(doc))
		}
		out = append(out, page...)
	}

	return out, nil
}
