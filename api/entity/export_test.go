package entity

import "time"

// SetMissDelayForTest overrides the AuthorizeResolve miss-delay tunables and
// returns a func restoring the previous values.
func SetMissDelayForTest(floor, jitter time.Duration) (restore func()) {
	of, oj := missDelayFloor, missDelayJitter
	missDelayFloor, missDelayJitter = floor, jitter
	return func() { missDelayFloor, missDelayJitter = of, oj }
}
