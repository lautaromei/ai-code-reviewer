// Package clock contains infrastructure implementations of application clocks.
package clock

import "time"

// SystemClock reads the current UTC time from the operating system.
type SystemClock struct{}

func (SystemClock) Now() time.Time {
	return time.Now().UTC()
}
