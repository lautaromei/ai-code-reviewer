package webhook

import (
	"context"
	"html/template"
	"net/http"
	"strings"
	"time"

	"github.com/lautaromei/ai-code-reviewer/internal/domain"
)

type RuleReader interface {
	Get(context.Context, string) (domain.Rule, bool, error)
	ListReferences(context.Context, string) ([]domain.RuleReference, error)
}

type rulePageData struct {
	Rule       domain.Rule
	References []domain.RuleReference
	ThumbsDown int
}

var rulePage = template.Must(template.New("rule").Funcs(template.FuncMap{
	"shortDate": func(t time.Time) string { return t.UTC().Format("2006-01-02") },
}).Parse(`<!doctype html>
<html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>{{.Rule.RuleID}} · AI Code Review</title><style>
:root{--bg:#f4f6fa;--surface:#fff;--soft:#f8fafc;--line:#dce3ee;--text:#172033;--muted:#68758b;--accent:#315bdc;--accent-soft:#e8edff;--success:#087443;--danger:#a12a3b;--shadow:0 8px 24px rgba(33,51,84,.07)}
*{box-sizing:border-box}html{color-scheme:light}body{margin:0;background:var(--bg);color:var(--text);font:14px/1.5 system-ui,-apple-system,"Segoe UI",sans-serif}a{color:var(--accent)}.shell{max-width:1180px;margin:auto;padding:22px 30px 40px}.topbar{display:flex;align-items:center;justify-content:space-between;gap:20px;margin-bottom:22px}.brand{font-size:18px;font-weight:750;color:var(--text);text-decoration:none}.back{display:inline-flex;align-items:center;gap:7px;padding:8px 11px;background:var(--surface);border:1px solid var(--line);border-radius:7px;color:var(--text);font-weight:650;text-decoration:none}.layout{display:grid;grid-template-columns:minmax(0,1.45fr) minmax(340px,.75fr);gap:18px}.card{background:var(--surface);border:1px solid var(--line);border-radius:10px;box-shadow:var(--shadow);padding:20px}.eyebrow{margin:0 0 7px;color:var(--muted);font-size:11px;font-weight:750;letter-spacing:.08em;text-transform:uppercase}.title-row{display:flex;align-items:flex-start;justify-content:space-between;gap:14px}h1{margin:0;font-size:25px;line-height:1.25}h2{margin:0;font-size:17px}.rule-id,.path{font:12px ui-monospace,SFMono-Regular,Menlo,monospace}.rule-id{display:inline-block;margin-bottom:8px;padding:4px 9px;background:var(--accent-soft);border-radius:999px;color:#2749a4}.pill{display:inline-flex;align-items:center;padding:3px 8px;border-radius:999px;background:var(--accent-soft);color:#2749a4;font-size:12px;white-space:nowrap}.pill.active{background:#e7f6ed;color:var(--success)}.meta-row{display:flex;flex-wrap:wrap;gap:7px;margin:16px 0 20px}.fields{display:grid;gap:22px}.field{padding:0}.field h2{margin:0 0 5px;color:var(--muted);font-size:11px;letter-spacing:.08em;text-transform:uppercase}.field p{margin:0;white-space:pre-wrap;font:14px/1.6 system-ui,-apple-system,"Segoe UI",sans-serif}.field.rule-quote h2{margin-bottom:8px}.field.rule-quote blockquote{max-width:720px;margin:0;padding:2px 0 2px 16px;border-left:3px solid var(--line);color:#39445a;white-space:pre-wrap;font:italic 16px/1.65 system-ui,-apple-system,"Segoe UI",sans-serif;font-weight:400}.field.scope{display:flex;align-items:baseline;gap:12px}.field.scope h2{flex:0 0 auto;margin:0}.field.scope p{color:var(--muted);font:12px ui-monospace,SFMono-Regular,Menlo,monospace}.field.bad h2,.field.bad p{color:var(--danger)}.field.good h2,.field.good p{color:var(--success)}.stats{display:grid;grid-template-columns:1fr 1fr;gap:10px;margin-bottom:18px}.stat{padding:14px;background:var(--soft);border:1px solid var(--line);border-radius:8px}.stat strong{display:block;font-size:24px;line-height:1.1}.stat span{color:var(--muted);font-size:12px}.negative{color:var(--danger)!important}.section-head{display:flex;align-items:center;justify-content:space-between;margin-bottom:12px}.section-head span{color:var(--muted);font-size:12px}.references{max-height:520px;overflow-y:auto;padding-right:7px;scrollbar-color:#b8c3d5 transparent;scrollbar-width:thin}.references::-webkit-scrollbar{width:8px}.references::-webkit-scrollbar-thumb{background:#b8c3d5;border-radius:999px}.reference{padding:14px 4px;border-top:1px solid var(--line)}.reference:first-child{border-top:0;padding-top:2px}.reference-head{display:flex;align-items:flex-start;justify-content:space-between;gap:12px}.reference-title{font-weight:700;text-decoration:none}.path{display:block;max-width:230px;margin-top:4px;overflow:hidden;color:var(--muted);text-overflow:ellipsis;white-space:nowrap}.reference p{display:-webkit-box;overflow:hidden;margin:9px 0;color:#39445a;-webkit-box-orient:vertical;-webkit-line-clamp:3}.reference-foot{display:flex;align-items:center;justify-content:space-between;gap:10px;color:var(--muted);font-size:12px}.open{font-weight:650;text-decoration:none}.empty{padding:36px 10px;text-align:center;color:var(--muted)}
@media(max-width:820px){.shell{padding:18px}.layout{grid-template-columns:1fr}.references{max-height:440px}.topbar{margin-bottom:16px}}@media(max-width:480px){.title-row{display:block}.title-row .pill{margin-top:10px}.stats{grid-template-columns:1fr 1fr}.reference-head{display:block}.path{max-width:100%}}
</style></head><body><div class="shell">
<header class="topbar"><a class="brand" href="/#rules">AI Code Review</a><a class="back" href="/#rules">← Back to rules</a></header>
<main class="layout"><section class="card"><div class="title-row"><div><span class="rule-id">{{.Rule.RuleID}}</span><h1>{{.Rule.Title}}</h1></div><span class="pill active">{{.Rule.Status}}</span></div><div class="meta-row"><span class="pill">{{.Rule.Language}}</span></div><div class="fields"><div class="field rule-quote"><h2>Rule</h2><blockquote>{{if .Rule.Rule}}{{.Rule.Rule}}{{else}}{{.Rule.Body}}{{end}}</blockquote></div><div class="field scope"><h2>Scope</h2><p>{{.Rule.Scope}}</p></div>{{if .Rule.Exception}}<div class="field"><h2>Exception</h2><p>{{.Rule.Exception}}</p></div>{{end}}{{if .Rule.BadExample}}<div class="field bad"><h2>Bad</h2><p>{{.Rule.BadExample}}</p></div>{{end}}{{if .Rule.GoodExample}}<div class="field good"><h2>Good</h2><p>{{.Rule.GoodExample}}</p></div>{{end}}</div></section>
<aside class="card"><div class="stats"><div class="stat"><strong>{{len .References}}</strong><span>PR mentions</span></div><div class="stat"><strong class="negative">{{.ThumbsDown}}</strong><span>thumbs-down</span></div></div><div class="section-head"><h2>Pull request references</h2><span>Newest first</span></div>
{{if .References}}<div class="references">{{range .References}}<article class="reference"><div class="reference-head"><div><a class="reference-title" href="{{.URL}}" target="_blank" rel="noreferrer">{{.Repository}} #{{.PullRequestNumber}}</a>{{if .Path}}<span class="path">{{.Path}}{{if .Line}}:{{.Line}}{{end}}</span>{{end}}</div><span class="pill{{if .ThumbsDown}} negative{{end}}">👎 {{.ThumbsDown}}</span></div><p>{{.Body}}</p><div class="reference-foot"><time>{{shortDate .CreatedAt}}</time><a class="open" href="{{.URL}}" target="_blank" rel="noreferrer">Open comment ↗</a></div></article>{{end}}</div>{{else}}<div class="empty">This rule has not been mentioned in a pull request yet.</div>{{end}}</aside></main>
</div></body></html>`))

func RulePages(rules RuleReader) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		id := strings.TrimPrefix(r.URL.Path, "/rules/")
		if id == "" || strings.Contains(id, "/") {
			http.NotFound(w, r)
			return
		}
		rule, found, err := rules.Get(r.Context(), id)
		if err != nil {
			http.Error(w, "could not load rule", http.StatusInternalServerError)
			return
		}
		if !found {
			http.NotFound(w, r)
			return
		}
		references, err := rules.ListReferences(r.Context(), id)
		if err != nil {
			http.Error(w, "could not load rule references", http.StatusInternalServerError)
			return
		}
		thumbsDown := 0
		for _, reference := range references {
			thumbsDown += reference.ThumbsDown
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_ = rulePage.Execute(w, rulePageData{Rule: rule, References: references, ThumbsDown: thumbsDown})
	}
}
