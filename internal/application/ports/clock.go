package ports

import "time"

// Clock is an application boundary so use cases can be deterministic.
type Clock interface {
	Now() time.Time
}
