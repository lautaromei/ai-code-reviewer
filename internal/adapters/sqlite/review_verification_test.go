package sqlite

import (
	"context"
	"testing"

	"github.com/lautaromei/ai-code-reviewer/internal/domain"
)

func TestSaveVerificationDecisionsPersistsEveryVerdict(t *testing.T) {
	db, _ := openTestStore(t)
	store := NewReviewStore(db)
	ctx := context.Background()
	if err := store.SeedRepositorySettings(ctx, []domain.RepositorySettings{{FullName: "acme/api", ReviewEnabled: true, Categories: []string{"GO"}}}); err != nil {
		t.Fatal(err)
	}
	job, created, err := store.CreateJob(ctx, "acme/api", 7, "head")
	if err != nil || !created {
		t.Fatalf("create job: created=%v err=%v", created, err)
	}
	attempt, err := store.StartAttempt(ctx, job.ID, "anthropic", "sonnet", "review-v1")
	if err != nil {
		t.Fatal(err)
	}
	decisions := []domain.VerificationDecision{
		{CandidateID: "candidate-1", Verdict: "accept", Reason: "confirmed", Kind: "general", Path: "main.go", Line: 3, Confidence: 90, Body: "real bug"},
		{CandidateID: "candidate-2", Verdict: "uncertain", Reason: "missing runtime context", Kind: "general", Confidence: 60, Body: "possible bug"},
	}
	if err = store.SaveVerificationDecisions(ctx, attempt, decisions); err != nil {
		t.Fatal(err)
	}
	if err = store.SaveVerificationUsage(ctx, attempt, "jev", 1_000_000, 12, 0, 0, 42_000); err != nil {
		t.Fatal(err)
	}
	findingID, err := store.SaveFinding(ctx, attempt, domain.ReviewFinding{Kind: domain.FindingGeneral, Path: "main.go", Line: 3, Confidence: 90, Body: "real bug", Fingerprint: "finding-1"})
	if err != nil {
		t.Fatal(err)
	}
	if err = store.Published(ctx, findingID, 12345); err != nil {
		t.Fatal(err)
	}
	rows, err := db.conn().QueryContext(ctx, `SELECT candidate_id,verdict,reason FROM review_verification_decisions WHERE attempt_id=? ORDER BY candidate_id`, attempt)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var got [][3]string
	for rows.Next() {
		var row [3]string
		if err = rows.Scan(&row[0], &row[1], &row[2]); err != nil {
			t.Fatal(err)
		}
		got = append(got, row)
	}
	if err = rows.Err(); err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0] != [3]string{"candidate-1", "accept", "confirmed"} || got[1] != [3]string{"candidate-2", "uncertain", "missing runtime context"} {
		t.Fatalf("unexpected persisted decisions: %#v", got)
	}
	detail, found, err := store.ReviewDetail(ctx, attempt)
	if err != nil || !found {
		t.Fatalf("review detail: found=%v err=%v", found, err)
	}
	if detail.Review.VerificationModel != "jev" || detail.Review.VerificationCostMicroUSD != 42_000 {
		t.Fatalf("unexpected verification detail: %#v", detail)
	}
	if len(detail.Decisions) != 2 || len(detail.Findings) != 1 || detail.Findings[0].GitHubCommentID != 12345 {
		t.Fatalf("missing review records: %#v", detail)
	}
}
