package claudecli

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/lautaromei/ai-code-reviewer/internal/application/ports"
	"github.com/lautaromei/ai-code-reviewer/prompts"
)

func (c *Client) Classify(ctx context.Context, in ports.ClassificationInput) (ports.ClassificationResult, error) {
	sys, e := prompts.Classify(in.PromptVersion)
	if e != nil {
		return ports.ClassificationResult{}, e
	}
	b, _ := json.Marshal(in)
	raw, _, _, e := c.run(ctx, sys+"\nReturn only JSON matching {\"verdicts\":[...]}.\nINPUT DATA:\n"+string(b))
	if e != nil {
		return ports.ClassificationResult{}, e
	}
	var x struct {
		Verdicts []ports.CommentVerdict `json:"verdicts"`
	}
	if e = json.Unmarshal(raw, &x); e != nil {
		return ports.ClassificationResult{}, fmt.Errorf("claude_cli classify: %w", e)
	}
	if len(x.Verdicts) != len(in.Comments) {
		return ports.ClassificationResult{}, fmt.Errorf("claude_cli classify: expected %d verdicts, got %d", len(in.Comments), len(x.Verdicts))
	}
	return ports.ClassificationResult{Model: c.Model, Verdicts: x.Verdicts}, nil
}
func (c *Client) Compact(ctx context.Context, in ports.CompactionInput) (ports.CompactionResult, error) {
	sys, e := prompts.Compact("v2")
	if e != nil {
		return ports.CompactionResult{}, e
	}
	b, _ := json.Marshal(in)
	raw, _, _, e := c.run(ctx, sys+"\nReturn only JSON matching {\"merges\":[...]}.\nINPUT DATA:\n"+string(b))
	if e != nil {
		return ports.CompactionResult{}, e
	}
	var x ports.CompactionResult
	e = json.Unmarshal(raw, &x)
	return x, e
}
func (c *Client) DraftRule(ctx context.Context, in ports.DraftRuleInput) (ports.RuleDraft, error) {
	sys, e := prompts.DraftRule("v2")
	if e != nil {
		return ports.RuleDraft{}, e
	}
	b, _ := json.Marshal(in)
	raw, _, _, e := c.run(ctx, sys+"\nReturn only JSON with title, scope, rule, exception, bad and good. Use empty strings when an optional field has no evidence.\nINPUT DATA:\n"+string(b))
	if e != nil {
		return ports.RuleDraft{}, e
	}
	var x ports.RuleDraft
	e = json.Unmarshal(raw, &x)
	return x, e
}
