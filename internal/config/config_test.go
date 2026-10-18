package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const validV2 = `version: 2
storage: {driver: sqlite, dsn: var/test.db, busy_timeout: 5s}
github: {hostname: github.com, auth: gh}
repositories:
  - {name: acme/api, review_enabled: true, mining_enabled: true, categories: [golang, common, typescript]}
rules: {prefix: air, id_width: 3}
model: {provider: claude_cli, name: sonnet, timeout: 5m}
maintenance: {interval: 14d, min_human_mentions: 3, disable_after_downvotes: 3}
`

func writeConfig(t *testing.T, text string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "config.yml")
	if e := os.WriteFile(p, []byte(text), 0600); e != nil {
		t.Fatal(e)
	}
	return p
}
func TestLoadV2(t *testing.T) {
	c, e := Load(writeConfig(t, validV2))
	if e != nil {
		t.Fatal(e)
	}
	if c.Rules.Prefix != "AIR" {
		t.Fatalf("prefix=%q", c.Rules.Prefix)
	}
	want := []string{"GO", "COMMON", "TS"}
	for i, v := range want {
		if c.Repositories.Items[0].Categories[i] != v {
			t.Fatalf("categories=%v", c.Repositories.Items[0].Categories)
		}
	}
	if c.Model.Timeout != 5*time.Minute || c.Maintenance.Interval != 14*24*time.Hour {
		t.Fatal("durations were not parsed")
	}
	if c.Webhook.Listen != "127.0.0.1:8081" || c.Web.Listen != "127.0.0.1:8080" {
		t.Fatal("loopback defaults missing")
	}
}
func TestVersionOneIsActionable(t *testing.T) {
	_, e := Load(writeConfig(t, "version: 1\nrepositories: {}\n"))
	if e == nil || !strings.Contains(e.Error(), "migrate") {
		t.Fatalf("error=%v", e)
	}
}
func TestRepositorySelectionIsExplicit(t *testing.T) {
	bad := strings.Replace(validV2, "categories: [golang, common, typescript]", "categories: []", 1)
	_, e := Load(writeConfig(t, bad))
	if e == nil || !strings.Contains(e.Error(), "categories") {
		t.Fatalf("error=%v", e)
	}
}
func TestThresholdsAreFixedAtThree(t *testing.T) {
	bad := strings.Replace(validV2, "min_human_mentions: 3", "min_human_mentions: 2", 1)
	_, e := Load(writeConfig(t, bad))
	if e == nil || !strings.Contains(e.Error(), "requires 3") {
		t.Fatalf("error=%v", e)
	}
}
func TestProviders(t *testing.T) {
	for _, provider := range []string{"claude_cli", "anthropic"} {
		text := strings.Replace(validV2, "provider: claude_cli", "provider: "+provider, 1)
		if _, e := Load(writeConfig(t, text)); e != nil {
			t.Fatalf("%s: %v", provider, e)
		}
	}
}
func TestFlexibleDays(t *testing.T) {
	d, e := parseFlexibleDuration("14d")
	if e != nil || d != 336*time.Hour {
		t.Fatalf("duration=%v error=%v", d, e)
	}
}
