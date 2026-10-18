package ghcli

import (
	"errors"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var (
	ErrNotFound = errors.New("ghcli: resource not found (404)")

	ErrPermission = errors.New("ghcli: permission denied (403)")

	ErrRateLimited = errors.New("ghcli: rate limit exceeded after exhausting retries")

	ErrRecoverable = errors.New("ghcli: recoverable error")

	ErrNotModified = errors.New("ghcli: resource not modified (304)")
)

type errKind int

const (
	errKindOther errKind = iota
	errKindNotFound
	errKindPermission
	errKindRateLimit
	errKindNotModified
)

type errClassification struct {
	kind       errKind
	httpStatus int
	retryAfter time.Duration
}

var httpStatusPattern = regexp.MustCompile(`\(HTTP (\d{3})\)`)

var retryAfterPattern = regexp.MustCompile(`(?i)retry.after[^\d]{0,10}(\d+)`)

var rateLimitMarkers = []string{
	"rate limit",
	"abuse detection",
	"secondary rate limit",
}

func classifyError(exitCode int, stdout, stderr []byte) errClassification {
	text := string(stderr) + "\n" + string(stdout)

	status := 0
	if m := httpStatusPattern.FindStringSubmatch(text); m != nil {
		if n, err := strconv.Atoi(m[1]); err == nil {
			status = n
		}
	}

	switch {
	case status == 304:
		return errClassification{kind: errKindNotModified, httpStatus: status}
	case status == 404:
		return errClassification{kind: errKindNotFound, httpStatus: status}
	case status == 403 || status == 429:
		if looksLikeRateLimit(text) {
			return errClassification{kind: errKindRateLimit, httpStatus: status, retryAfter: parseRetryAfter(text)}
		}
		return errClassification{kind: errKindPermission, httpStatus: status}
	default:
		return errClassification{kind: errKindOther, httpStatus: status}
	}
}

func looksLikeRateLimit(text string) bool {
	lower := strings.ToLower(text)
	for _, marker := range rateLimitMarkers {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

const noExplicitRetryAfter time.Duration = -1

func parseRetryAfter(text string) time.Duration {
	if m := retryAfterPattern.FindStringSubmatch(text); m != nil {
		if secs, err := strconv.Atoi(m[1]); err == nil && secs >= 0 {
			return time.Duration(secs) * time.Second
		}
	}
	return noExplicitRetryAfter
}

func backoffDuration(attempt int) time.Duration {
	const (
		base    = 500 * time.Millisecond
		maxWait = 30 * time.Second
	)
	d := base
	for i := 0; i < attempt; i++ {
		d *= 2
		if d >= maxWait {
			return maxWait
		}
	}
	return d
}
