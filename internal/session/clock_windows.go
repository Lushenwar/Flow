//go:build windows

package session

import (
	"fmt"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	kernelBase              = windows.NewLazySystemDLL("kernelbase.dll")
	procInterruptTime       = kernelBase.NewProc("QueryInterruptTime")
	procUnbiasedInterrupt   = kernelBase.NewProc("QueryUnbiasedInterruptTime")
)

// SystemClock reads the real machine.
type SystemClock struct{}

func (SystemClock) Wall() time.Time { return time.Now() }

// Monotonic uses the BIASED interrupt time, which keeps counting through sleep.
func (SystemClock) Monotonic() time.Duration {
	var t uint64
	procInterruptTime.Call(uintptr(unsafe.Pointer(&t)))
	return time.Duration(t) * 100 // units are 100ns
}

func (SystemClock) Unbiased() time.Duration {
	var t uint64
	procUnbiasedInterrupt.Call(uintptr(unsafe.Pointer(&t)))
	return time.Duration(t) * 100
}

// BootID is the derived boot instant at minute granularity. Minutes rather than
// seconds so an NTP step mid-boot is not mistaken for a reboot; a reboot that
// lands in the same minute is caught by the monotonic-went-backwards check in
// Anchor.sameBoot.
func (c SystemClock) BootID() string {
	boot := c.Wall().Add(-c.Monotonic()).UTC().Truncate(time.Minute)
	return fmt.Sprintf("boot-%d", boot.Unix())
}
