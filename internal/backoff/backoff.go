// Package backoff implements the connection/request retry delay schedule
// used by the official MooseFS client (moosefs/mfsclient/readdata.c:1994):
// a handful of immediate retries (transient glitches are worth retrying
// instantly, at no latency cost), then a linear ramp, then a hard cap.
//
// Shared, top-level package (not under any plugin's internal/ tree) so it
// can be imported both by plugins/moosefs/internal/mfsclient (chunk-server
// I/O retries, issue #160) and by internal/placeholder (placeholder
// download retries, issue #162) — both layers apply the same, tuned-from-a-
// real-client retry cadence instead of each inventing its own.
package backoff

import "time"

// Schedule describes a MooseFS-style retry backoff: a number of free
// immediate attempts, followed by a linear ramp capped at MaxDelay.
type Schedule struct {
	// ImmediateAttempts is the number of prior attempts (0-based trycnt,
	// i.e. attempts already made before the one about to be tried) that
	// still incur zero delay.
	ImmediateAttempts int
	// RampBase is the delay once ImmediateAttempts has been reached
	// (trycnt == ImmediateAttempts).
	RampBase time.Duration
	// RampStep is the per-attempt delay increment beyond RampBase.
	RampStep time.Duration
	// MaxDelay caps the delay regardless of how many attempts were made.
	MaxDelay time.Duration
}

// MooseFS is the schedule used by the official MooseFS client
// (readdata.c:1994, values converted from µs to time.Duration):
//
//	trycnt   delay
//	< 3      0            — immediate, transient glitches are cheap to retry
//	3 → 30   1ms + (trycnt-3) × 300ms   — linear ramp
//	≥ 30     10s          — hard cap
//
// Delay's generic "cap once the ramp exceeds MaxDelay" behaviour reaches the
// same 10s ceiling slightly later (around trycnt≈36 rather than exactly 30);
// callers of this package never retry anywhere near that many times, so the
// difference is not observable in practice.
var MooseFS = Schedule{
	ImmediateAttempts: 3,
	RampBase:          1 * time.Millisecond,
	RampStep:          300 * time.Millisecond,
	MaxDelay:          10 * time.Second,
}

// Delay returns the delay to wait before the attempt that follows trycnt
// prior attempts (trycnt is 0-based: trycnt=0 means "no attempt made yet",
// so Delay(0) is the delay before the very first attempt — always 0 for any
// sane schedule with ImmediateAttempts ≥ 1). Negative trycnt is treated as 0.
func (s Schedule) Delay(trycnt int) time.Duration {
	if trycnt < 0 {
		trycnt = 0
	}
	if trycnt < s.ImmediateAttempts {
		return 0
	}
	d := s.RampBase + time.Duration(trycnt-s.ImmediateAttempts)*s.RampStep
	if d > s.MaxDelay || d < 0 { // d<0 guards a theoretical overflow on huge trycnt
		return s.MaxDelay
	}
	return d
}
