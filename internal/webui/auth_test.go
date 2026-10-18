package webui

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestBasicLoginProtectsPanelAndAPIs(t *testing.T) {
	h := withBasicLogin(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}), "admin", "password", []byte("a-session-secret-with-at-least-32-characters"), time.Now)

	page := httptest.NewRecorder()
	h.ServeHTTP(page, httptest.NewRequest(http.MethodGet, "/", nil))
	if page.Code != http.StatusSeeOther || page.Header().Get("Location") != "/login" {
		t.Fatalf("panel status=%d location=%q", page.Code, page.Header().Get("Location"))
	}

	api := httptest.NewRecorder()
	h.ServeHTTP(api, httptest.NewRequest(http.MethodGet, "/api/repositories", nil))
	if api.Code != http.StatusUnauthorized || !strings.Contains(api.Body.String(), "authentication required") {
		t.Fatalf("api status=%d response=%s", api.Code, api.Body.String())
	}
}

func TestBasicLoginCreatesLongLivedSecureCookie(t *testing.T) {
	now := time.Date(2026, 10, 18, 10, 0, 0, 0, time.UTC)
	h := withBasicLogin(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}), "admin", "password", []byte("a-session-secret-with-at-least-32-characters"), func() time.Time { return now })
	form := url.Values{"username": {"admin"}, "password": {"password"}}
	login := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-Forwarded-Proto", "https")
	h.ServeHTTP(login, req)
	if login.Code != http.StatusSeeOther {
		t.Fatalf("login status=%d response=%s", login.Code, login.Body.String())
	}
	cookies := login.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("cookies=%v", cookies)
	}
	cookie := cookies[0]
	if cookie.MaxAge != adminSessionMaxAge || !cookie.HttpOnly || !cookie.Secure || cookie.SameSite != http.SameSiteStrictMode {
		t.Fatalf("unexpected cookie: %+v", cookie)
	}

	panel := httptest.NewRecorder()
	authenticated := httptest.NewRequest(http.MethodGet, "/", nil)
	authenticated.AddCookie(cookie)
	h.ServeHTTP(panel, authenticated)
	if panel.Code != http.StatusNoContent {
		t.Fatalf("authenticated status=%d", panel.Code)
	}
}

func TestBasicLoginRejectsInvalidCredentials(t *testing.T) {
	h := withBasicLogin(http.NotFoundHandler(), "admin", "password", []byte("a-session-secret-with-at-least-32-characters"), time.Now)
	form := url.Values{"username": {"admin"}, "password": {"wrong"}}
	r := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	h.ServeHTTP(r, req)
	if r.Code != http.StatusSeeOther || r.Header().Get("Location") != "/login?error=1" || len(r.Result().Cookies()) != 0 {
		t.Fatalf("status=%d location=%q cookies=%v", r.Code, r.Header().Get("Location"), r.Result().Cookies())
	}
}

func TestSessionTokenExpiresAndRejectsTampering(t *testing.T) {
	secret := []byte("a-session-secret-with-at-least-32-characters")
	now := time.Date(2026, 10, 18, 10, 0, 0, 0, time.UTC)
	token := sessionToken("admin", now.Add(time.Hour), secret)
	if !validSessionToken(token, "admin", secret, now) {
		t.Fatal("fresh token was rejected")
	}
	if validSessionToken(token, "admin", secret, now.Add(2*time.Hour)) {
		t.Fatal("expired token was accepted")
	}
	if validSessionToken(token+"x", "admin", secret, now) {
		t.Fatal("tampered token was accepted")
	}
}

func TestAdminLoginRequiresEnvironmentSecrets(t *testing.T) {
	t.Setenv(adminUsernameEnv, "")
	t.Setenv(adminPasswordEnv, "")
	t.Setenv(adminSessionSecretEnv, "")
	if _, err := WithAdminLoginFromEnvironment(http.NotFoundHandler()); err == nil {
		t.Fatal("expected missing environment error")
	}
	t.Setenv(adminUsernameEnv, "admin")
	t.Setenv(adminPasswordEnv, "password")
	t.Setenv(adminSessionSecretEnv, "a-session-secret-with-at-least-32-characters")
	if _, err := WithAdminLoginFromEnvironment(http.NotFoundHandler()); err != nil {
		t.Fatalf("unexpected environment error: %v", err)
	}
}
