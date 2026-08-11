package sandbox

import (
	"context"
	"time"

	"github.com/c4rb0nx1/tuprwre/internal/sandbox/pool"
)

// adminPool returns a pool handle for management commands. Unlike initPool it
// works even when warm pooling is disabled in config — leftover containers
// must stay manageable after the feature is toggled off.
func (d *DockerRuntime) adminPool() (*pool.WarmPool, error) {
	if err := d.initClient(); err != nil {
		return nil, err
	}
	if d.pool != nil {
		return d.pool, nil
	}

	ttl, err := time.ParseDuration(d.config.WarmPoolTTL)
	if err != nil {
		ttl = 10 * time.Minute
	}

	return pool.NewWarmPool(d.client, pool.PoolConfig{
		PoolDir:   d.config.PoolDir,
		MaxPerKey: d.config.WarmPoolMaxPerKey,
		MaxTotal:  d.config.WarmPoolMaxTotal,
		TTL:       ttl,
	}), nil
}

// PoolStatus lists warm pool containers with lease and idle information.
func (d *DockerRuntime) PoolStatus(ctx context.Context) ([]pool.ContainerStatus, error) {
	p, err := d.adminPool()
	if err != nil {
		return nil, err
	}
	return p.Status(ctx)
}

// PoolGC removes dead and TTL-expired warm pool containers.
func (d *DockerRuntime) PoolGC(ctx context.Context) (int, error) {
	p, err := d.adminPool()
	if err != nil {
		return 0, err
	}
	return p.GC(ctx)
}

// PoolDrain removes all unleased warm pool containers regardless of TTL.
func (d *DockerRuntime) PoolDrain(ctx context.Context) (int, error) {
	p, err := d.adminPool()
	if err != nil {
		return 0, err
	}
	return p.Drain(ctx)
}
