package clock

import (
	"testing"
	"time"

	"github.com/lautaromei/testigo"
	"github.com/lautaromei/testigo/assert"
)

func TestSystemClock(t *testing.T) {
	testigo.Run(t, "returns UTC time", func(t *testing.T) {
		assert.Equal(t, SystemClock{}.Now().Location(), time.UTC)
	})
}
