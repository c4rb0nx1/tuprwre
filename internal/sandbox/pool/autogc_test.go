package pool

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// throttledPool builds a pool whose client is nil — safe for tests that must
// return before reaching GC's docker calls.
func throttledPool(t *testing.T) *WarmPool {
	t.Helper()
	return NewWarmPool(nil, PoolConfig{
		PoolDir: t.TempDir(),
		TTL:     10 * time.Minute,
	})
}

func TestMaybeGCSkipsWhenStampFresh(t *testing.T) {
	p := throttledPool(t)

	stamp := filepath.Join(p.cfg.PoolDir, gcStampName+".stamp")
	if err := os.WriteFile(stamp, nil, 0o644); err != nil {
		t.Fatalf("write stamp: %v", err)
	}

	removed, ran, err := p.MaybeGC(context.Background())
	if err != nil {
		t.Fatalf("MaybeGC: %v", err)
	}
	if ran || removed != 0 {
		t.Fatalf("MaybeGC ran despite fresh stamp (ran=%v removed=%d)", ran, removed)
	}
}

func TestMaybeGCSkipsWhenLockHeld(t *testing.T) {
	p := throttledPool(t)

	lockFile, err := tryLock(p.cfg.PoolDir, gcStampName)
	if err != nil {
		t.Fatalf("acquire gc lock: %v", err)
	}
	defer lockFile.Close()

	removed, ran, err := p.MaybeGC(context.Background())
	if err != nil {
		t.Fatalf("MaybeGC: %v", err)
	}
	if ran || removed != 0 {
		t.Fatalf("MaybeGC ran despite held lock (ran=%v removed=%d)", ran, removed)
	}
}

func TestMaybeGCHonorsStaleStampUnderMinInterval(t *testing.T) {
	p := throttledPool(t)

	stamp := filepath.Join(p.cfg.PoolDir, gcStampName+".stamp")
	if err := os.WriteFile(stamp, nil, 0o644); err != nil {
		t.Fatalf("write stamp: %v", err)
	}
	past := time.Now().Add(-30 * time.Second)
	if err := os.Chtimes(stamp, past, past); err != nil {
		t.Fatalf("age stamp: %v", err)
	}

	// 30s old is still inside the 1-minute floor even though TTL/2 for a
	// tiny TTL would be shorter.
	p.cfg.TTL = 10 * time.Second
	removed, ran, err := p.MaybeGC(context.Background())
	if err != nil {
		t.Fatalf("MaybeGC: %v", err)
	}
	if ran || removed != 0 {
		t.Fatalf("MaybeGC ignored the minimum interval floor (ran=%v removed=%d)", ran, removed)
	}
}
