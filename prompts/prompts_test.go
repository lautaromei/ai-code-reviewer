package prompts

import (
	"testing"

	"github.com/lautaromei/testigo"
	"github.com/lautaromei/testigo/assert"
)

func TestEmbeddedPrompts(t *testing.T) {
	for _, prompt := range []struct {
		name string
		load func(string) (string, error)
	}{
		{"classification", Classify},
		{"compaction", Compact},
		{"rule drafting", DraftRule},
	} {
		testigo.Run(t, "loads the "+prompt.name+" conversation", func(t *testing.T) {
			content, err := prompt.load("v1")
			assert.NoError(t, err)
			assert.NotEmpty(t, content)
		})
	}

	testigo.Run(t, "reports an unknown prompt version", func(t *testing.T) {
		_, err := Classify("missing")
		assert.Error(t, err)
	})
}
