package fake

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/lautaromei/ai-code-reviewer/internal/application/ports"
	"github.com/lautaromei/ai-code-reviewer/internal/domain"
	"github.com/lautaromei/testigo/memdb"
)

type GitHub struct{}

func NewGitHub() *GitHub {
	return &GitHub{}
}

func (g *GitHub) ListReviewComments(ctx context.Context, repo string, since ports.Cursor) ([]ports.ReviewComment, ports.Cursor, error) {
	return nil, since, nil
}

func (g *GitHub) ListReactions(ctx context.Context, repo string, commentID int64, etag string) (ports.ReactionPage, error) {
	return ports.ReactionPage{}, nil
}

func (g *GitHub) RepositoryLanguages(ctx context.Context, repo string) ([]ports.Language, error) {
	return nil, nil
}

func (g *GitHub) DefaultBranch(ctx context.Context, repo string) (string, error) {
	return "main", nil
}

func (g *GitHub) ReadFile(ctx context.Context, repo string, path string, branch string) ([]byte, error) {
	return nil, nil
}

func (g *GitHub) UpsertRulesPullRequest(ctx context.Context, request ports.PublicationRequest) (ports.PublicationResult, error) {
	return ports.PublicationResult{Status: domain.PublicationAlreadyCurrent}, nil
}

type Classifier struct{}

func NewClassifier() *Classifier {
	return &Classifier{}
}

func (c *Classifier) Classify(ctx context.Context, input ports.ClassificationInput) (ports.ClassificationResult, error) {
	return ports.ClassificationResult{}, nil
}

func (c *Classifier) Compact(ctx context.Context, input ports.CompactionInput) (ports.CompactionResult, error) {
	return ports.CompactionResult{}, nil
}

func (c *Classifier) DraftRule(ctx context.Context, input ports.DraftRuleInput) (ports.RuleDraft, error) {
	return ports.RuleDraft{}, nil
}

func NewRepositoryDirectory() *repositoryRepository {
	return &repositoryRepository{byFullName: memdb.New(func(repository domain.Repository) string { return repository.FullName })}
}

func NewCommentCursor() *cursorRepository {
	return &cursorRepository{byKey: memdb.New(func(cursor domain.Cursor) cursorKey {
		return cursorKey{cursor.RepositoryID, cursor.Stream}
	})}
}

func NewRuleCatalog() *ruleRepository {
	return &ruleRepository{
		byID:       memdb.New(func(rule domain.Rule) string { return rule.RuleID }),
		sequences:  make(map[string]int),
		usages:     make(map[string][]domain.RuleUsage),
		statusHist: make(map[string][]domain.RuleStatusEvent),
	}
}

func NewCandidateGroups() *candidateRepository {
	return &candidateRepository{
		byID: memdb.New(func(candidate domain.CandidateCluster) string { return candidate.ID }),
		evidence: memdb.New(func(evidence domain.CandidateEvidence) evidenceKey {
			return evidenceKey{evidence.CandidateID, evidence.CommentID}
		}),
	}
}

func NewClassificationHistory() *classificationRepository {
	return &classificationRepository{byKey: memdb.New(func(classification domain.Classification) classificationKey {
		return classificationKey{classification.CommentID, classification.InputHash, classification.PromptVersion}
	})}
}

func NewCommentArchive(classifications classificationReader, rules *ruleRepository) *commentRepository {
	return &commentRepository{
		byKey: memdb.New(func(comment domain.Comment) commentKey {
			return commentKey{comment.RepositoryID, comment.GitHubCommentID}
		}),
		classifications: classifications,
		rules:           rules,
	}
}

func NewRuleFeedback(rules *ruleRepository) *reactionRepository {
	return &reactionRepository{byComment: memdb.New(func(bucket reactionBucket) int64 { return bucket.commentID }), rules: rules}
}

func NewRunJournal() *runRepository {
	return &runRepository{byID: memdb.New(func(run domain.Run) int64 { return run.ID })}
}
func NewSnapshotArchive() *snapshotRepository { return &snapshotRepository{} }
func NewPublicationJournal() *publicationRepository {
	return &publicationRepository{byRepositoryID: memdb.New(func(publication domain.Publication) int64 { return publication.RepositoryID })}
}

type repositoryRepository struct {
	mu         sync.Mutex
	nextID     int64
	byFullName memdb.DB[domain.Repository]
}

func (r *repositoryRepository) Upsert(ctx context.Context, repo domain.Repository) (domain.Repository, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if existing, ok := r.byFullName.Get(repo.FullName); ok {
		repo.ID = existing.ID
	} else {
		r.nextID++
		repo.ID = r.nextID
	}
	r.byFullName.Insert(repo)
	return repo, nil
}

func (r *repositoryRepository) GetByFullName(ctx context.Context, fullName string) (domain.Repository, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	repo, ok := r.byFullName.Get(fullName)
	if !ok {
		return domain.Repository{}, false, nil
	}
	return repo, true, nil
}

func (r *repositoryRepository) List(ctx context.Context) ([]domain.Repository, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.byFullName.All(), nil
}

type cursorKey struct {
	repositoryID int64
	stream       string
}

type cursorRepository struct {
	mu    sync.Mutex
	byKey memdb.DB[domain.Cursor]
}

func (c *cursorRepository) Get(ctx context.Context, repositoryID int64, stream string) (domain.Cursor, bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	cur, ok := c.byKey.Get(cursorKey{repositoryID, stream})
	if !ok {
		return domain.Cursor{}, false, nil
	}
	return cur, true, nil
}

func (c *cursorRepository) Advance(ctx context.Context, cursor domain.Cursor) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.byKey.Insert(cursor)
	return nil
}

type commentKey struct {
	repositoryID    int64
	githubCommentID int64
}

type commentRepository struct {
	mu              sync.Mutex
	nextID          int64
	byKey           memdb.DB[domain.Comment]
	classifications classificationReader
	rules           *ruleRepository
}

type classificationReader interface {
	Get(context.Context, int64, string, string) (domain.Classification, bool, error)
}

func (c *commentRepository) Upsert(ctx context.Context, comment domain.Comment) (domain.Comment, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	key := commentKey{comment.RepositoryID, comment.GitHubCommentID}
	if existing, ok := c.byKey.Get(key); ok {
		comment.ID = existing.ID
	} else {
		c.nextID++
		comment.ID = c.nextID
	}
	c.byKey.Insert(comment)
	return comment, nil
}

func (c *commentRepository) GetByID(ctx context.Context, id int64) (domain.Comment, bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	comment, ok := c.byKey.First(func(comment domain.Comment) bool { return comment.ID == id })
	return comment, ok, nil
}

func (c *commentRepository) GetByGitHubID(ctx context.Context, repositoryID int64, githubCommentID int64) (domain.Comment, bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	comment, ok := c.byKey.Get(commentKey{repositoryID, githubCommentID})
	if !ok {
		return domain.Comment{}, false, nil
	}
	return comment, true, nil
}

func (c *commentRepository) ListPendingClassification(ctx context.Context, promptVersion string) ([]domain.Comment, error) {
	c.mu.Lock()
	comments := c.byKey.All()
	c.mu.Unlock()

	var out []domain.Comment
	for _, comment := range comments {
		if _, ok, err := c.classifications.Get(ctx, comment.ID, comment.InputHash, promptVersion); err != nil {
			return nil, err
		} else if ok {
			continue
		}
		out = append(out, comment)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func (c *commentRepository) ListTrackedAI(ctx context.Context) ([]domain.Comment, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	var out []domain.Comment
	for _, comment := range c.byKey.All() {
		if comment.Origin == domain.OriginAIRuleUsage || comment.Origin == domain.OriginAICandidate {
			out = append(out, comment)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func (c *commentRepository) ListForRule(ctx context.Context, ruleID string) ([]domain.Comment, error) {
	usages := c.rules.usagesFor(ruleID)

	c.mu.Lock()
	defer c.mu.Unlock()
	var out []domain.Comment
	for _, usage := range usages {
		for _, comment := range c.byKey.All() {
			if comment.ID == usage.CommentID {
				out = append(out, comment)
				break
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func (c *commentRepository) ListAllOrdered(ctx context.Context) ([]domain.Comment, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := c.byKey.All()
	sort.Slice(out, func(i, j int) bool {
		if out[i].RepositoryID != out[j].RepositoryID {
			return out[i].RepositoryID < out[j].RepositoryID
		}
		if !out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].CreatedAt.Before(out[j].CreatedAt)
		}
		return out[i].GitHubCommentID < out[j].GitHubCommentID
	})
	return out, nil
}

type reactionRepository struct {
	mu        sync.Mutex
	byComment memdb.DB[reactionBucket]
	rules     *ruleRepository
}

type reactionBucket struct {
	commentID int64
	reactions []domain.Reaction
}

func (r *reactionRepository) ReplaceForComment(ctx context.Context, commentID int64, reactions []domain.Reaction) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.byComment.Insert(reactionBucket{commentID: commentID, reactions: reactions})
	return nil
}

func (r *reactionRepository) CountByRule(ctx context.Context, ruleID string, content string) (int, error) {
	usages := r.rules.usagesFor(ruleID)

	r.mu.Lock()
	defer r.mu.Unlock()
	count := 0
	for _, usage := range usages {
		bucket, _ := r.byComment.Get(usage.CommentID)
		for _, reaction := range bucket.reactions {
			if reaction.Content == content {
				count++
			}
		}
	}
	return count, nil
}

func (r *reactionRepository) CountByComment(ctx context.Context, commentID int64, content string) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	count := 0
	bucket, _ := r.byComment.Get(commentID)
	for _, reaction := range bucket.reactions {
		if reaction.Content == content {
			count++
		}
	}
	return count, nil
}

type ruleRepository struct {
	mu         sync.Mutex
	byID       memdb.DB[domain.Rule]
	sequences  map[string]int
	usages     map[string][]domain.RuleUsage
	statusHist map[string][]domain.RuleStatusEvent
}

func (r *ruleRepository) Insert(ctx context.Context, rule domain.Rule) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.byID.Insert(rule)
	return nil
}

func (r *ruleRepository) Get(ctx context.Context, ruleID string) (domain.Rule, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	rule, ok := r.byID.Get(ruleID)
	if !ok {
		return domain.Rule{}, false, nil
	}
	return rule, true, nil
}

func (r *ruleRepository) ListByStatus(ctx context.Context, status domain.RuleStatus) ([]domain.Rule, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.byID.Where(func(rule domain.Rule) bool { return rule.Status == status }), nil
}

func (r *ruleRepository) ListActiveByLanguages(ctx context.Context, languages []string) ([]domain.Rule, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	wanted := make(map[string]bool, len(languages))
	for _, lang := range languages {
		wanted[lang] = true
	}
	return r.byID.Where(func(rule domain.Rule) bool {
		return rule.Status == domain.RuleActive && wanted[rule.Language]
	}), nil
}

func (r *ruleRepository) UpdateStatus(ctx context.Context, event domain.RuleStatusEvent) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if rule, ok := r.byID.Get(event.RuleID); ok {
		rule.Status = event.ToState
		rule.DisabledReason = event.Reason
		if event.ToState == domain.RuleActive {
			rule.DisabledReason = ""
			rule.DisabledAt = nil
		} else {
			occurredAt := event.OccurredAt
			rule.DisabledAt = &occurredAt
		}
		r.byID.Set(event.RuleID, rule)
	}
	r.statusHist[event.RuleID] = append(r.statusHist[event.RuleID], event)
	return nil
}

func (r *ruleRepository) RecordUsage(ctx context.Context, usage domain.RuleUsage) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	usages := r.usages[usage.RuleID]
	for i, existing := range usages {
		if existing.CommentID != usage.CommentID {
			continue
		}
		usages[i] = usage
		r.usages[usage.RuleID] = usages
		r.updateLastUsedAt(usage.RuleID, usage.UsedAt)
		return nil
	}
	r.usages[usage.RuleID] = append(usages, usage)
	r.updateLastUsedAt(usage.RuleID, usage.UsedAt)
	return nil
}

func (r *ruleRepository) updateLastUsedAt(ruleID string, usedAt time.Time) {
	rule, ok := r.byID.Get(ruleID)
	if !ok || (rule.LastUsedAt != nil && !usedAt.After(*rule.LastUsedAt)) {
		return
	}
	lastUsedAt := usedAt
	rule.LastUsedAt = &lastUsedAt
	r.byID.Set(ruleID, rule)
}

func (r *ruleRepository) usagesFor(ruleID string) []domain.RuleUsage {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]domain.RuleUsage(nil), r.usages[ruleID]...)
}

func (r *ruleRepository) NextSequence(ctx context.Context, languageCode string) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	next := r.sequences[languageCode] + 1
	r.sequences[languageCode] = next
	return next, nil
}

type candidateRepository struct {
	mu       sync.Mutex
	byID     memdb.DB[domain.CandidateCluster]
	evidence memdb.DB[domain.CandidateEvidence]
}

type evidenceKey struct {
	candidateID string
	commentID   int64
}

func (c *candidateRepository) Upsert(ctx context.Context, candidate domain.CandidateCluster) (domain.CandidateCluster, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.byID.Insert(candidate)
	return candidate, nil
}

func (c *candidateRepository) Get(ctx context.Context, id string) (domain.CandidateCluster, bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	cand, ok := c.byID.Get(id)
	if !ok {
		return domain.CandidateCluster{}, false, nil
	}
	return cand, true, nil
}

func (c *candidateRepository) ListMergedInto(ctx context.Context, survivingID string) ([]domain.CandidateCluster, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.byID.Where(func(candidate domain.CandidateCluster) bool {
		return candidate.Status == domain.CandidateMerged && candidate.MergedInto == survivingID
	}), nil
}

func (c *candidateRepository) ListEvidence(ctx context.Context, candidateID string) ([]domain.CandidateEvidence, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.evidence.Where(func(evidence domain.CandidateEvidence) bool {
		return evidence.CandidateID == candidateID
	}), nil
}

func (c *candidateRepository) ListOpen(ctx context.Context, language string) ([]domain.CandidateCluster, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.byID.Where(func(candidate domain.CandidateCluster) bool {
		return candidate.Status == domain.CandidateOpen && candidate.Language == language
	}), nil
}

func (c *candidateRepository) AttachEvidence(ctx context.Context, evidence domain.CandidateEvidence) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.evidence.Insert(evidence)
	return nil
}

func (c *candidateRepository) Merge(ctx context.Context, survivingID string, mergedIDs []string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, id := range mergedIDs {
		if cand, ok := c.byID.Get(id); ok {
			cand.Status = domain.CandidateMerged
			cand.MergedInto = survivingID
			c.byID.Set(id, cand)
		}
	}
	return nil
}

func (c *candidateRepository) Promote(ctx context.Context, candidateID string, ruleID string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if cand, ok := c.byID.Get(candidateID); ok {
		cand.Status = domain.CandidatePromoted
		cand.PromotedRuleID = ruleID
		c.byID.Set(candidateID, cand)
	}
	return nil
}

type classificationKey struct {
	commentID     int64
	inputHash     string
	promptVersion string
}

type classificationRepository struct {
	mu    sync.Mutex
	byKey memdb.DB[domain.Classification]
}

func (c *classificationRepository) Get(ctx context.Context, commentID int64, inputHash string, promptVersion string) (domain.Classification, bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	classification, ok := c.byKey.Get(classificationKey{commentID, inputHash, promptVersion})
	if !ok {
		return domain.Classification{}, false, nil
	}
	return classification, true, nil
}

func (c *classificationRepository) Save(ctx context.Context, classification domain.Classification) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.byKey.Insert(classification)
	return nil
}

type snapshotRepository struct {
	mu     sync.Mutex
	latest domain.Snapshot
	has    bool
}

func (s *snapshotRepository) Save(ctx context.Context, snapshot domain.Snapshot) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.latest = snapshot
	s.has = true
	return nil
}

func (s *snapshotRepository) Latest(ctx context.Context) (domain.Snapshot, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.has {
		return domain.Snapshot{}, false, nil
	}
	return s.latest, true, nil
}

type publicationRepository struct {
	mu             sync.Mutex
	nextID         int64
	byRepositoryID memdb.DB[domain.Publication]
}

func (p *publicationRepository) Save(ctx context.Context, publication domain.Publication) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if publication.ID == 0 {
		p.nextID++
		publication.ID = p.nextID
	}
	p.byRepositoryID.Insert(publication)
	return nil
}

func (p *publicationRepository) LastForRepository(ctx context.Context, repositoryID int64) (domain.Publication, bool, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	pub, ok := p.byRepositoryID.Get(repositoryID)
	if !ok {
		return domain.Publication{}, false, nil
	}
	return pub, true, nil
}

type runRepository struct {
	mu     sync.Mutex
	nextID int64
	byID   memdb.DB[domain.Run]
}

func (r *runRepository) Start(ctx context.Context, run domain.Run) (domain.Run, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.nextID++
	run.ID = r.nextID
	r.byID.Insert(run)
	return run, nil
}

func (r *runRepository) Finish(ctx context.Context, run domain.Run) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.byID.Set(run.ID, run)
	return nil
}
