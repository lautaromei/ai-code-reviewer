package webhook

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type Handler struct {
	Secret       []byte
	MaxBody      int64
	Repositories map[string]bool
	DB           DeliveryStore
}
type DeliveryStore interface {
	RecordDelivery(context.Context, string, string, string, string, time.Time) (bool, error)
	EnqueueDelivery(context.Context, string, string, string, string, string, int64, string, time.Time) (bool, error)
}
type repositoryPolicy interface {
	ReviewEnabled(context.Context, string) (bool, error)
}
type installationRegistry interface {
	RegisterInstallationRepositories(context.Context, int64, []string) error
	UnregisterInstallationRepositories(context.Context, int64, []string) error
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(405)
		return
	}
	body, e := io.ReadAll(http.MaxBytesReader(w, r.Body, h.MaxBody))
	if e != nil {
		http.Error(w, "request too large", 413)
		return
	}
	sig := strings.TrimPrefix(r.Header.Get("X-Hub-Signature-256"), "sha256=")
	got, e := hex.DecodeString(sig)
	mac := hmac.New(sha256.New, h.Secret)
	_, _ = mac.Write(body)
	if e != nil || !hmac.Equal(got, mac.Sum(nil)) {
		http.Error(w, "invalid signature", 401)
		return
	}
	event := r.Header.Get("X-GitHub-Event")
	delivery := r.Header.Get("X-GitHub-Delivery")
	if delivery == "" {
		http.Error(w, "missing delivery id", 400)
		return
	}
	if event == "ping" {
		_, e = h.DB.RecordDelivery(r.Context(), delivery, event, "", "ping", time.Now().UTC())
		if e != nil {
			http.Error(w, e.Error(), 500)
			return
		}
		w.WriteHeader(202)
		return
	}
	var p struct {
		Action     string `json:"action"`
		Repository struct {
			FullName string `json:"full_name"`
		} `json:"repository"`
		PullRequest struct {
			Number int64 `json:"number"`
			Draft  bool  `json:"draft"`
			Head   struct {
				SHA string `json:"sha"`
			} `json:"head"`
		} `json:"pull_request"`
		Installation struct {
			ID int64 `json:"id"`
		} `json:"installation"`
		Repositories []struct {
			FullName string `json:"full_name"`
		} `json:"repositories"`
		RepositoriesAdded []struct {
			FullName string `json:"full_name"`
		} `json:"repositories_added"`
		RepositoriesRemoved []struct {
			FullName string `json:"full_name"`
		} `json:"repositories_removed"`
	}
	if e = json.Unmarshal(body, &p); e != nil {
		http.Error(w, "invalid payload", 400)
		return
	}
	if registry, ok := h.DB.(installationRegistry); ok {
		added := repositoryNames(p.Repositories)
		added = append(added, repositoryNames(p.RepositoriesAdded)...)
		if p.Repository.FullName != "" && p.Installation.ID > 0 {
			added = append(added, p.Repository.FullName)
		}
		if len(added) > 0 {
			if e = registry.RegisterInstallationRepositories(r.Context(), p.Installation.ID, added); e != nil {
				http.Error(w, e.Error(), 500)
				return
			}
		}
		removed := repositoryNames(p.RepositoriesRemoved)
		if event == "installation" && (p.Action == "deleted" || p.Action == "suspend") {
			removed = repositoryNames(p.Repositories)
		}
		if len(removed) > 0 || (event == "installation" && (p.Action == "deleted" || p.Action == "suspend")) {
			if e = registry.UnregisterInstallationRepositories(r.Context(), p.Installation.ID, removed); e != nil {
				http.Error(w, e.Error(), 500)
				return
			}
		}
	}
	if event == "installation" || event == "installation_repositories" {
		_, e = h.DB.RecordDelivery(r.Context(), delivery, event, p.Action, "registered", time.Now().UTC())
		if e != nil {
			http.Error(w, e.Error(), 500)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		_, _ = fmt.Fprint(w, `{"result":"registered"}`)
		return
	}
	enabled := h.Repositories[p.Repository.FullName]
	if policy, ok := h.DB.(repositoryPolicy); ok {
		enabled, e = policy.ReviewEnabled(r.Context(), p.Repository.FullName)
		if e != nil {
			http.Error(w, e.Error(), 500)
			return
		}
	}
	result := "ignored"
	readyEvent := p.Action == "ready_for_review" || (p.Action == "opened" && !p.PullRequest.Draft)
	if event == "pull_request" && readyEvent && enabled {
		result = "queued"
	}
	_, e = h.DB.EnqueueDelivery(r.Context(), delivery, event, p.Action, result, p.Repository.FullName, p.PullRequest.Number, p.PullRequest.Head.SHA, time.Now().UTC())
	if e != nil {
		http.Error(w, e.Error(), 500)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(202)
	_, _ = fmt.Fprintf(w, `{"result":%q}`, result)
}

func repositoryNames(repositories []struct {
	FullName string `json:"full_name"`
}) []string {
	out := make([]string, 0, len(repositories))
	for _, repository := range repositories {
		if repository.FullName != "" {
			out = append(out, repository.FullName)
		}
	}
	return out
}
