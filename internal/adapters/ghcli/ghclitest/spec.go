//go:generate go tool testigo-gen

package ghclitest

import "github.com/lautaromei/ai-code-reviewer/internal/application/ports"

type testigoSpec struct {
	PublicationRequest ports.PublicationRequest `testigo:"fixture"`
	Cursor             ports.Cursor             `testigo:"fixture"`
	ReviewComment      ports.ReviewComment      `testigo:"fixture"`
	ReactionRecord     ports.ReactionRecord     `testigo:"fixture"`
	ReactionPage       ports.ReactionPage       `testigo:"fixture"`
	Language           ports.Language           `testigo:"fixture"`
}
