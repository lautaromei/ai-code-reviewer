package config

import (
	"context"
	"errors"
	"fmt"
	"gopkg.in/yaml.v3"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var SupportedVersions = map[int]bool{2: true}

type Config struct {
	Version      int          `yaml:"version"`
	Storage      Storage      `yaml:"storage"`
	GitHub       GitHub       `yaml:"github"`
	Repositories Repositories `yaml:"repositories"`
	Rules        Rules        `yaml:"rules"`
	Model        Model        `yaml:"model"`
	Webhook      Webhook      `yaml:"webhook"`
	Review       Review       `yaml:"review"`
	Maintenance  Maintenance  `yaml:"maintenance"`
	Web          Web          `yaml:"web"`
	Comments     Comments     `yaml:"-"`
	Thresholds   Thresholds   `yaml:"-"`
	Classifier   Classifier   `yaml:"-"`
	Publish      Publish      `yaml:"-"`
}
type Storage struct {
	Driver         string        `yaml:"driver"`
	DSN            string        `yaml:"dsn"`
	BusyTimeout    time.Duration `yaml:"-"`
	BusyTimeoutRaw string
}
type GitHub struct {
	Hostname          string        `yaml:"hostname"`
	Auth              string        `yaml:"auth"`
	APIVersion        string        `yaml:"api_version"`
	AIAuthors         []string      `yaml:"ai_authors"`
	RequestTimeout    time.Duration `yaml:"-"`
	MaxRetries        int           `yaml:"max_retries"`
	AppIDEnv          string        `yaml:"app_id_env"`
	InstallationIDEnv string        `yaml:"installation_id_env"`
	PrivateKeyPathEnv string        `yaml:"private_key_path_env"`
	AppSlug           string        `yaml:"app_slug"`
}
type Repository struct {
	Name          string   `yaml:"name"`
	ReviewEnabled bool     `yaml:"review_enabled"`
	MiningEnabled bool     `yaml:"mining_enabled"`
	Categories    []string `yaml:"categories"`
}
type Repositories struct {
	Items      []Repository
	Analyzed   []string
	Actionable []ActionableRepo
}
type ActionableRepo struct {
	Name      string
	RulesPath string
	Languages []string
}

func (r *Repositories) UnmarshalYAML(n *yaml.Node) error { return n.Decode(&r.Items) }

type Comments struct {
	InitialSince                               string
	CursorOverlap, RefreshReactionsFor         time.Duration
	IncludeReplies                             bool
	JSONLPath                                  string
	MaxBodyCharsForModel, MaxHunkCharsForModel int
}
type Thresholds struct{ MinRepeatedComments, MinThumbsUp, MinThumbsDown int }
type Classifier struct {
	Provider, Model, PromptVersion string
	MaxCommentsPerCall             int
}
type Publish struct {
	Branch, PRTitle, CommitMessage string
	Draft                          bool
}
type Rules struct {
	Prefix                 string `yaml:"prefix"`
	IDWidth                int    `yaml:"id_width"`
	SeedDir, SnapshotTitle string
	ExpireAfter            time.Duration
	LanguageCodes          map[string]string
}
type Model struct {
	Provider string        `yaml:"provider"`
	Name     string        `yaml:"name"`
	Timeout  time.Duration `yaml:"-"`
}
type Webhook struct {
	Listen       string `yaml:"listen"`
	PublicURL    string `yaml:"public_url"`
	SecretEnv    string `yaml:"secret_env"`
	MaxBodyBytes int64  `yaml:"max_body_bytes"`
}
type Review struct {
	Workers         int    `yaml:"workers"`
	CommentLanguage string `yaml:"comment_language"`
	MaxComments     int    `yaml:"max_comments"`
	MaxFiles        int    `yaml:"max_files"`
	MaxFileBytes    int    `yaml:"max_file_bytes"`
	MaxContextBytes int    `yaml:"max_context_bytes"`
	MaxTokens       int    `yaml:"max_tokens"`
}
type Maintenance struct {
	Interval              time.Duration `yaml:"-"`
	MinHumanMentions      int           `yaml:"min_human_mentions"`
	DisableAfterDownvotes int           `yaml:"disable_after_downvotes"`
	IncludeReplies        bool          `yaml:"include_replies"`
	CursorOverlap         time.Duration `yaml:"-"`
}
type Web struct {
	Listen string `yaml:"listen"`
}

func (s *Storage) UnmarshalYAML(n *yaml.Node) error {
	var p struct {
		Driver string `yaml:"driver"`
		DSN    string `yaml:"dsn"`
		Busy   string `yaml:"busy_timeout"`
	}
	if e := n.Decode(&p); e != nil {
		return e
	}
	s.Driver = p.Driver
	s.DSN = p.DSN
	s.BusyTimeoutRaw = p.Busy
	if p.Busy != "" {
		d, e := parseFlexibleDuration(p.Busy)
		s.BusyTimeout = d
		return e
	}
	return nil
}
func (g *GitHub) UnmarshalYAML(n *yaml.Node) error {
	type plain GitHub
	var p struct {
		Hostname          string   `yaml:"hostname"`
		Auth              string   `yaml:"auth"`
		APIVersion        string   `yaml:"api_version"`
		AIAuthors         []string `yaml:"ai_authors"`
		RequestTimeout    string   `yaml:"request_timeout"`
		MaxRetries        int      `yaml:"max_retries"`
		AppIDEnv          string   `yaml:"app_id_env"`
		InstallationIDEnv string   `yaml:"installation_id_env"`
		PrivateKeyPathEnv string   `yaml:"private_key_path_env"`
		AppSlug           string   `yaml:"app_slug"`
	}
	if e := n.Decode(&p); e != nil {
		return e
	}
	g.Hostname = p.Hostname
	g.Auth = p.Auth
	g.APIVersion = p.APIVersion
	g.AIAuthors = p.AIAuthors
	g.MaxRetries = p.MaxRetries
	g.AppIDEnv, g.InstallationIDEnv, g.PrivateKeyPathEnv = p.AppIDEnv, p.InstallationIDEnv, p.PrivateKeyPathEnv
	g.AppSlug = p.AppSlug
	if p.RequestTimeout != "" {
		d, e := parseFlexibleDuration(p.RequestTimeout)
		g.RequestTimeout = d
		return e
	}
	return nil
}
func (m *Model) UnmarshalYAML(n *yaml.Node) error {
	var p struct {
		Provider string `yaml:"provider"`
		Name     string `yaml:"name"`
		Timeout  string `yaml:"timeout"`
	}
	if e := n.Decode(&p); e != nil {
		return e
	}
	m.Provider = p.Provider
	m.Name = p.Name
	if p.Timeout != "" {
		d, e := parseFlexibleDuration(p.Timeout)
		m.Timeout = d
		return e
	}
	return nil
}
func (m *Maintenance) UnmarshalYAML(n *yaml.Node) error {
	var p struct {
		Interval string `yaml:"interval"`
		Min      int    `yaml:"min_human_mentions"`
		Down     int    `yaml:"disable_after_downvotes"`
		Replies  bool   `yaml:"include_replies"`
		Overlap  string `yaml:"cursor_overlap"`
	}
	if e := n.Decode(&p); e != nil {
		return e
	}
	m.MinHumanMentions = p.Min
	m.DisableAfterDownvotes = p.Down
	m.IncludeReplies = p.Replies
	var e error
	if p.Interval != "" {
		m.Interval, e = parseFlexibleDuration(p.Interval)
	}
	if e == nil && p.Overlap != "" {
		m.CursorOverlap, e = parseFlexibleDuration(p.Overlap)
	}
	return e
}

var flexibleDurationPattern = regexp.MustCompile(`^(-?\d+)d$`)

func parseFlexibleDuration(s string) (time.Duration, error) {
	s = strings.TrimSpace(s)
	if m := flexibleDurationPattern.FindStringSubmatch(s); m != nil {
		v, e := strconv.Atoi(m[1])
		return time.Duration(v) * 24 * time.Hour, e
	}
	d, e := time.ParseDuration(s)
	if e != nil {
		return 0, fmt.Errorf("invalid duration %q: %w", s, e)
	}
	return d, nil
}
func Load(path string) (*Config, error) {
	b, e := os.ReadFile(path)
	if e != nil {
		return nil, fmt.Errorf("config: could not read %q: %w", path, e)
	}
	var header struct {
		Version int `yaml:"version"`
	}
	if e = yaml.Unmarshal(b, &header); e == nil && header.Version == 1 {
		return nil, errors.New("version: 1 is no longer supported; migrate the configuration to version: 2")
	}
	var c Config
	if e = yaml.Unmarshal(b, &c); e != nil {
		return nil, fmt.Errorf("config: invalid YAML in %q: %w", path, e)
	}
	c.applyDefaults()
	if e = c.Validate(); e != nil {
		return nil, e
	}
	return &c, nil
}
func (c *Config) applyDefaults() {
	if c.Storage.Driver == "" {
		c.Storage.Driver = "sqlite"
	}
	if c.Storage.DSN == "" {
		c.Storage.DSN = "var/ai-reviewer.db"
	}
	if c.Storage.BusyTimeout == 0 {
		c.Storage.BusyTimeout = 5 * time.Second
	}
	if c.GitHub.Hostname == "" {
		c.GitHub.Hostname = "github.com"
	}
	if c.GitHub.Auth == "" {
		c.GitHub.Auth = "gh"
	}
	if c.GitHub.AppIDEnv == "" {
		c.GitHub.AppIDEnv = "GITHUB_APP_ID"
	}
	if c.GitHub.InstallationIDEnv == "" {
		c.GitHub.InstallationIDEnv = "GITHUB_APP_INSTALLATION_ID"
	}
	if c.GitHub.PrivateKeyPathEnv == "" {
		c.GitHub.PrivateKeyPathEnv = "GITHUB_APP_PRIVATE_KEY_PATH"
	}
	if c.GitHub.AppSlug == "" {
		c.GitHub.AppSlug = "ai-code-reviewer"
	}
	if c.GitHub.APIVersion == "" {
		c.GitHub.APIVersion = "2022-11-28"
	}
	if c.GitHub.RequestTimeout == 0 {
		c.GitHub.RequestTimeout = 30 * time.Second
	}
	if c.GitHub.MaxRetries == 0 {
		c.GitHub.MaxRetries = 5
	}
	if c.Rules.IDWidth == 0 {
		c.Rules.IDWidth = 3
	}
	if c.Model.Provider == "" {
		c.Model.Provider = "claude_cli"
	}
	if c.Model.Name == "" {
		c.Model.Name = "opus"
	}
	if c.Model.Timeout == 0 {
		c.Model.Timeout = 5 * time.Minute
	}
	if c.Webhook.Listen == "" {
		c.Webhook.Listen = "127.0.0.1:8081"
	}
	if c.Webhook.SecretEnv == "" {
		c.Webhook.SecretEnv = "GITHUB_WEBHOOK_SECRET"
	}
	if c.Webhook.MaxBodyBytes == 0 {
		c.Webhook.MaxBodyBytes = 2 << 20
	}
	if c.Review.Workers == 0 {
		c.Review.Workers = 1
	}
	if c.Review.CommentLanguage == "" {
		c.Review.CommentLanguage = "en"
	}
	if c.Review.MaxComments == 0 {
		c.Review.MaxComments = 20
	}
	if c.Review.MaxFiles == 0 {
		c.Review.MaxFiles = 100
	}
	if c.Review.MaxFileBytes == 0 {
		c.Review.MaxFileBytes = 256 << 10
	}
	if c.Review.MaxContextBytes == 0 {
		c.Review.MaxContextBytes = 1 << 20
	}
	if c.Review.MaxTokens == 0 {
		c.Review.MaxTokens = 128000
	}
	if c.Maintenance.Interval == 0 {
		c.Maintenance.Interval = 14 * 24 * time.Hour
	}
	if c.Maintenance.MinHumanMentions == 0 {
		c.Maintenance.MinHumanMentions = 3
	}
	if c.Maintenance.DisableAfterDownvotes == 0 {
		c.Maintenance.DisableAfterDownvotes = 3
	}
	if c.Maintenance.CursorOverlap == 0 {
		c.Maintenance.CursorOverlap = 10 * time.Minute
	}
	if c.Web.Listen == "" {
		c.Web.Listen = "127.0.0.1:8080"
	}
}

var repoPattern = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`)
var prefixPattern = regexp.MustCompile(`^[A-Za-z0-9-]+$`)
var aliases = map[string]string{"TYPESCRIPT": "TS", "JAVASCRIPT": "JS", "GOLANG": "GO"}

func CanonicalCategory(v string) string {
	v = strings.ToUpper(strings.TrimSpace(v))
	if a := aliases[v]; a != "" {
		return a
	}
	return v
}
func (c *Config) Validate() error {
	var es []error
	if c.Version == 1 {
		return errors.New("version: 1 is no longer supported; migrate the configuration to version: 2")
	}
	if !SupportedVersions[c.Version] {
		es = append(es, fmt.Errorf("version: %d is not supported", c.Version))
	}
	if c.Storage.Driver != "sqlite" {
		es = append(es, errors.New("storage.driver: only sqlite is supported"))
	}
	if c.Rules.Prefix == "" || !prefixPattern.MatchString(c.Rules.Prefix) {
		es = append(es, errors.New("rules.prefix: must contain only letters, numbers, or hyphens"))
	}
	c.Rules.Prefix = strings.ToUpper(c.Rules.Prefix)
	seen := map[string]bool{}
	for i := range c.Repositories.Items {
		r := &c.Repositories.Items[i]
		if !repoPattern.MatchString(r.Name) {
			es = append(es, fmt.Errorf("repositories[%d].name: must use owner/name", i))
		}
		if seen[r.Name] {
			es = append(es, fmt.Errorf("repositories: %q is duplicated", r.Name))
		}
		seen[r.Name] = true
		if len(r.Categories) == 0 && (r.ReviewEnabled || r.MiningEnabled) {
			es = append(es, fmt.Errorf("repositories[%s].categories: cannot be empty", r.Name))
		}
		for j := range r.Categories {
			r.Categories[j] = CanonicalCategory(r.Categories[j])
		}
	}
	if c.Maintenance.MinHumanMentions != 3 {
		es = append(es, errors.New("maintenance.min_human_mentions: this version requires 3"))
	}
	if c.Maintenance.DisableAfterDownvotes != 3 {
		es = append(es, errors.New("maintenance.disable_after_downvotes: this version requires 3"))
	}
	if c.Model.Provider != "claude_cli" && c.Model.Provider != "anthropic" {
		es = append(es, fmt.Errorf("model.provider: %q is not supported", c.Model.Provider))
	}
	if c.GitHub.Auth != "gh" && c.GitHub.Auth != "app" {
		es = append(es, fmt.Errorf("github.auth: %q is not supported", c.GitHub.Auth))
	}
	if c.Storage.DSN == "" {
		es = append(es, errors.New("storage.dsn: cannot be empty"))
	} else if e := os.MkdirAll(filepath.Dir(c.Storage.DSN), 0755); e != nil {
		es = append(es, e)
	}
	return errors.Join(es...)
}
func (c *Config) Repository(name string) (Repository, bool) {
	for _, r := range c.Repositories.Items {
		if r.Name == name {
			return r, true
		}
	}
	return Repository{}, false
}
func (c *Config) ValidateEnvironment(ctx context.Context) error {
	var es []error
	cmd := exec.CommandContext(ctx, "gh", "auth", "status", "--hostname", c.GitHub.Hostname)
	if b, e := cmd.CombinedOutput(); e != nil {
		es = append(es, fmt.Errorf("gh authentication failed: %w: %s", e, strings.TrimSpace(string(b))))
	}
	if c.Model.Provider == "anthropic" && os.Getenv("ANTHROPIC_API_KEY") == "" {
		es = append(es, errors.New("ANTHROPIC_API_KEY is not set"))
	}
	if c.Model.Provider == "claude_cli" {
		if _, e := exec.LookPath("claude"); e != nil {
			es = append(es, errors.New("claude CLI was not found in PATH"))
		}
	}
	return errors.Join(es...)
}
func ensureWritableParentDir(path string) error {
	if path == "" {
		return errors.New("path is empty")
	}
	dir := filepath.Dir(path)
	if e := os.MkdirAll(dir, 0755); e != nil {
		return e
	}
	f, e := os.CreateTemp(dir, ".ai-reviewer-writable-*")
	if e != nil {
		return e
	}
	name := f.Name()
	_ = f.Close()
	return os.Remove(name)
}
