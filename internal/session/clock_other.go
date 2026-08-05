//go:build !windows

package session

import (
	"fmt"
	"time"
)

// ponytail: no cross-platform boot-relative monotonic in stdlib. Process start is
// the reference off Windows, which means a daemon restart looks like a reboot and
// falls through to the wall-clock gap credit. Correct, just coarser.
var processStart = time.Now()

type SystemClock struct{}

func (SystemClock) Wall() time.Time            { return time.Now() }
func (SystemClock) Monotonic() time.Duration   { return time.Since(processStart) }
func (SystemClock) Unbiased() time.Duration    { return time.Since(processStart) }
func (SystemClock) BootID() string {
	return fmt.Sprintf("proc-%d", processStart.UTC().Unix())
}
