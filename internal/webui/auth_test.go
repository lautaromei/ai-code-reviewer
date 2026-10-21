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
	if page.Code != http.StatusSeeOther || page.Header().Get("Location") != "/login?next=%2F" {
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
	if cookie.MaxAge != adminSessionMaxAge || !cookie.HttpOnly || !cookie.Secure || cookie.SameSite != http.SameSiteLaxMode {
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

func TestBasicLoginReturnsToRequestedRule(t *testing.T) {
	now := time.Date(2026, 10, 18, 10, 0, 0, 0, time.UTC)
	h := withBasicLogin(http.NotFoundHandler(), "admin", "password", []byte("a-session-secret-with-at-least-32-characters"), func() time.Time { return now })

	protected := httptest.NewRecorder()
	h.ServeHTTP(protected, httptest.NewRequest(http.MethodGet, "/rules/AIR-GO-001?source=github", nil))
	if protected.Code != http.StatusSeeOther || protected.Header().Get("Location") != "/login?next=%2Frules%2FAIR-GO-001%3Fsource%3Dgithub" {
		t.Fatalf("status=%d location=%q", protected.Code, protected.Header().Get("Location"))
	}

	loginPage := httptest.NewRecorder()
	h.ServeHTTP(loginPage, httptest.NewRequest(http.MethodGet, protected.Header().Get("Location"), nil))
	if !strings.Contains(loginPage.Body.String(), `name="next" value="/rules/AIR-GO-001?source=github"`) {
		t.Fatalf("login page did not preserve destination: %s", loginPage.Body.String())
	}

	form := url.Values{"username": {"admin"}, "password": {"password"}, "next": {"/rules/AIR-GO-001?source=github"}}
	login := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	h.ServeHTTP(login, req)
	if login.Code != http.StatusSeeOther || login.Header().Get("Location") != "/rules/AIR-GO-001?source=github" {
		t.Fatalf("status=%d location=%q", login.Code, login.Header().Get("Location"))
	}
}

func TestBasicLoginRecognizesExistingSessionAndRejectsExternalRedirect(t *testing.T) {
	now := time.Date(2026, 10, 18, 10, 0, 0, 0, time.UTC)
	secret := []byte("a-session-secret-with-at-least-32-characters")
	h := withBasicLogin(http.NotFoundHandler(), "admin", "password", secret, func() time.Time { return now })

	req := httptest.NewRequest(http.MethodGet, "/login?next=%2Frules%2FAIR-GO-001", nil)
	req.AddCookie(&http.Cookie{Name: adminSessionCookie, Value: sessionToken("admin", now.Add(time.Hour), secret)})
	r := httptest.NewRecorder()
	h.ServeHTTP(r, req)
	if r.Code != http.StatusSeeOther || r.Header().Get("Location") != "/rules/AIR-GO-001" {
		t.Fatalf("status=%d location=%q", r.Code, r.Header().Get("Location"))
	}

	form := url.Values{"username": {"admin"}, "password": {"password"}, "next": {"https://attacker.example"}}
	login := httptest.NewRecorder()
	loginRequest := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(form.Encode()))
	loginRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	h.ServeHTTP(login, loginRequest)
	if login.Header().Get("Location") != "/" {
		t.Fatalf("external redirect was accepted: %q", login.Header().Get("Location"))
	}
}

func TestBasicLoginRejectsInvalidCredentials(t *testing.T) {
	h := withBasicLogin(http.NotFoundHandler(), "admin", "password", []byte("a-session-secret-with-at-least-32-characters"), time.Now)
	form := url.Values{"username": {"admin"}, "password": {"wrong"}}
	r := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	h.ServeHTTP(r, req)
	if r.Code != http.StatusSeeOther || r.Header().Get("Location") != "/login?error=1&next=%2F" || len(r.Result().Cookies()) != 0 {
		t.Fatalf("status=%d location=%q cookies=%v", r.Code, r.Header().Get("Location"), r.Result().Cookies())
	}
}

func TestLogoutRequiresSameOriginPost(t *testing.T) {
	h := withBasicLogin(http.NotFoundHandler(), "admin", "password", []byte("a-session-secret-with-at-least-32-characters"), time.Now)

	get := httptest.NewRecorder()
	h.ServeHTTP(get, httptest.NewRequest(http.MethodGet, "/logout", nil))
	if get.Code != http.StatusMethodNotAllowed || len(get.Result().Cookies()) != 0 {
		t.Fatalf("GET logout status=%d cookies=%v", get.Code, get.Result().Cookies())
	}

	crossOrigin := httptest.NewRecorder()
	crossOriginRequest := httptest.NewRequest(http.MethodPost, "/logout", nil)
	crossOriginRequest.Header.Set("Origin", "https://attacker.example")
	h.ServeHTTP(crossOrigin, crossOriginRequest)
	if crossOrigin.Code != http.StatusMethodNotAllowed || len(crossOrigin.Result().Cookies()) != 0 {
		t.Fatalf("cross-origin logout status=%d cookies=%v", crossOrigin.Code, crossOrigin.Result().Cookies())
	}

	logout := httptest.NewRecorder()
	h.ServeHTTP(logout, httptest.NewRequest(http.MethodPost, "/logout", nil))
	if logout.Code != http.StatusSeeOther || logout.Header().Get("Location") != "/login" || len(logout.Result().Cookies()) != 1 || logout.Result().Cookies()[0].MaxAge != -1 {
		t.Fatalf("logout status=%d location=%q cookies=%v", logout.Code, logout.Header().Get("Location"), logout.Result().Cookies())
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
