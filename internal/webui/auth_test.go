package webui

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestBasicLoginProtectsPanelAndAPIs(t *testing.T) {
	h := WithBasicLogin(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}), "sfl", "review123")

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
	h := WithBasicLogin(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}), "sfl", "review123")
	form := url.Values{"username": {"sfl"}, "password": {"review123"}}
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
	h := WithBasicLogin(http.NotFoundHandler(), "sfl", "review123")
	form := url.Values{"username": {"sfl"}, "password": {"wrong"}}
	r := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	h.ServeHTTP(r, req)
	if r.Code != http.StatusSeeOther || r.Header().Get("Location") != "/login?error=1" || len(r.Result().Cookies()) != 0 {
		t.Fatalf("status=%d location=%q cookies=%v", r.Code, r.Header().Get("Location"), r.Result().Cookies())
	}
}
