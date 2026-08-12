package backoff

import (
	"testing"
	"time"
)

func TestMooseFS_ImmediateAttempts(t *testing.T) {
	for trycnt := 0; trycnt < 3; trycnt++ {
		if got := MooseFS.Delay(trycnt); got != 0 {
			t.Errorf("Delay(%d) = %v, want 0 (immediate attempt)", trycnt, got)
		}
	}
}

func TestMooseFS_Ramp(t *testing.T) {
	cases := []struct {
		trycnt int
		want   time.Duration
	}{
		{3, 1 * time.Millisecond},
		{4, 301 * time.Millisecond},
		{5, 601 * time.Millisecond},
	}
	for _, c := range cases {
		if got := MooseFS.Delay(c.trycnt); got != c.want {
			t.Errorf("Delay(%d) = %v, want %v", c.trycnt, got, c.want)
		}
	}
}

func TestMooseFS_CappedAtMaxDelay(t *testing.T) {
	// Far enough along the ramp that the linear formula would exceed MaxDelay.
	got := MooseFS.Delay(1000)
	if got != MooseFS.MaxDelay {
		t.Errorf("Delay(1000) = %v, want cap %v", got, MooseFS.MaxDelay)
	}
}

func TestMooseFS_NegativeTrycntTreatedAsZero(t *testing.T) {
	if got := MooseFS.Delay(-5); got != 0 {
		t.Errorf("Delay(-5) = %v, want 0", got)
	}
}

func TestMooseFS_MonotonicNonDecreasing(t *testing.T) {
	prev := time.Duration(-1)
	for trycnt := 0; trycnt <= 200; trycnt++ {
		d := MooseFS.Delay(trycnt)
		if d < prev {
			t.Fatalf("Delay(%d) = %v is less than Delay(%d) = %v — schedule must be monotonic", trycnt, d, trycnt-1, prev)
		}
		if d > MooseFS.MaxDelay {
			t.Fatalf("Delay(%d) = %v exceeds MaxDelay %v", trycnt, d, MooseFS.MaxDelay)
		}
		prev = d
	}
}
