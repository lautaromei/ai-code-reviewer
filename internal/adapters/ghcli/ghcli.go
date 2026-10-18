package ghcli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

type commandRunner func(ctx context.Context, name string, args ...string) (stdout []byte, stderr []byte, exitCode int, err error)

type temporaryRequestBody interface {
	io.Writer
	Name() string
	Close() error
}

type requestBodyFactory func() (temporaryRequestBody, error)

const (
	githubPageSize        = 100
	maxCommandErrorDetail = 4096
)

func defaultCommandRunner(ctx context.Context, name string, args ...string) ([]byte, []byte, int, error) {
	cmd := exec.CommandContext(ctx, name, args...)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	runErr := cmd.Run()
	if runErr == nil {
		return stdout.Bytes(), stderr.Bytes(), 0, nil
	}

	var exitErr *exec.ExitError
	if errors.As(runErr, &exitErr) {
		return stdout.Bytes(), stderr.Bytes(), exitErr.ExitCode(), nil
	}

	return stdout.Bytes(), stderr.Bytes(), -1, runErr
}

type GitHub struct {
	hostname       string
	apiVersion     string
	requestTimeout time.Duration
	maxRetries     int
	pageSize       int

	run         commandRunner
	requestBody requestBodyFactory
}

func (g *GitHub) withTokenSource(source *AppTokenSource) *GitHub {
	g.run = func(ctx context.Context, name string, args ...string) ([]byte, []byte, int, error) {
		token, err := source.Token(ctx)
		if err != nil {
			return nil, nil, -1, err
		}
		cmd := exec.CommandContext(ctx, name, args...)
		cmd.Env = append(os.Environ(), "GH_TOKEN="+token)
		var stdout, stderr bytes.Buffer
		cmd.Stdout, cmd.Stderr = &stdout, &stderr
		err = cmd.Run()
		if err == nil {
			return stdout.Bytes(), stderr.Bytes(), 0, nil
		}
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return stdout.Bytes(), stderr.Bytes(), exitErr.ExitCode(), nil
		}
		return stdout.Bytes(), stderr.Bytes(), -1, err
	}
	return g
}

func NewGitHubApp(hostname, apiVersion string, timeout time.Duration, retries int, appID, installationID int64, privateKey []byte) (*GitHub, error) {
	source, err := NewAppTokenSource(hostname, apiVersion, appID, installationID, privateKey)
	if err != nil {
		return nil, err
	}
	return New(hostname, apiVersion, timeout, retries).withTokenSource(source), nil
}

func New(hostname string, apiVersion string, requestTimeout time.Duration, maxRetries int) *GitHub {
	return &GitHub{
		hostname:       hostname,
		apiVersion:     apiVersion,
		requestTimeout: requestTimeout,
		maxRetries:     maxRetries,
		pageSize:       githubPageSize,
		run:            defaultCommandRunner,
		requestBody:    func() (temporaryRequestBody, error) { return os.CreateTemp("", "ghcli-body-*.json") },
	}
}

func (g *GitHub) execGH(ctx context.Context, args []string) ([]byte, error) {
	return g.execGHWithTimeout(ctx, args, g.requestTimeout)
}

func (g *GitHub) execGHWithTimeout(ctx context.Context, args []string, requestTimeout time.Duration) ([]byte, error) {
	var lastRateLimitErr error

	maxRetries := g.maxRetries
	if maxRetries < 0 {
		maxRetries = 0
	}

	for attempt := 0; ; attempt++ {
		callCtx, cancel := context.WithTimeout(ctx, requestTimeout)
		stdout, stderr, exitCode, runErr := g.run(callCtx, "gh", args...)
		cancel()

		if runErr != nil {
			return nil, fmt.Errorf("ghcli: could not execute `gh %s`: %w", strings.Join(args, " "), runErr)
		}

		if exitCode == 0 {
			return stdout, nil
		}

		classification := classifyError(exitCode, stdout, stderr)
		detail := truncateCommandDetail(stderr)
		if detail == "" {
			detail = truncateCommandDetail(stdout)
		}

		switch classification.kind {
		case errKindNotModified:
			return nil, fmt.Errorf("%w: `gh %s`: %s", ErrNotModified, strings.Join(args, " "), detail)

		case errKindNotFound:
			return nil, fmt.Errorf("%w: `gh %s`: %s", ErrNotFound, strings.Join(args, " "), detail)

		case errKindPermission:
			return nil, fmt.Errorf("%w: `gh %s`: %s", ErrPermission, strings.Join(args, " "), detail)

		case errKindRateLimit:
			lastRateLimitErr = fmt.Errorf("%w: `gh %s`: %s", ErrRateLimited, strings.Join(args, " "), detail)
			if attempt >= maxRetries {
				return nil, lastRateLimitErr
			}
			wait := classification.retryAfter
			if wait < 0 {
				wait = backoffDuration(attempt)
			}
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(wait):
			}
			continue

		default:
			return nil, fmt.Errorf("ghcli: `gh %s` failed (exit %d): %w: %s", strings.Join(args, " "), exitCode, ErrRecoverable, detail)
		}
	}
}

func (g *GitHub) baseFlags() []string {
	return []string{
		"-H", fmt.Sprintf("X-GitHub-Api-Version: %s", g.apiVersion),
		"--hostname", g.hostname,
	}
}

func (g *GitHub) apiGet(ctx context.Context, endpoint string) ([]byte, error) {
	args := append([]string{"api", endpoint}, g.baseFlags()...)
	return g.execGH(ctx, args)
}

func (g *GitHub) apiGetWithHeaders(ctx context.Context, endpoint string, headers map[string]string) ([]byte, error) {
	args := []string{"api", endpoint}
	for key, value := range headers {
		args = append(args, "-H", fmt.Sprintf("%s: %s", key, value))
	}
	args = append(args, g.baseFlags()...)
	return g.execGH(ctx, args)
}

func (g *GitHub) apiGetPaginated(ctx context.Context, endpoint string) ([]byte, error) {
	var pages bytes.Buffer
	for page := 1; ; page++ {
		pageEndpoint, err := endpointWithPage(endpoint, page)
		if err != nil {
			return nil, err
		}

		stdout, err := g.apiGet(ctx, pageEndpoint)
		if err != nil {
			return nil, fmt.Errorf("ghcli: fetch page %d: %w", page, err)
		}

		trimmed := bytes.TrimSpace(stdout)
		var items []json.RawMessage
		if err := json.Unmarshal(trimmed, &items); err != nil {
			return nil, fmt.Errorf("ghcli: page %d: expected a JSON array: %w", page, err)
		}

		if page > 1 {
			pages.WriteByte('\n')
		}
		pages.Write(trimmed)

		pageSize := g.pageSize
		if pageSize <= 0 {
			pageSize = githubPageSize
		}
		if len(items) < pageSize {
			return pages.Bytes(), nil
		}
	}
}

func endpointWithPage(endpoint string, page int) (string, error) {
	parsed, err := url.Parse(endpoint)
	if err != nil {
		return "", fmt.Errorf("ghcli: add page to endpoint: %w", err)
	}
	query := parsed.Query()
	query.Set("page", strconv.Itoa(page))
	parsed.RawQuery = query.Encode()
	return parsed.String(), nil
}

func truncateCommandDetail(raw []byte) string {
	detail := strings.TrimSpace(string(raw))
	if len(detail) <= maxCommandErrorDetail {
		return detail
	}
	return detail[:maxCommandErrorDetail] + "… [output truncated]"
}

func (g *GitHub) apiMutate(ctx context.Context, method string, endpoint string, body any) ([]byte, error) {
	tmp, err := g.requestBody()
	if err != nil {
		return nil, fmt.Errorf("ghcli: create temporary file for %s %s: %w", method, endpoint, err)
	}
	defer os.Remove(tmp.Name())

	if err := json.NewEncoder(tmp).Encode(body); err != nil {
		tmp.Close()
		return nil, fmt.Errorf("ghcli: serialize body for %s %s: %w", method, endpoint, err)
	}
	if err := tmp.Close(); err != nil {
		return nil, fmt.Errorf("ghcli: close temporary file for %s %s: %w", method, endpoint, err)
	}

	args := append([]string{"api", "-X", method, endpoint, "--input", tmp.Name()}, g.baseFlags()...)
	return g.execGH(ctx, args)
}
