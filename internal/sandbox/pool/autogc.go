package pool

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"time"
)

// gcStampName is the mtime marker for the last opportunistic sweep;
// gcLockName coordinates contenders across processes (same flock scheme as
// container leases). Container IDs are hex, so neither name can collide.
const (
	gcStampName = "gc"
	minGCEvery  = time.Minute
)

// MaybeGC runs GC when the previous opportunistic sweep is older than half
// the pool TTL (at least one minute). Contenders skip instead of waiting, so
// the hot run path pays one stat call in the common case. Called from the
// run path after lease release; cleanup must not require anyone to remember
// running `pool gc`.
func (p *WarmPool) MaybeGC(ctx context.Context) (int, bool, error) {
	interval := p.cfg.TTL / 2
	if interval < minGCEvery {
		interval = minGCEvery
	}

	stamp := filepath.Join(p.cfg.PoolDir, gcStampName+".stamp")
	if fresh(stamp, interval) {
		return 0, false, nil
	}

	lockFile, err := tryLock(p.cfg.PoolDir, gcStampName)
	if err != nil {
		if errors.Is(err, ErrLocked) {
			return 0, false, nil
		}
		return 0, false, err
	}
	defer func() {
		_ = lockFile.Close()
	}()

	// Re-check under the lock: another process may have swept while this one
	// was acquiring.
	if fresh(stamp, interval) {
		return 0, false, nil
	}

	removed, err := p.GC(ctx)
	if err != nil {
		return removed, true, err
	}

	if f, err := os.Create(stamp); err == nil {
		_ = f.Close()
	}
	return removed, true, nil
}

func fresh(stamp string, interval time.Duration) bool {
	info, err := os.Stat(stamp)
	return err == nil && time.Since(info.ModTime()) < interval
}
