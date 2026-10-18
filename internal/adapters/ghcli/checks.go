package ghcli

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/lautaromei/ai-code-reviewer/internal/application/ports"
	"time"
)

func (g *GitHub) StartCheckRun(ctx context.Context, repo, sha, name string) (ports.CheckRun, error) {
	b, err := g.apiMutate(ctx, "POST", fmt.Sprintf("repos/%s/check-runs", repo), map[string]any{"name": name, "head_sha": sha, "status": "in_progress", "started_at": nowRFC3339()})
	if err != nil {
		return ports.CheckRun{}, err
	}
	var response struct {
		ID int64 `json:"id"`
	}
	if err = json.Unmarshal(b, &response); err != nil {
		return ports.CheckRun{}, err
	}
	return ports.CheckRun{ID: response.ID}, nil
}

func (g *GitHub) CompleteCheckRun(ctx context.Context, repo string, id int64, conclusion, title, summary string) error {
	_, err := g.apiMutate(ctx, "PATCH", fmt.Sprintf("repos/%s/check-runs/%d", repo, id), map[string]any{"status": "completed", "conclusion": conclusion, "completed_at": nowRFC3339(), "output": map[string]string{"title": title, "summary": summary}})
	return err
}

func nowRFC3339() string { return time.Now().UTC().Format(time.RFC3339) }
