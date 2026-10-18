package webui

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"html/template"
	"net/http"
	"strings"
	"time"
)

const (
	adminSessionCookie = "ai_reviewer_admin"
	adminSessionMaxAge = 30 * 24 * 60 * 60
)

var loginPage = template.Must(template.New("login").Parse(`<!doctype html>
<html lang="es"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>Acceso · AI Code Reviewer</title><style>
:root{color-scheme:light;--bg:#f4f6fa;--surface:#fff;--line:#dce3ee;--text:#172033;--muted:#68758b;--accent:#315bdc;--danger:#a12a3b}*{box-sizing:border-box}body{min-height:100vh;margin:0;display:grid;place-items:center;background:var(--bg);color:var(--text);font:14px/1.5 system-ui,-apple-system,"Segoe UI",sans-serif}.card{width:min(400px,calc(100% - 32px));padding:28px;background:var(--surface);border:1px solid var(--line);border-radius:12px;box-shadow:0 12px 32px rgba(33,51,84,.1)}h1{margin:0 0 6px;font-size:24px}.sub{margin:0 0 22px;color:var(--muted)}label{display:block;margin:14px 0 5px;font-weight:650}input{width:100%;padding:10px 11px;border:1px solid var(--line);border-radius:7px;font:inherit}button{width:100%;margin-top:22px;padding:10px;border:0;border-radius:7px;background:var(--accent);color:#fff;font:inherit;font-weight:700;cursor:pointer}.error{margin:0 0 14px;color:var(--danger)}
</style></head><body><form class="card" method="post" action="/login"><h1>AI Code Reviewer</h1><p class="sub">Accede al panel de administración.</p>{{if .}}<p class="error">Usuario o contraseña incorrectos.</p>{{end}}<label for="username">Usuario</label><input id="username" name="username" autocomplete="username" required autofocus><label for="password">Contraseña</label><input id="password" name="password" type="password" autocomplete="current-password" required><button type="submit">Entrar</button></form></body></html>`))

// WithBasicLogin protects an administration handler with a persistent login cookie.
func WithBasicLogin(next http.Handler, username, password string) http.Handler {
	token := sessionToken(username, password)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/login":
			if r.Method == http.MethodGet {
				_ = loginPage.Execute(w, r.URL.Query().Get("error") != "")
				return
			}
			if r.Method != http.MethodPost || !sameOrigin(r) {
				w.WriteHeader(http.StatusMethodNotAllowed)
				return
			}
			if !hmac.Equal([]byte(r.FormValue("username")), []byte(username)) || !hmac.Equal([]byte(r.FormValue("password")), []byte(password)) {
				http.Redirect(w, r, "/login?error=1", http.StatusSeeOther)
				return
			}
			http.SetCookie(w, sessionCookie(r, token, adminSessionMaxAge))
			http.Redirect(w, r, "/", http.StatusSeeOther)
			return
		case "/logout":
			http.SetCookie(w, sessionCookie(r, "", -1))
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}

		cookie, err := r.Cookie(adminSessionCookie)
		if err == nil && hmac.Equal([]byte(cookie.Value), []byte(token)) {
			next.ServeHTTP(w, r)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/api/") {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "authentication required"})
			return
		}
		http.Redirect(w, r, "/login", http.StatusSeeOther)
	})
}

func sessionToken(username, password string) string {
	sum := sha256.Sum256([]byte("ai-reviewer-admin-session-v1\x00" + username + "\x00" + password))
	return hex.EncodeToString(sum[:])
}

func sessionCookie(r *http.Request, value string, maxAge int) *http.Cookie {
	secure := r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
	return &http.Cookie{Name: adminSessionCookie, Value: value, Path: "/", MaxAge: maxAge, Expires: time.Now().Add(time.Duration(maxAge) * time.Second), HttpOnly: true, Secure: secure, SameSite: http.SameSiteStrictMode}
}
