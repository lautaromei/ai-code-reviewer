package application

import (
	"github.com/lautaromei/ai-code-reviewer/internal/domain"
	"testing"
)

func TestPromotionRequiresThreeDistinctHumanComments(t *testing.T) {
	threshold := PromotionThresholds{MinRepeatedComments: 3, MinThumbsUp: 1}
	cases := []struct {
		name string
		e    []domain.CandidateEvidence
		want bool
	}{{"two comments", []domain.CandidateEvidence{{CommentID: 1, Kind: domain.EvidenceHuman}, {CommentID: 2, Kind: domain.EvidenceHuman}}, false}, {"same comment repeated", []domain.CandidateEvidence{{CommentID: 1, Kind: domain.EvidenceHuman}, {CommentID: 1, Kind: domain.EvidenceHuman}, {CommentID: 1, Kind: domain.EvidenceHuman}}, false}, {"three comments", []domain.CandidateEvidence{{CommentID: 1, Kind: domain.EvidenceHuman}, {CommentID: 2, Kind: domain.EvidenceHuman}, {CommentID: 3, Kind: domain.EvidenceHuman}}, true}, {"upvote ignored", []domain.CandidateEvidence{{CommentID: 1, Kind: domain.EvidenceAICandidate, ThumbsUp: 99}}, false}}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := EvaluateCandidatePromotion(tc.e, threshold); got != tc.want {
				t.Fatalf("got %v want %v", got, tc.want)
			}
		})
	}
}
