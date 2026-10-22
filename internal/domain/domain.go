package domain

import "time"

type AuthorKind string

const (
	AuthorHuman AuthorKind = "human"
	AuthorAI    AuthorKind = "ai"
	AuthorBot   AuthorKind = "bot"
)

type CommentOrigin string

const (
	OriginHuman       CommentOrigin = "human"
	OriginAIRuleUsage CommentOrigin = "ai_rule_usage"
	OriginAICandidate CommentOrigin = "ai_candidate"
	OriginBotNoise    CommentOrigin = "bot_noise"
)

type RuleStatus string

const (
	RuleActive   RuleStatus = "active"
	RuleDisabled RuleStatus = "disabled"
	RuleExpired  RuleStatus = "expired"
)

type DisabledReason string

const (
	DisabledDownvoteThreshold DisabledReason = "downvote_threshold"
	DisabledUnused            DisabledReason = "unused"
	DisabledManual            DisabledReason = "manual"
)

type CandidateStatus string

const (
	CandidateOpen     CandidateStatus = "open"
	CandidatePromoted CandidateStatus = "promoted"
	CandidateCovered  CandidateStatus = "covered"
	CandidateMerged   CandidateStatus = "merged"
)

type ClassificationVerdict string

const (
	VerdictCovered        ClassificationVerdict = "covered"
	VerdictCandidateMatch ClassificationVerdict = "candidate_match"
	VerdictNewCandidate   ClassificationVerdict = "new_candidate"
	VerdictNew            ClassificationVerdict = "new" // legacy persisted value
	VerdictOneOff         ClassificationVerdict = "one_off"
	VerdictNoise          ClassificationVerdict = "noise"
)

type RunStatus string

const (
	RunRunning   RunStatus = "running"
	RunSucceeded RunStatus = "succeeded"
	RunPartial   RunStatus = "partial"
	RunFailed    RunStatus = "failed"
)

type PublicationStatus string

const (
	PublicationAlreadyCurrent PublicationStatus = "already_current"
	PublicationUpdated        PublicationStatus = "updated"
	PublicationCreated        PublicationStatus = "created"
	PublicationFailed         PublicationStatus = "failed"
)

type Repository struct {
	ID            int64
	FullName      string
	Analyzed      bool
	Actionable    bool
	RulesPath     string
	Languages     []string
	LanguagesAt   time.Time
	DefaultBranch string
}

type RepositorySettings struct {
	ID                           int64
	FullName                     string
	ReviewEnabled, MiningEnabled bool
	ReviewModel                  string
	Categories                   []string
	Version                      int64
	UpdatedAt                    time.Time
}

type ReviewModelSettings struct {
	GlobalModel       string
	VerificationModel string
	GenerationAgents  int
	GenerationModels  []string
	Version           int64
	Actor             string
	UpdatedAt         time.Time
}

type ReviewJobStatus string

const (
	ReviewQueued     ReviewJobStatus = "queued"
	ReviewRunning    ReviewJobStatus = "running"
	ReviewRetryWait  ReviewJobStatus = "retry_wait"
	ReviewPublishing ReviewJobStatus = "publishing"
	ReviewCompleted  ReviewJobStatus = "completed"
	ReviewSkipped    ReviewJobStatus = "skipped"
	ReviewFailed     ReviewJobStatus = "failed"
)

type FindingKind string

const (
	FindingCatalogRule FindingKind = "catalog_rule"
	FindingGeneral     FindingKind = "general"
)

type ReviewJob struct {
	ID                int64
	RepositoryID      int64
	Repository        string
	PullRequestNumber int64
	InitialHeadSHA    string
	Status            ReviewJobStatus
	Attempts          int
	NextAttemptAt     time.Time
	LeaseOwner        string
	LeaseUntil        time.Time
	LastError         string
	TerminalReason    string
	PromptRevisionID  *int64
	PromptHash        string
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

type ReviewAttempt struct {
	ID, JobID                                          int64
	Provider, Model, PromptVersion, Result, Error      string
	StartedAt                                          time.Time
	FinishedAt                                         *time.Time
	InputTokens, OutputTokens                          int64
	CacheCreationTokens, CacheReadTokens, CostMicroUSD int64
}

type ReviewHistoryEntry struct {
	AttemptID, PullRequestNumber                                    int64
	Repository, Provider, Model, VerificationModel, Result, Error   string
	StartedAt                                                       time.Time
	FinishedAt                                                      *time.Time
	InputTokens, OutputTokens, CacheCreationTokens, CacheReadTokens int64
	CostMicroUSD                                                    int64
	VerificationInputTokens, VerificationOutputTokens               int64
	VerificationCacheCreationTokens, VerificationCacheReadTokens    int64
	VerificationCostMicroUSD                                        int64
	FindingCount                                                    int
}

type ReviewRuleInput struct {
	RuleID, Category, Title, Body, Scopes, ContentHash string
}

type ReviewDetail struct {
	Review    ReviewHistoryEntry
	Findings  []ReviewFinding
	Decisions []VerificationDecision
	Rules     []ReviewRuleInput
}

type DailyReviewMetric struct {
	Date         string
	ReviewCount  int
	CostMicroUSD int64
}

type DashboardMetrics struct {
	ReviewsToday                 int
	CostTodayMicroUSD            int64
	AverageReviewsPerDay         float64
	AverageCostPerReviewMicroUSD int64
	ActiveRules                  int
	TotalRules                   int
	Daily                        []DailyReviewMetric
}

type ReviewFinding struct {
	ID, AttemptID                                              int64
	RuleID                                                     *string
	Kind                                                       FindingKind
	Path, Side                                                 string
	Line                                                       int
	Confidence                                                 int
	Severity, Body, Suggestion, Fingerprint, PublicationStatus string
	GitHubCommentID                                            int64
}

type VerificationDecision struct {
	CandidateID string
	Verdict     string
	Reason      string
	DuplicateOf string
	Kind        string
	RuleID      string
	Path        string
	Line        int
	Confidence  int
	Severity    string
	Body        string
	Suggestion  string
}

type RuleSource struct {
	RuleID                                          string
	RepositoryID                                    int64
	SourcePath, SourceRef, SourceRuleID, SourceHash string
}

type ReviewPromptRevision struct {
	ID                                            int64
	Scope, Repository, Text, Hash, Origin, Author string
	PreviousRevisionID                            *int64
	CreatedAt                                     time.Time
}

type ReviewPrompt struct {
	Revision        ReviewPromptRevision
	BindingVersion  int64
	EffectiveOrigin string
}

type Comment struct {
	ID                int64
	GitHubCommentID   int64
	RepositoryID      int64
	PullRequestNumber int64
	ReviewID          int64
	ParentCommentID   *int64
	Author            string
	AuthorKind        AuthorKind
	Origin            CommentOrigin
	Body              string
	Path              string
	DiffHunk          string
	Language          string
	URL               string
	RuleIDs           []string
	CreatedAt         time.Time
	UpdatedAt         time.Time
	IngestedAt        time.Time
	InputHash         string
}

type Reaction struct {
	ID               int64
	GitHubReactionID int64
	CommentID        int64
	User             string
	Content          string
	CreatedAt        time.Time
	LastSeenAt       time.Time
}

type Rule struct {
	RuleID         string
	Language       string
	Title          string
	Scope          string
	Body           string
	Rule           string
	Exception      string
	BadExample     string
	GoodExample    string
	Status         RuleStatus
	CreatedAt      time.Time
	ActivatedAt    time.Time
	LastUsedAt     *time.Time
	DisabledAt     *time.Time
	DisabledReason DisabledReason
	Source         string
	ContentHash    string
}

type RuleUsage struct {
	RuleID    string
	CommentID int64
	UsedAt    time.Time
}

// RuleReference identifies a published PR comment where a catalog rule was used.
type RuleReference struct {
	Repository        string
	PullRequestNumber int64
	GitHubCommentID   int64
	URL               string
	Path              string
	Line              int
	Body              string
	ThumbsDown        int
	CreatedAt         time.Time
}

type RuleStatusEvent struct {
	EventKey   string
	RuleID     string
	FromState  RuleStatus
	ToState    RuleStatus
	Reason     DisabledReason
	RunID      int64
	OccurredAt time.Time
}

type CandidateCluster struct {
	ID                string
	Language          string
	Label             string
	Status            CandidateStatus
	MergedInto        string
	PromotedRuleID    string
	ClassifierVersion string
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

type EvidenceKind string

const (
	EvidenceHuman       EvidenceKind = "human"
	EvidenceAICandidate EvidenceKind = "ai_candidate"
)

type CandidateEvidence struct {
	CandidateID string
	CommentID   int64
	Kind        EvidenceKind
	ThumbsUp    int
	ThumbsDown  int
	EvaluatedAt time.Time
}

type Classification struct {
	CommentID     int64
	InputHash     string
	PromptVersion string
	Model         string
	Verdict       ClassificationVerdict
	RawPayload    string
	CreatedAt     time.Time
}

type Snapshot struct {
	ID              int64
	Hash            string
	RunID           int64
	RendererVersion string
	CreatedAt       time.Time
	RuleIDs         []string
}

type Publication struct {
	ID                   int64
	SnapshotID           int64
	RepositoryID         int64
	FilteredSnapshotHash string
	Branch               string
	CommitSHA            string
	PullRequestURL       string
	Status               PublicationStatus
	LastError            string
	CreatedAt            time.Time
}

type Run struct {
	ID         int64
	Command    string
	ConfigHash string
	Status     RunStatus
	StartedAt  time.Time
	FinishedAt *time.Time
	Summary    string
	Error      string
}

type Cursor struct {
	RepositoryID  int64
	Stream        string
	HighWatermark time.Time
	LastCommentID int64
	ETag          string
	LastSuccessAt time.Time
}
