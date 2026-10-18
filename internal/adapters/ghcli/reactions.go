package ghcli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/lautaromei/ai-code-reviewer/internal/application/ports"
)

type ghReaction struct {
	ID        int64     `json:"id"`
	Content   string    `json:"content"`
	User      ghUser    `json:"user"`
	CreatedAt time.Time `json:"created_at"`
}

func (g *GitHub) ListReactions(ctx context.Context, repo string, commentID int64, etag string) (ports.ReactionPage, error) {
	if repo == "" {
		return ports.ReactionPage{}, fmt.Errorf("ghcli: ListReactions: repository cannot be empty")
	}

	endpoint := fmt.Sprintf("repos/%s/pulls/comments/%d/reactions", repo, commentID)

	headers := map[string]string{}
	if etag != "" {
		headers["If-None-Match"] = etag
	}

	stdout, err := g.apiGetWithHeaders(ctx, endpoint, headers)
	if err != nil {
		if errors.Is(err, ErrNotModified) {
			return ports.ReactionPage{NotModified: true, ETag: etag}, nil
		}
		return ports.ReactionPage{}, fmt.Errorf("ghcli: ListReactions(%s, %d): %w", repo, commentID, err)
	}

	var rawReactions []ghReaction
	if err := json.Unmarshal(stdout, &rawReactions); err != nil {
		return ports.ReactionPage{}, fmt.Errorf("ghcli: ListReactions(%s, %d): invalid response: %w", repo, commentID, err)
	}

	reactions := make([]ports.ReactionRecord, 0, len(rawReactions))
	for _, raw := range rawReactions {
		reactions = append(reactions, ports.ReactionRecord{
			GitHubReactionID: raw.ID,
			User:             raw.User.Login,
			Content:          raw.Content,
			CreatedAt:        raw.CreatedAt,
		})
	}

	return ports.ReactionPage{Reactions: reactions}, nil
}
