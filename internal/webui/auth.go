package webui

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"html/template"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	adminUsernameEnv      = "AI_REVIEWER_ADMIN_USERNAME"
	adminPasswordEnv      = "AI_REVIEWER_ADMIN_PASSWORD"
	adminSessionSecretEnv = "AI_REVIEWER_ADMIN_SESSION_SECRET"
)

const (
	adminSessionCookie = "ai_reviewer_admin"
	adminSessionMaxAge = 30 * 24 * 60 * 60
)

type loginPageData struct {
	Error bool
	Next  string
}

var loginPage = template.Must(template.New("login").Parse(`<!doctype html>
<html lang="es"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>Acceso · AI Code Reviewer</title><style>
:root{color-scheme:light;--bg:#f4f6fa;--surface:#fff;--line:#dce3ee;--text:#172033;--muted:#68758b;--accent:#315bdc;--danger:#a12a3b}*{box-sizing:border-box}body{min-height:100vh;margin:0;display:grid;place-items:center;background:var(--bg);color:var(--text);font:14px/1.5 system-ui,-apple-system,"Segoe UI",sans-serif}.card{width:min(400px,calc(100% - 32px));padding:28px;background:var(--surface);border:1px solid var(--line);border-radius:12px;box-shadow:0 12px 32px rgba(33,51,84,.1)}h1{margin:0 0 6px;font-size:24px}.sub{margin:0 0 22px;color:var(--muted)}label{display:block;margin:14px 0 5px;font-weight:650}input{width:100%;padding:10px 11px;border:1px solid var(--line);border-radius:7px;font:inherit}button{width:100%;margin-top:22px;padding:10px;border:0;border-radius:7px;background:var(--accent);color:#fff;font:inherit;font-weight:700;cursor:pointer}.error{margin:0 0 14px;color:var(--danger)}
</style></head><body><form class="card" method="post" action="/login"><h1>AI Code Reviewer</h1><p class="sub">Accede al panel de administración.</p>{{if .Error}}<p class="error">Usuario o contraseña incorrectos.</p>{{end}}<input type="hidden" name="next" value="{{.Next}}"><label for="username">Usuario</label><input id="username" name="username" autocomplete="username" required autofocus><label for="password">Contraseña</label><input id="password" name="password" type="password" autocomplete="current-password" required><button type="submit">Entrar</button></form></body></html>`))

// WithAdminLoginFromEnvironment protects an administration handler using
// credentials and a session-signing secret supplied by the process environment.
func WithAdminLoginFromEnvironment(next http.Handler) (http.Handler, error) {
	username := os.Getenv(adminUsernameEnv)
	password := os.Getenv(adminPasswordEnv)
	secret := os.Getenv(adminSessionSecretEnv)
	if username == "" || password == "" || secret == "" {
		return nil, errors.New("admin login requires AI_REVIEWER_ADMIN_USERNAME, AI_REVIEWER_ADMIN_PASSWORD, and AI_REVIEWER_ADMIN_SESSION_SECRET")
	}
	if len(secret) < 32 {
		return nil, errors.New("AI_REVIEWER_ADMIN_SESSION_SECRET must contain at least 32 characters")
	}
	return withBasicLogin(next, username, password, []byte(secret), time.Now), nil
}

func withBasicLogin(next http.Handler, username, password string, secret []byte, now func() time.Time) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie, cookieErr := r.Cookie(adminSessionCookie)
		authenticated := cookieErr == nil && validSessionToken(cookie.Value, username, secret, now())

		switch r.URL.Path {
		case "/login":
			if r.Method == http.MethodGet {
				destination := safeRedirectTarget(r.URL.Query().Get("next"))
				if authenticated {
					http.Redirect(w, r, destination, http.StatusSeeOther)
					return
				}
				_ = loginPage.Execute(w, loginPageData{Error: r.URL.Query().Get("error") != "", Next: destination})
				return
			}
			if r.Method != http.MethodPost || !sameOrigin(r) {
				w.WriteHeader(http.StatusMethodNotAllowed)
				return
			}
			if !hmac.Equal([]byte(r.FormValue("username")), []byte(username)) || !hmac.Equal([]byte(r.FormValue("password")), []byte(password)) {
				destination := safeRedirectTarget(r.FormValue("next"))
				http.Redirect(w, r, "/login?error=1&next="+url.QueryEscape(destination), http.StatusSeeOther)
				return
			}
			token := sessionToken(username, now().Add(adminSessionMaxAge*time.Second), secret)
			http.SetCookie(w, sessionCookie(r, token, adminSessionMaxAge, now()))
			http.Redirect(w, r, safeRedirectTarget(r.FormValue("next")), http.StatusSeeOther)
			return
		case "/logout":
			if r.Method != http.MethodPost || !sameOrigin(r) {
				w.WriteHeader(http.StatusMethodNotAllowed)
				return
			}
			http.SetCookie(w, sessionCookie(r, "", -1, now()))
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}

		if authenticated {
			next.ServeHTTP(w, r)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/api/") {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "authentication required"})
			return
		}
		http.Redirect(w, r, "/login?next="+url.QueryEscape(r.URL.RequestURI()), http.StatusSeeOther)
	})
}

func safeRedirectTarget(target string) string {
	if target == "" || !strings.HasPrefix(target, "/") || strings.HasPrefix(target, "//") {
		return "/"
	}
	return target
}

func sessionToken(username string, expires time.Time, secret []byte) string {
	payload := username + "\x00" + strconv.FormatInt(expires.Unix(), 10)
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write([]byte(payload))
	return base64.RawURLEncoding.EncodeToString([]byte(payload)) + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func validSessionToken(token, username string, secret []byte, now time.Time) bool {
	parts := strings.Split(token, ".")
	if len(parts) != 2 {
		return false
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return false
	}
	signature, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return false
	}
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write(payload)
	if !hmac.Equal(signature, mac.Sum(nil)) {
		return false
	}
	fields := strings.Split(string(payload), "\x00")
	if len(fields) != 2 || !hmac.Equal([]byte(fields[0]), []byte(username)) {
		return false
	}
	expires, err := strconv.ParseInt(fields[1], 10, 64)
	return err == nil && now.Unix() < expires
}

func sessionCookie(r *http.Request, value string, maxAge int, now time.Time) *http.Cookie {
	secure := r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
	return &http.Cookie{Name: adminSessionCookie, Value: value, Path: "/", MaxAge: maxAge, Expires: now.Add(time.Duration(maxAge) * time.Second), HttpOnly: true, Secure: secure, SameSite: http.SameSiteLaxMode}
}
