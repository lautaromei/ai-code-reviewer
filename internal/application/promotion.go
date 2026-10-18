package application

import "github.com/lautaromei/ai-code-reviewer/internal/domain"

type PromotionThresholds struct {
	MinRepeatedComments int
	MinThumbsUp         int
}

func EvaluateCandidatePromotion(evidence []domain.CandidateEvidence, thresholds PromotionThresholds) bool {
	distinctComments := make(map[int64]struct{})

	for _, e := range evidence {
		if e.Kind == domain.EvidenceHuman {
			distinctComments[e.CommentID] = struct{}{}
		}
	}

	return len(distinctComments) >= thresholds.MinRepeatedComments
}
