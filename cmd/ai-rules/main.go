package main

import (
	"context"
	"flag"
	"fmt"
	"github.com/lautaromei/ai-code-reviewer/internal/adapters/anthropic"
	"github.com/lautaromei/ai-code-reviewer/internal/adapters/claudecli"
	"github.com/lautaromei/ai-code-reviewer/internal/adapters/clock"
	"github.com/lautaromei/ai-code-reviewer/internal/adapters/ghcli"
	"github.com/lautaromei/ai-code-reviewer/internal/adapters/sqlite"
	"github.com/lautaromei/ai-code-reviewer/internal/application"
	"github.com/lautaromei/ai-code-reviewer/internal/application/ports"
	"github.com/lautaromei/ai-code-reviewer/internal/config"
	"github.com/lautaromei/ai-code-reviewer/internal/domain"
	"github.com/lautaromei/ai-code-reviewer/internal/webui"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

const usageMessage = "Usage: ai-rules <sync|bootstrap|serve|migrate> --config <path> [--all-history] [--if-due]"

func main() { processExit(run(os.Args[1:], os.Stdout, os.Stderr)) }

var processExit = os.Exit

func run(args []string, out, errout io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(errout, usageMessage)
		return 1
	}
	cmd := args[0]
	if cmd != "sync" && cmd != "bootstrap" && cmd != "serve" && cmd != "migrate" {
		fmt.Fprintln(errout, usageMessage)
		return 1
	}
	fs := flag.NewFlagSet(cmd, flag.ContinueOnError)
	fs.SetOutput(errout)
	cp := fs.String("config", "config.yml", "")
	all := fs.Bool("all-history", false, "")
	due := fs.Bool("if-due", false, "")
	if e := fs.Parse(args[1:]); e != nil {
		return 1
	}
	if cmd == "bootstrap" && !*all {
		fmt.Fprintln(errout, "bootstrap requires --all-history")
		return 1
	}
	cfg, e := config.Load(*cp)
	if e != nil {
		fmt.Fprintln(errout, e)
		return 1
	}
	ctx := context.Background()
	db, e := sqlite.Open(ctx, cfg.Storage.DSN, cfg.Storage.BusyTimeout)
	if e != nil {
		fmt.Fprintln(errout, e)
		return 1
	}
	defer db.Close()
	store := sqlite.NewReviewStore(db)
	seedRepos := []domain.RepositorySettings{}
	for _, r := range cfg.Repositories.Items {
		seedRepos = append(seedRepos, domain.RepositorySettings{FullName: r.Name, ReviewEnabled: r.ReviewEnabled, MiningEnabled: r.MiningEnabled, Categories: r.Categories})
	}
	if e = store.SeedRepositorySettings(ctx, seedRepos); e != nil {
		fmt.Fprintln(errout, e)
		return 1
	}
	switch cmd {
	case "migrate":
		fmt.Fprintln(out, "database schema is current")
		return 0
	case "serve":
		h, authErr := webui.WithAdminLoginFromEnvironment(webui.NewAdmin(sqlite.NewRuleCatalog(db), sqlite.NewPromptStore(db), store))
		if authErr != nil {
			fmt.Fprintln(errout, authErr)
			return 1
		}
		srv := &http.Server{Addr: cfg.Web.Listen, Handler: h, ReadHeaderTimeout: 5 * time.Second}
		fmt.Fprintf(out, "AI Rules UI available at http://%s\n", cfg.Web.Listen)
		if e = srv.ListenAndServe(); e != nil && e != http.ErrServerClosed {
			fmt.Fprintln(errout, e)
			return 1
		}
		return 0
	default:
		if *due {
			ok, x := store.MaintenanceDue(ctx, cfg.Maintenance.Interval)
			if x != nil {
				fmt.Fprintln(errout, x)
				return 1
			}
			if !ok {
				fmt.Fprintln(out, "maintenance is not due")
				return 0
			}
		}
		owner := fmt.Sprintf("pid-%d", os.Getpid())
		ok, x := store.AcquireMaintenance(ctx, "sync", owner, 30*time.Minute)
		if x != nil {
			fmt.Fprintln(errout, x)
			return 1
		}
		if !ok {
			fmt.Fprintln(errout, "another maintenance run holds the lease")
			return 1
		}
		defer store.ReleaseMaintenance(ctx, "sync", owner)
		classifier := maintenanceClassifier(cfg)
		if classifier == nil {
			fmt.Fprintln(errout, "model provider is not configured")
			return 1
		}
		github := ghcli.New(cfg.GitHub.Hostname, cfg.GitHub.APIVersion, cfg.GitHub.RequestTimeout, cfg.GitHub.MaxRetries)
		names := []string{}
		codes := map[string]string{}
		ext := map[string]string{".go": "go", ".php": "php", ".sql": "sql", ".js": "js", ".jsx": "js", ".ts": "ts", ".tsx": "ts", ".css": "css"}
		effectiveRepos, settingsErr := store.ListRepositorySettings(ctx)
		if settingsErr != nil {
			fmt.Fprintln(errout, settingsErr)
			return 1
		}
		for _, r := range effectiveRepos {
			if r.MiningEnabled {
				names = append(names, r.FullName)
			}
			for _, c := range r.Categories {
				codes[strings.ToLower(c)] = c
			}
		}
		initial := time.Now().UTC()
		if cmd == "bootstrap" {
			initial = time.Time{}
		}
		summary, x := application.Sync(ctx, application.SyncDeps{GitHub: github, Classifier: classifier, Repositories: sqlite.NewRepositoryDirectory(db), Cursors: sqlite.NewCommentCursor(db), Comments: sqlite.NewCommentArchive(db), Reactions: sqlite.NewRuleFeedback(db), Rules: sqlite.NewRuleCatalog(db), Candidates: sqlite.NewCandidateGroups(db), Classifications: sqlite.NewClassificationHistory(db), Runs: sqlite.NewRunJournal(db), Clock: clock.SystemClock{}}, application.SyncOptions{AnalyzedRepositories: names, CursorOverlap: cfg.Maintenance.CursorOverlap, InitialSince: initial, AIAuthors: cfg.GitHub.AIAuthors, ExtensionToLanguage: ext, RulePrefix: cfg.Rules.Prefix, IncludeReplies: cfg.Maintenance.IncludeReplies, PromptVersion: "v2", MaxCommentsPerCall: 50, LanguageCodes: codes, IDWidth: cfg.Rules.IDWidth, MinRepeatedComments: 3, MinThumbsDown: 3})
		fmt.Fprintf(out, "command: %s\nmessage: %s\n", cmd, summary.Message)
		if x != nil {
			fmt.Fprintln(errout, x)
			return 1
		}
		return 0
	}
}
func maintenanceClassifier(cfg *config.Config) ports.Classifier {
	if cfg.Model.Provider == "claude_cli" {
		return claudecli.New(cfg.Model.Name, cfg.Model.Timeout)
	}
	if cfg.Model.Provider == "anthropic" {
		name := cfg.Model.Name
		if name == "sonnet" {
			name = "claude-sonnet-4-5"
		}
		return anthropic.New(os.Getenv("ANTHROPIC_API_KEY"), name, "v2")
	}
	return nil
}
