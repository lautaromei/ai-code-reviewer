package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"github.com/lautaromei/ai-code-reviewer/internal/adapters/anthropic"
	"github.com/lautaromei/ai-code-reviewer/internal/adapters/claudecli"
	"github.com/lautaromei/ai-code-reviewer/internal/adapters/ghcli"
	"github.com/lautaromei/ai-code-reviewer/internal/adapters/sqlite"
	"github.com/lautaromei/ai-code-reviewer/internal/adapters/typesafejev"
	"github.com/lautaromei/ai-code-reviewer/internal/application"
	"github.com/lautaromei/ai-code-reviewer/internal/application/ports"
	"github.com/lautaromei/ai-code-reviewer/internal/config"
	"github.com/lautaromei/ai-code-reviewer/internal/domain"
	"github.com/lautaromei/ai-code-reviewer/internal/webhook"
	"github.com/lautaromei/ai-code-reviewer/internal/webui"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }
func run(args []string, out, errout io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(errout, "Usage: ai-reviewer <serve|review|doctor> --config config.yml")
		return 2
	}
	fs := flag.NewFlagSet(args[0], flag.ContinueOnError)
	fs.SetOutput(errout)
	cp := fs.String("config", "config.yml", "")
	repo := fs.String("repo", "", "")
	pr := fs.Int64("pr", 0, "")
	dry := fs.Bool("dry-run", false, "")
	if e := fs.Parse(args[1:]); e != nil {
		return 2
	}
	cfg, e := config.Load(*cp)
	if e != nil {
		fmt.Fprintln(errout, e)
		return 1
	}
	ctx := context.Background()
	if args[0] == "doctor" {
		return doctor(ctx, cfg, out, errout)
	}
	db, e := sqlite.Open(ctx, cfg.Storage.DSN, cfg.Storage.BusyTimeout)
	if e != nil {
		fmt.Fprintln(errout, e)
		return 1
	}
	defer db.Close()
	store := sqlite.NewReviewStore(db)
	if e = store.SeedReviewModel(ctx, cfg.Model.Name); e != nil {
		fmt.Fprintln(errout, e)
		return 1
	}
	seedRepos := []domain.RepositorySettings{}
	for _, r := range cfg.Repositories.Items {
		seedRepos = append(seedRepos, domain.RepositorySettings{FullName: r.Name, ReviewEnabled: r.ReviewEnabled, MiningEnabled: r.MiningEnabled, Categories: r.Categories})
	}
	if e = store.SeedRepositorySettings(ctx, seedRepos); e != nil {
		fmt.Fprintln(errout, e)
		return 1
	}
	if args[0] == "serve" {
		secret := os.Getenv(cfg.Webhook.SecretEnv)
		if secret == "" {
			fmt.Fprintf(errout, "%s is not set\n", cfg.Webhook.SecretEnv)
			return 1
		}
		gh, e := githubClientForRepository(ctx, cfg, store, "")
		if e != nil {
			fmt.Fprintln(errout, e)
			return 1
		}
		deps := application.ReviewDeps{GitHub: gh, Store: store, Prompts: sqlite.NewPromptStore(db), Provider: cfg.Model.Provider}
		if cfg.GitHub.Auth == "app" {
			deps.Checks = gh
		}
		for i := 0; i < cfg.Review.Workers; i++ {
			go reviewWorker(ctx, fmt.Sprintf("worker-%d", i+1), store, deps, cfg, errout)
		}
		mux := http.NewServeMux()
		mux.Handle("/webhooks/github", &webhook.Handler{Secret: []byte(secret), MaxBody: cfg.Webhook.MaxBodyBytes, DB: store})
		mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(200); _, _ = w.Write([]byte("ok")) })
		installURL := fmt.Sprintf("https://%s/apps/%s/installations/new", cfg.GitHub.Hostname, cfg.GitHub.AppSlug)
		var installationChecker interface {
			RepositoryInstalled(context.Context, string) (bool, error)
		} = gh
		if cfg.GitHub.Auth == "app" {
			installationChecker = store
		}
		admin := http.NewServeMux()
		admin.Handle("/rules/", webhook.RulePages(sqlite.NewRuleCatalog(db)))
		admin.Handle("/", webui.NewAdminWithInstallations(sqlite.NewRuleCatalog(db), sqlite.NewPromptStore(db), store, installationChecker, installURL))
		authenticatedAdmin, authErr := webui.WithAdminLoginFromEnvironment(admin)
		if authErr != nil {
			fmt.Fprintln(errout, authErr)
			return 1
		}
		mux.Handle("/", authenticatedAdmin)
		srv := &http.Server{Addr: cfg.Webhook.Listen, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
		fmt.Fprintf(out, "webhook listening on %s\n", cfg.Webhook.Listen)
		if e = srv.ListenAndServe(); e != nil && e != http.ErrServerClosed {
			fmt.Fprintln(errout, e)
			return 1
		}
		return 0
	}
	if args[0] != "review" || *repo == "" || *pr <= 0 {
		fmt.Fprintln(errout, "review requires --repo and --pr")
		return 2
	}
	r, ok, settingsErr := store.RepositorySettings(ctx, *repo)
	if settingsErr != nil || !ok || !r.ReviewEnabled {
		fmt.Fprintln(errout, "repository is not review-enabled")
		return 1
	}
	gh, e := githubClientForRepository(ctx, cfg, store, *repo)
	if e != nil {
		fmt.Fprintln(errout, e)
		return 1
	}
	modelName, e := store.EffectiveReviewModel(ctx, *repo)
	if e != nil {
		fmt.Fprintln(errout, e)
		return 1
	}
	model, e := reviewModel(cfg, modelName)
	if e != nil {
		fmt.Fprintln(errout, e)
		return 1
	}
	verificationModelName, e := store.EffectiveVerificationModel(ctx)
	if e != nil {
		fmt.Fprintln(errout, e)
		return 1
	}
	verificationModel, e := reviewModel(cfg, verificationModelName)
	if e != nil {
		fmt.Fprintln(errout, e)
		return 1
	}
	generationModelNames, e := store.GenerationModels(ctx, *repo)
	if e != nil {
		fmt.Fprintln(errout, e)
		return 1
	}
	generationModels, e := buildGenerationModels(cfg, generationModelNames)
	if e != nil {
		fmt.Fprintln(errout, e)
		return 1
	}
	deps := application.ReviewDeps{GitHub: gh, Model: model, GenerationModels: generationModels, VerificationModel: verificationModel, Store: store, Prompts: sqlite.NewPromptStore(db), Provider: cfg.Model.Provider, ModelName: modelName, VerificationModelName: verificationModelName}
	if cfg.GitHub.Auth == "app" {
		deps.Checks = gh
	}
	res, e := application.ReviewPullRequest(ctx, deps, application.ReviewOptions{Repository: *repo, PullRequest: *pr, DryRun: *dry, RuleBaseURL: cfg.Webhook.PublicURL, Categories: r.Categories, MaxComments: cfg.Review.MaxComments, MaxFiles: cfg.Review.MaxFiles, MaxFileBytes: cfg.Review.MaxFileBytes, MaxContextBytes: cfg.Review.MaxContextBytes})
	_ = json.NewEncoder(out).Encode(res)
	if e != nil {
		fmt.Fprintln(errout, e)
		return 1
	}
	return 0
}
func reviewWorker(ctx context.Context, owner string, store *sqlite.ReviewStore, deps application.ReviewDeps, cfg *config.Config, errout io.Writer) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			job, ok, e := store.Claim(ctx, owner, 2*time.Minute)
			if e != nil {
				fmt.Fprintf(errout, "worker claim: %v\n", e)
				continue
			}
			if !ok {
				continue
			}
			r, exists, settingsErr := store.RepositorySettings(ctx, job.Repository)
			if settingsErr != nil || !exists || !r.ReviewEnabled {
				reason := "repository review disabled"
				if settingsErr != nil {
					reason = settingsErr.Error()
				}
				_ = store.SetStatus(ctx, job.ID, domain.ReviewSkipped, reason, "")
				continue
			}
			modelName, modelErr := store.EffectiveReviewModel(ctx, job.Repository)
			if modelErr != nil {
				e = modelErr
			} else {
				jobDeps := deps
				if cfg.GitHub.Auth == "app" {
					var repositoryGitHub *ghcli.GitHub
					repositoryGitHub, e = githubClientForRepository(ctx, cfg, store, job.Repository)
					jobDeps.GitHub = repositoryGitHub
					jobDeps.Checks = repositoryGitHub
				}
				jobDeps.ModelName = modelName
				if e == nil {
					jobDeps.Model, e = reviewModel(cfg, modelName)
				}
				if e == nil {
					jobDeps.VerificationModelName, e = store.EffectiveVerificationModel(ctx)
				}
				if e == nil {
					jobDeps.VerificationModel, e = reviewModel(cfg, jobDeps.VerificationModelName)
				}
				if e == nil {
					var generationModelNames []string
					generationModelNames, e = store.GenerationModels(ctx, job.Repository)
					if e == nil {
						jobDeps.GenerationModels, e = buildGenerationModels(cfg, generationModelNames)
					}
					if e == nil {
						_, e = application.ProcessReviewJob(ctx, jobDeps, application.ReviewOptions{RuleBaseURL: cfg.Webhook.PublicURL, Categories: r.Categories, MaxComments: cfg.Review.MaxComments, MaxFiles: cfg.Review.MaxFiles, MaxFileBytes: cfg.Review.MaxFileBytes, MaxContextBytes: cfg.Review.MaxContextBytes}, job)
					}
				}
			}
			if e != nil {
				if job.Attempts < 3 {
					_ = store.Retry(ctx, job.ID, e.Error(), time.Now().Add(time.Duration(job.Attempts)*time.Minute))
				} else {
					_ = store.SetStatus(ctx, job.ID, domain.ReviewFailed, "", e.Error())
				}
			}
		}
	}
}
func buildGenerationModels(c *config.Config, names []string) ([]application.GenerationModel, error) {
	out := make([]application.GenerationModel, len(names))
	for i, name := range names {
		model, err := reviewModel(c, name)
		if err != nil {
			return nil, fmt.Errorf("generation agent %d: %w", i+1, err)
		}
		out[i] = application.GenerationModel{Name: name, Model: model}
	}
	return out, nil
}
func reviewModel(c *config.Config, modelName string) (ports.ReviewModel, error) {
	if modelName == "jev" {
		return typesafejev.New(os.Getenv("TYPESAFE_API_KEY"), typesafejev.WithHTTPClient(&http.Client{Timeout: c.Model.Timeout})), nil
	}
	if modelName != "sonnet" && modelName != "opus" && modelName != "haiku_max" {
		return nil, fmt.Errorf("unsupported review model %q", modelName)
	}
	if c.Model.Provider == "claude_cli" {
		if modelName == "haiku_max" {
			return nil, fmt.Errorf("haiku max requires the anthropic provider")
		}
		return claudecli.New(modelName, c.Model.Timeout), nil
	}
	if c.Model.Provider == "anthropic" {
		models := map[string]string{
			"sonnet":    "claude-sonnet-5-5",
			"opus":      "claude-opus-5-5",
			"haiku_max": "claude-haiku-5-5",
		}
		name := models[modelName]
		opts := []anthropic.Option{anthropic.WithHTTPClient(&http.Client{Timeout: c.Model.Timeout}), anthropic.WithMaxTokens(c.Review.MaxTokens)}
		if modelName == "haiku_max" {
			opts = append(opts, anthropic.WithEffort("high"))
		}
		return anthropic.New(os.Getenv("ANTHROPIC_API_KEY"), name, "v2", opts...), nil
	}
	return nil, fmt.Errorf("unsupported provider")
}
func githubClient(c *config.Config) (*ghcli.GitHub, error) {
	if c.GitHub.Auth != "app" {
		return ghcli.New(c.GitHub.Hostname, c.GitHub.APIVersion, c.GitHub.RequestTimeout, c.GitHub.MaxRetries), nil
	}
	appID, err := strconv.ParseInt(os.Getenv(c.GitHub.AppIDEnv), 10, 64)
	if err != nil || appID <= 0 {
		return nil, fmt.Errorf("%s must contain a GitHub App ID", c.GitHub.AppIDEnv)
	}
	installationID, err := strconv.ParseInt(os.Getenv(c.GitHub.InstallationIDEnv), 10, 64)
	if err != nil || installationID <= 0 {
		return nil, fmt.Errorf("%s must contain a GitHub App installation ID", c.GitHub.InstallationIDEnv)
	}
	keyPath := os.Getenv(c.GitHub.PrivateKeyPathEnv)
	key, err := os.ReadFile(keyPath)
	if err != nil {
		return nil, fmt.Errorf("read GitHub App private key: %w", err)
	}
	return ghcli.NewGitHubApp(c.GitHub.Hostname, c.GitHub.APIVersion, c.GitHub.RequestTimeout, c.GitHub.MaxRetries, appID, installationID, key)
}

func githubClientForRepository(ctx context.Context, c *config.Config, store *sqlite.ReviewStore, repository string) (*ghcli.GitHub, error) {
	if c.GitHub.Auth != "app" {
		return githubClient(c)
	}
	installationID := int64(0)
	var err error
	if repository != "" {
		installationID, err = store.InstallationID(ctx, repository)
	} else {
		installationID, _ = strconv.ParseInt(os.Getenv(c.GitHub.InstallationIDEnv), 10, 64)
		if installationID <= 0 {
			installationID = 1
		} // validates credentials; workers replace it per repository.
	}
	if err != nil {
		return nil, err
	}
	appID, err := strconv.ParseInt(os.Getenv(c.GitHub.AppIDEnv), 10, 64)
	if err != nil || appID <= 0 {
		return nil, fmt.Errorf("%s must contain a GitHub App ID", c.GitHub.AppIDEnv)
	}
	key, err := os.ReadFile(os.Getenv(c.GitHub.PrivateKeyPathEnv))
	if err != nil {
		return nil, fmt.Errorf("read GitHub App private key: %w", err)
	}
	return ghcli.NewGitHubApp(c.GitHub.Hostname, c.GitHub.APIVersion, c.GitHub.RequestTimeout, c.GitHub.MaxRetries, appID, installationID, key)
}
func doctor(ctx context.Context, c *config.Config, out, errout io.Writer) int {
	checks := []struct {
		name string
		args []string
	}{{"gh", []string{"auth", "status", "--hostname", c.GitHub.Hostname}}}
	if c.Model.Provider == "claude_cli" {
		checks = append(checks, struct {
			name string
			args []string
		}{"claude", []string{"--version"}})
	}
	failed := false
	for _, x := range checks {
		cmd := exec.CommandContext(ctx, x.name, x.args...)
		b, e := cmd.CombinedOutput()
		if e != nil {
			failed = true
			fmt.Fprintf(errout, "%s: failed: %s\n", x.name, strings.TrimSpace(string(b)))
		} else {
			fmt.Fprintf(out, "%s: ok\n", x.name)
		}
	}
	if c.Model.Provider == "anthropic" {
		if os.Getenv("ANTHROPIC_API_KEY") == "" {
			failed = true
			fmt.Fprintln(errout, "ANTHROPIC_API_KEY: missing")
		} else {
			fmt.Fprintln(out, "ANTHROPIC_API_KEY: set")
		}
	}
	if failed {
		return 1
	}
	return 0
}

var _ = strconv.Itoa
