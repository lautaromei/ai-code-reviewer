//go:generate go tool testigo-gen

package markdowntest

import "github.com/lautaromei/ai-code-reviewer/internal/domain"

type testigoSpec struct {
	Rule domain.Rule `testigo:"fixture"`
}
