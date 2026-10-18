package markdown

import (
	"bytes"
	"testing"

	"github.com/lautaromei/ai-code-reviewer/internal/adapters/markdown/markdowntest"
	"github.com/lautaromei/ai-code-reviewer/internal/domain"
	"github.com/lautaromei/testigo"
	"github.com/lautaromei/testigo/assert"
)

func sampleRules() []domain.Rule {
	return []domain.Rule{
		markdowntest.Rule.WithRuleID("GO-002").WithLanguage("go").WithTitle("Second rule").WithBody("Body 2").Bare(),
		markdowntest.Rule.WithRuleID("PHP-001").WithLanguage("php").WithTitle("PHP rule").WithBody("PHP body").Bare(),
		markdowntest.Rule.WithRuleID("GO-001").WithLanguage("go").WithTitle("First rule").WithBody("Body 1").Bare(),
	}
}

func TestRuleCatalogRendering(t *testing.T) {
	testigo.Run(t, "is deterministic", func(t *testing.T) {
		rules := sampleRules()
		assert.Equal(t, Render("Catalog", rules), Render("Catalog", rules))
	})

	testigo.Run(t, "does not depend on input order", func(t *testing.T) {
		ordered := sampleRules()
		shuffled := []domain.Rule{ordered[2], ordered[0], ordered[1]}
		assert.Equal(t, Render("Catalog", shuffled), Render("Catalog", ordered))
	})

	testigo.Run(t, "orders rules within a language", func(t *testing.T) {
		catalog := Render("Catalog", sampleRules())
		assert.Less(t, bytes.Index(catalog, []byte("GO-001")), bytes.Index(catalog, []byte("GO-002")))
	})

	testigo.Run(t, "orders language sections", func(t *testing.T) {
		catalog := Render("Catalog", sampleRules())
		assert.Less(t, bytes.Index(catalog, []byte("# go")), bytes.Index(catalog, []byte("# php")))
	})

	testigo.Run(t, "merges generated rules after repository-owned content", func(t *testing.T) {
		rendered := Render("Catalog", sampleRules())
		assert.Equal(t, Merge(nil, "Catalog", sampleRules()), rendered)
		assert.Equal(t, Merge([]byte("Repository guidance"), "Catalog", sampleRules()), append([]byte("Repository guidance\n\n"), rendered...))
		assert.Equal(t, Merge([]byte("Repository guidance\n"), "Catalog", sampleRules()), append([]byte("Repository guidance\n\n"), rendered...))
	})
}
