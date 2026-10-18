package anthropic

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/lautaromei/ai-code-reviewer/internal/application/ports"
	"github.com/lautaromei/ai-code-reviewer/internal/domain"
	"github.com/lautaromei/ai-code-reviewer/prompts"
)

var validVerdicts = map[domain.ClassificationVerdict]bool{
	domain.VerdictCovered:        true,
	domain.VerdictCandidateMatch: true,
	domain.VerdictNewCandidate:   true,
	domain.VerdictNew:            true,
	domain.VerdictOneOff:         true,
	domain.VerdictNoise:          true,
}

func classifyTool() anthropicTool {
	return anthropicTool{
		Name:        "emit_classification",
		Description: "Emit one verdict for each received comment.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"verdicts": map[string]any{
					"type": "array",
					"items": map[string]any{
						"type": "object",
						"properties": map[string]any{
							"comment_id":    map[string]any{"type": "integer"},
							"verdict":       map[string]any{"type": "string", "enum": []string{"covered", "candidate_match", "new_candidate", "new", "one_off", "noise"}},
							"rule_id":       map[string]any{"type": "string"},
							"candidate_key": map[string]any{"type": "string"},
							"candidate_id":  map[string]any{"type": "string"},
							"reason":        map[string]any{"type": "string"},
						},
						"required": []string{"comment_id", "verdict"},
					},
				},
			},
			"required": []string{"verdicts"},
		},
	}
}

type classifyVerdictPayload struct {
	CommentID    int64  `json:"comment_id"`
	Verdict      string `json:"verdict"`
	RuleID       string `json:"rule_id"`
	CandidateKey string `json:"candidate_key"`
	CandidateID  string `json:"candidate_id"`
	Reason       string `json:"reason"`
}

type classifyToolInput struct {
	Verdicts []classifyVerdictPayload `json:"verdicts"`
}

func (c *Client) Classify(ctx context.Context, input ports.ClassificationInput) (ports.ClassificationResult, error) {
	promptVersion := input.PromptVersion
	if promptVersion == "" {
		promptVersion = c.promptVersion
	}
	systemPrompt, err := prompts.Classify(promptVersion)
	if err != nil {
		return ports.ClassificationResult{}, err
	}

	userPrompt, _ := json.Marshal(map[string]any{
		"catalog":         input.Catalog,
		"open_candidates": input.OpenCandidates,
		"comments":        input.Comments,
	})

	raw, err := c.callTool(ctx, systemPrompt, string(userPrompt), classifyTool())
	if err != nil {
		return ports.ClassificationResult{}, fmt.Errorf("anthropic: classify: %w", err)
	}

	var parsed classifyToolInput
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return ports.ClassificationResult{}, fmt.Errorf("anthropic: classify: invalid tool response: %w", err)
	}

	knownRuleIDs := make(map[string]bool, len(input.Catalog))
	for _, entry := range input.Catalog {
		knownRuleIDs[entry.RuleID] = true
	}
	knownCandidateIDs := make(map[string]bool, len(input.OpenCandidates))
	for _, entry := range input.OpenCandidates {
		knownCandidateIDs[entry.CandidateID] = true
	}
	expectedCommentIDs := make(map[int64]bool, len(input.Comments))
	for _, comment := range input.Comments {
		expectedCommentIDs[comment.CommentID] = true
	}

	seen := make(map[int64]bool, len(parsed.Verdicts))
	verdicts := make([]ports.CommentVerdict, 0, len(parsed.Verdicts))
	for _, v := range parsed.Verdicts {
		if !expectedCommentIDs[v.CommentID] {
			return ports.ClassificationResult{}, fmt.Errorf("anthropic: classify: comment_id %d was not in the input", v.CommentID)
		}
		if seen[v.CommentID] {
			return ports.ClassificationResult{}, fmt.Errorf("anthropic: classify: comment_id %d received more than one verdict", v.CommentID)
		}
		seen[v.CommentID] = true

		verdict := domain.ClassificationVerdict(v.Verdict)
		if !validVerdicts[verdict] {
			return ports.ClassificationResult{}, fmt.Errorf("anthropic: classify: unknown verdict %q for comment_id %d", v.Verdict, v.CommentID)
		}
		if verdict == domain.VerdictCovered {
			if v.RuleID == "" {
				return ports.ClassificationResult{}, fmt.Errorf("anthropic: classify: covered verdict has no rule_id for comment_id %d", v.CommentID)
			}
			if !knownRuleIDs[v.RuleID] {
				return ports.ClassificationResult{}, fmt.Errorf("anthropic: classify: rule_id %q does not exist in the catalog (comment_id %d)", v.RuleID, v.CommentID)
			}
		}
		if verdict == domain.VerdictNew && v.CandidateKey == "" {
			return ports.ClassificationResult{}, fmt.Errorf("anthropic: classify: new verdict has no candidate_key for comment_id %d", v.CommentID)
		}
		if verdict == domain.VerdictCandidateMatch && !knownCandidateIDs[v.CandidateID] {
			return ports.ClassificationResult{}, fmt.Errorf("anthropic: classify: candidate_id %q does not exist", v.CandidateID)
		}

		rawVerdict, _ := json.Marshal(v)

		verdicts = append(verdicts, ports.CommentVerdict{
			CommentID:    v.CommentID,
			Verdict:      verdict,
			RuleID:       v.RuleID,
			CandidateKey: v.CandidateKey,
			CandidateID:  v.CandidateID,
			Reason:       v.Reason,
			RawPayload:   string(rawVerdict),
		})
	}

	for id := range expectedCommentIDs {
		if !seen[id] {
			return ports.ClassificationResult{}, fmt.Errorf("anthropic: classify: comment_id %d received no verdict", id)
		}
	}

	return ports.ClassificationResult{Model: c.model, Verdicts: verdicts}, nil
}

func compactTool() anthropicTool {
	return anthropicTool{
		Name:        "emit_compaction",
		Description: "Emit merges for equivalent candidates.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"merges": map[string]any{
					"type": "array",
					"items": map[string]any{
						"type": "object",
						"properties": map[string]any{
							"surviving_candidate_id": map[string]any{"type": "string"},
							"merged_candidate_ids":   map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
						},
						"required": []string{"surviving_candidate_id", "merged_candidate_ids"},
					},
				},
			},
			"required": []string{"merges"},
		},
	}
}

type compactMergePayload struct {
	SurvivingCandidateID string   `json:"surviving_candidate_id"`
	MergedCandidateIDs   []string `json:"merged_candidate_ids"`
}

type compactToolInput struct {
	Merges []compactMergePayload `json:"merges"`
}

func (c *Client) Compact(ctx context.Context, input ports.CompactionInput) (ports.CompactionResult, error) {
	systemPrompt, err := prompts.Compact(c.promptVersion)
	if err != nil {
		return ports.CompactionResult{}, err
	}

	userPrompt, _ := json.Marshal(map[string]any{
		"language":   input.Language,
		"candidates": input.Candidates,
	})

	raw, err := c.callTool(ctx, systemPrompt, string(userPrompt), compactTool())
	if err != nil {
		return ports.CompactionResult{}, fmt.Errorf("anthropic: compact: %w", err)
	}

	var parsed compactToolInput
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return ports.CompactionResult{}, fmt.Errorf("anthropic: compact: invalid tool response: %w", err)
	}

	known := make(map[string]bool, len(input.Candidates))
	for _, cand := range input.Candidates {
		known[cand.CandidateID] = true
	}

	merges := make([]ports.CompactionMerge, 0, len(parsed.Merges))
	for _, m := range parsed.Merges {
		if !known[m.SurvivingCandidateID] {
			return ports.CompactionResult{}, fmt.Errorf("anthropic: compact: surviving_candidate_id %q is not in the input", m.SurvivingCandidateID)
		}
		for _, mergedID := range m.MergedCandidateIDs {
			if !known[mergedID] {
				return ports.CompactionResult{}, fmt.Errorf("anthropic: compact: merged_candidate_id %q is not in the input", mergedID)
			}
			if mergedID == m.SurvivingCandidateID {
				return ports.CompactionResult{}, fmt.Errorf("anthropic: compact: %q cannot be merged into itself", mergedID)
			}
		}
		merges = append(merges, ports.CompactionMerge{
			SurvivingCandidateID: m.SurvivingCandidateID,
			MergedCandidateIDs:   m.MergedCandidateIDs,
		})
	}

	return ports.CompactionResult{Merges: merges}, nil
}

func draftRuleTool() anthropicTool {
	return anthropicTool{
		Name:        "emit_rule_draft",
		Description: "Emit a rule draft from its evidence.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"title":     map[string]any{"type": "string"},
				"scope":     map[string]any{"type": "string"},
				"rule":      map[string]any{"type": "string"},
				"exception": map[string]any{"type": "string"},
				"bad":       map[string]any{"type": "string"},
				"good":      map[string]any{"type": "string"},
			},
			"required": []string{"title", "scope", "rule", "exception", "bad", "good"},
		},
	}
}

type draftRulePayload struct {
	Title     string `json:"title"`
	Scope     string `json:"scope"`
	Rule      string `json:"rule"`
	Exception string `json:"exception"`
	Bad       string `json:"bad"`
	Good      string `json:"good"`
	Body      string `json:"body"`
}

func (c *Client) DraftRule(ctx context.Context, input ports.DraftRuleInput) (ports.RuleDraft, error) {
	systemPrompt, err := prompts.DraftRule(c.promptVersion)
	if err != nil {
		return ports.RuleDraft{}, err
	}

	userPrompt, _ := json.Marshal(map[string]any{
		"language": input.Language,
		"evidence": input.Evidence,
	})

	raw, err := c.callTool(ctx, systemPrompt, string(userPrompt), draftRuleTool())
	if err != nil {
		return ports.RuleDraft{}, fmt.Errorf("anthropic: draft_rule: %w", err)
	}

	var parsed draftRulePayload
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return ports.RuleDraft{}, fmt.Errorf("anthropic: draft_rule: invalid tool response: %w", err)
	}
	if parsed.Rule == "" {
		parsed.Rule = parsed.Body
	}
	if parsed.Title == "" || parsed.Rule == "" {
		return ports.RuleDraft{}, fmt.Errorf("anthropic: draft_rule: response does not include title/rule")
	}

	return ports.RuleDraft{Title: parsed.Title, Scope: parsed.Scope, Rule: parsed.Rule, Exception: parsed.Exception, Bad: parsed.Bad, Good: parsed.Good}, nil
}
