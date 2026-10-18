package webhook

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

type deliveries struct {
	calls          int
	repo           string
	pr             int64
	result         string
	installationID int64
	registered     []string
}

func (d *deliveries) RecordDelivery(context.Context, string, string, string, string, time.Time) (bool, error) {
	d.calls++
	return true, nil
}
func (d *deliveries) RegisterInstallationRepositories(_ context.Context, id int64, names []string) error {
	d.installationID, d.registered = id, append([]string(nil), names...)
	return nil
}
func (d *deliveries) UnregisterInstallationRepositories(_ context.Context, id int64, names []string) error {
	d.installationID, d.registered = id, append([]string(nil), names...)
	return nil
}
func (d *deliveries) EnqueueDelivery(_ context.Context, _, _, _, result, repo string, pr int64, _ string, _ time.Time) (bool, error) {
	d.calls++
	d.repo = repo
	d.pr = pr
	d.result = result
	return true, nil
}

func TestInstallationAutomaticallyRegistersRepositories(t *testing.T) {
	body := []byte(`{"action":"created","installation":{"id":42},"repositories":[{"full_name":"acme/api"}]}`)
	d := &deliveries{}
	r := signed(body, "secret")
	r.Header.Set("X-GitHub-Event", "installation")
	w := httptest.NewRecorder()
	(&Handler{Secret: []byte("secret"), MaxBody: 1024, DB: d}).ServeHTTP(w, r)
	if w.Code != http.StatusAccepted || d.installationID != 42 || len(d.registered) != 1 || d.registered[0] != "acme/api" {
		t.Fatalf("status=%d delivery=%+v", w.Code, d)
	}
}
func signed(body []byte, secret string) *http.Request {
	r := httptest.NewRequest(http.MethodPost, "/webhooks/github", bytes.NewReader(body))
	m := hmac.New(sha256.New, []byte(secret))
	_, _ = m.Write(body)
	r.Header.Set("X-Hub-Signature-256", fmt.Sprintf("sha256=%x", m.Sum(nil)))
	r.Header.Set("X-GitHub-Event", "pull_request")
	r.Header.Set("X-GitHub-Delivery", "delivery-1")
	return r
}
func TestOpenedEnabledRepositoryQueues(t *testing.T) {
	body := []byte(`{"action":"opened","repository":{"full_name":"acme/api"},"pull_request":{"number":7,"head":{"sha":"abc"}}}`)
	d := &deliveries{}
	w := httptest.NewRecorder()
	(&Handler{Secret: []byte("secret"), MaxBody: 1024, Repositories: map[string]bool{"acme/api": true}, DB: d}).ServeHTTP(w, signed(body, "secret"))
	if w.Code != 202 || d.calls != 1 || d.result != "queued" || d.repo != "acme/api" || d.pr != 7 {
		t.Fatalf("status=%d delivery=%+v", w.Code, d)
	}
}
func TestOpenedDraftIsIgnoredUntilReady(t *testing.T) {
	body := []byte(`{"action":"opened","repository":{"full_name":"acme/api"},"pull_request":{"number":7,"draft":true,"head":{"sha":"abc"}}}`)
	d := &deliveries{}
	w := httptest.NewRecorder()
	(&Handler{Secret: []byte("secret"), MaxBody: 1024, Repositories: map[string]bool{"acme/api": true}, DB: d}).ServeHTTP(w, signed(body, "secret"))
	if w.Code != http.StatusAccepted || d.result != "ignored" {
		t.Fatalf("status=%d delivery=%+v", w.Code, d)
	}
}
func TestInvalidSignatureRejected(t *testing.T) {
	r := signed([]byte(`{}`), "wrong")
	d := &deliveries{}
	w := httptest.NewRecorder()
	(&Handler{Secret: []byte("secret"), MaxBody: 1024, DB: d}).ServeHTTP(w, r)
	if w.Code != 401 || d.calls != 0 {
		t.Fatalf("status=%d calls=%d", w.Code, d.calls)
	}
}
func TestOtherActionsArePersistedButIgnored(t *testing.T) {
	body := []byte(`{"action":"synchronize","repository":{"full_name":"acme/api"},"pull_request":{"number":7,"head":{"sha":"abc"}}}`)
	d := &deliveries{}
	w := httptest.NewRecorder()
	(&Handler{Secret: []byte("secret"), MaxBody: 1024, Repositories: map[string]bool{"acme/api": true}, DB: d}).ServeHTTP(w, signed(body, "secret"))
	if w.Code != 202 || d.result != "ignored" {
		t.Fatalf("status=%d delivery=%+v", w.Code, d)
	}
}

func TestReadyForReviewQueuesDraftWorkflow(t *testing.T) {
	body := []byte(`{"action":"ready_for_review","repository":{"full_name":"acme/api"},"pull_request":{"number":7,"head":{"sha":"abc"}}}`)
	d := &deliveries{}
	w := httptest.NewRecorder()
	(&Handler{Secret: []byte("secret"), MaxBody: 1024, Repositories: map[string]bool{"acme/api": true}, DB: d}).ServeHTTP(w, signed(body, "secret"))
	if w.Code != http.StatusAccepted || d.result != "queued" {
		t.Fatalf("status=%d delivery=%+v", w.Code, d)
	}
}
