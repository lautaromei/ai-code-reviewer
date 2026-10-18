package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func configFile(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	text := `version: 2
storage: {driver: sqlite, dsn: ` + filepath.Join(dir, "rules.db") + `, busy_timeout: 5s}
repositories: [{name: acme/api, review_enabled: true, mining_enabled: false, categories: [GO]}]
rules: {prefix: AIR, id_width: 3}
model: {provider: claude_cli, name: sonnet}
maintenance: {min_human_mentions: 3, disable_after_downvotes: 3}
`
	p := filepath.Join(dir, "config.yml")
	if e := os.WriteFile(p, []byte(text), 0600); e != nil {
		t.Fatal(e)
	}
	return p
}
func TestMigrate(t *testing.T) {
	var out, err bytes.Buffer
	if code := run([]string{"migrate", "--config", configFile(t)}, &out, &err); code != 0 {
		t.Fatalf("code=%d stderr=%s", code, err.String())
	}
	if !strings.Contains(out.String(), "current") {
		t.Fatal(out.String())
	}
}
func TestPublishCommandRemoved(t *testing.T) {
	var out, err bytes.Buffer
	if code := run([]string{"publish"}, &out, &err); code == 0 || !strings.Contains(err.String(), "Usage") {
		t.Fatalf("code=%d stderr=%s", code, err.String())
	}
}
func TestBootstrapRequiresExplicitHistory(t *testing.T) {
	var out, err bytes.Buffer
	if code := run([]string{"bootstrap", "--config", configFile(t)}, &out, &err); code == 0 || !strings.Contains(err.String(), "--all-history") {
		t.Fatalf("code=%d stderr=%s", code, err.String())
	}
}
