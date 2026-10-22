package ports

import "context"

type PullRequest struct {
	Number  int64  `json:"number"`
	State   string `json:"state"`
	Title   string `json:"title"`
	Body    string `json:"body"`
	BaseSHA string `json:"base_sha"`
	HeadSHA string `json:"head_sha"`
	Draft   bool   `json:"draft"`
}
type ChangedFile struct {
	Path         string `json:"path"`
	PreviousPath string `json:"previous_path,omitempty"`
	Status       string `json:"status"`
	Patch        string `json:"patch"`
	Additions    int    `json:"additions"`
	Deletions    int    `json:"deletions"`
	Truncated    bool   `json:"truncated"`
}
type ExistingReviewComment struct {
	ID         int64
	Body, Path string
	Line       int
}
type PublishedComment struct {
	ID  int64
	URL string
}
type CheckRun struct {
	ID int64
}
type CheckRunReporter interface {
	StartCheckRun(context.Context, string, string, string) (CheckRun, error)
	CompleteCheckRun(context.Context, string, int64, string, string, string) error
}
type ReviewPublishRequest struct {
	Body, Path, Side string
	Line             int
}
type PullRequestGateway interface {
	GetPullRequest(context.Context, string, int64) (PullRequest, error)
	ListChangedFiles(context.Context, string, int64) ([]ChangedFile, error)
	ReadFileAtSHA(context.Context, string, string, string) ([]byte, error)
	ListPullRequestComments(context.Context, string, int64) ([]ExistingReviewComment, error)
	PublishReviewComment(context.Context, string, int64, string, ReviewPublishRequest) (PublishedComment, error)
	PublishIssueComment(context.Context, string, int64, string) (PublishedComment, error)
}

// ReviewContextGateway optionally supplies adjacent source paths without
// checking out or executing the reviewed repository.
type ReviewContextGateway interface {
	ListDirectoryAtSHA(context.Context, string, string, string) ([]string, error)
}

type ContextFile struct {
	Path      string   `json:"path"`
	Revision  string   `json:"revision"` // HEAD or BASE; content uses original line numbers.
	Content   string   `json:"content"`
	RelatedTo []string `json:"related_to,omitempty"`
}
type ReviewRule struct {
	RuleID      string   `json:"rule_id"`
	Category    string   `json:"category"`
	Title       string   `json:"title"`
	Body        string   `json:"body,omitempty"` // legacy callers only
	Rule        string   `json:"rule"`
	Scopes      []string `json:"scopes"`
	Exception   string   `json:"exception"`
	Exceptions  []string `json:"exceptions,omitempty"` // legacy callers only
	Examples    []string `json:"examples,omitempty"`   // legacy callers only
	BadExample  string   `json:"bad"`
	GoodExample string   `json:"good"`
	ContentHash string   `json:"content_hash"`
}
type ReviewPayload struct {
	Repository         string         `json:"repository"`
	PullRequest        PullRequest    `json:"pull_request"`
	SelectedCategories []string       `json:"selected_categories"`
	Rules              []ReviewRule   `json:"rules"`
	ChangedFiles       []ChangedFile  `json:"changed_files"`
	ContextFiles       []ContextFile  `json:"context_files,omitempty"`
	ReviewFocus        string         `json:"review_focus,omitempty"`
	ReviewPolicy       string         `json:"review_policy,omitempty"`
	Limitations        []string       `json:"limitations"`
	MaxFindings        int            `json:"max_findings"`
	Stage              string         `json:"stage"`
	CandidateFindings  []ModelFinding `json:"candidate_findings,omitempty"`
}
type ModelFinding struct {
	CandidateID string `json:"candidate_id,omitempty"`
	Verdict     string `json:"verdict,omitempty"`
	Reason      string `json:"reason,omitempty"`
	DuplicateOf string `json:"duplicate_of,omitempty"`
	Kind        string `json:"kind"`
	RuleID      string `json:"rule_id"`
	Path        string `json:"path"`
	Side        string `json:"side"`
	Line        int    `json:"line"`
	Confidence  int    `json:"confidence"`
	Severity    string `json:"severity"`
	Body        string `json:"body"`
	Suggestion  string `json:"suggestion"`
}
type ReviewModelResult struct {
	Summary             string         `json:"summary"`
	Findings            []ModelFinding `json:"findings"`
	Limitations         []string       `json:"limitations"`
	Model               string         `json:"model"`
	InputTokens         int64          `json:"input_tokens"`
	OutputTokens        int64          `json:"output_tokens"`
	CacheCreationTokens int64          `json:"cache_creation_tokens"`
	CacheReadTokens     int64          `json:"cache_read_tokens"`
}
type ReviewModel interface {
	Review(context.Context, string, ReviewPayload) (ReviewModelResult, error)
}
