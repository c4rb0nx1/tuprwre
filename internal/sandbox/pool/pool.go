package pool

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/client"
)

// PoolConfig configures warm pool behavior.
type PoolConfig struct {
	PoolDir   string
	MaxPerKey int
	MaxTotal  int
	TTL       time.Duration
}

// WarmPool manages warm sandbox containers.
type WarmPool struct {
	client *client.Client
	cfg    PoolConfig
}

// ErrPoolExhausted indicates no warm container can be leased or created.
var ErrPoolExhausted = errors.New("pool exhausted")

// ErrLocked indicates a warm container is already leased.
var ErrLocked = errors.New("container is locked")

// ContainerStatus describes one pooled container for status reporting.
type ContainerStatus struct {
	ContainerID   string
	Image         string
	KeyHash       string
	State         string
	Locked        bool
	CreatedAt     time.Time
	LastUsed      time.Time
	WorkspaceRoot string
	IdleFor       time.Duration
}

// NewWarmPool creates a warm pool instance and ensures pool directory exists.
func NewWarmPool(cli *client.Client, cfg PoolConfig) *WarmPool {
	if cfg.MaxPerKey <= 0 {
		cfg.MaxPerKey = 1
	}
	if cfg.MaxTotal <= 0 {
		cfg.MaxTotal = 5
	}
	if cfg.MaxTotal < cfg.MaxPerKey {
		cfg.MaxTotal = cfg.MaxPerKey
	}
	if cfg.TTL <= 0 {
		cfg.TTL = 10 * time.Minute
	}
	if cfg.PoolDir == "" {
		cfg.PoolDir = filepath.Join(os.TempDir(), "tuprwre", "containers", "pool")
	}

	_ = os.MkdirAll(cfg.PoolDir, 0o755)

	return &WarmPool{
		client: cli,
		cfg:    cfg,
	}
}

// Acquire acquires an exclusive lease for a warm container matching key.
func (p *WarmPool) Acquire(ctx context.Context, key PoolKey) (*Lease, error) {
	if p == nil || p.client == nil {
		return nil, fmt.Errorf("warm pool is not initialized")
	}

	keyHash := key.Hash()

	if lease, containers, err := p.findAvailableLease(ctx, keyHash); err != nil {
		return nil, err
	} else if lease != nil {
		return lease, nil
	} else if len(containers) < p.cfg.MaxPerKey {
		return p.createAndLease(ctx, key, keyHash)
	}

	total, err := p.totalPoolContainers(ctx)
	if err != nil {
		return nil, err
	}

	if total >= p.cfg.MaxTotal {
		for total >= p.cfg.MaxTotal {
			before := total
			if err := p.evictOldest(ctx); err != nil {
				break
			}

			total, err = p.totalPoolContainers(ctx)
			if err != nil {
				return nil, err
			}
			if total >= before {
				break
			}
		}
	}

	if total >= p.cfg.MaxTotal {
		return nil, ErrPoolExhausted
	}

	containers, err := p.containersByKey(ctx, keyHash)
	if err != nil {
		return nil, err
	}
	if len(containers) >= p.cfg.MaxPerKey {
		return nil, ErrPoolExhausted
	}

	return p.createAndLease(ctx, key, keyHash)
}

// Release releases a previously acquired lease.
// If the lease is marked unhealthy, the underlying container is removed.
func (p *WarmPool) Release(ctx context.Context, lease *Lease) {
	if lease == nil {
		return
	}

	if lease.unhealthy {
		_ = p.removeContainerAndArtifacts(ctx, lease.ContainerID)
		if lease.lockFile != nil {
			_ = lease.lockFile.Close()
			lease.lockFile = nil
		}
		return
	}

	lease.Release()
}

func (p *WarmPool) findAvailableLease(ctx context.Context, keyHash string) (*Lease, []container.Summary, error) {
	containers, err := p.containersByKey(ctx, keyHash)
	if err != nil {
		return nil, nil, err
	}

	for _, c := range containers {
		if c.State != "running" {
			continue
		}

		lockFile, err := tryLock(p.cfg.PoolDir, c.ID)
		if err != nil {
			if errors.Is(err, ErrLocked) {
				continue
			}
			return nil, nil, err
		}

		inspect, err := p.client.ContainerInspect(ctx, c.ID)
		if err != nil || inspect.State == nil || !inspect.State.Running {
			_ = lockFile.Close()
			_ = p.removeContainerAndArtifacts(context.Background(), c.ID)
			continue
		}

		return &Lease{
			ContainerID: c.ID,
			lockFile:    lockFile,
			metaPath:    metaPath(p.cfg.PoolDir, c.ID),
		}, containers, nil
	}

	return nil, containers, nil
}

func (p *WarmPool) createAndLease(ctx context.Context, key PoolKey, keyHash string) (*Lease, error) {
	id, err := p.createWarmContainer(ctx, key)
	if err != nil {
		return nil, err
	}

	lockFile, err := tryLock(p.cfg.PoolDir, id)
	if err != nil {
		_ = p.removeContainerAndArtifacts(context.Background(), id)
		if errors.Is(err, ErrLocked) {
			return nil, ErrPoolExhausted
		}
		return nil, err
	}

	now := time.Now().UTC()
	meta := ContainerMeta{
		KeyHash:       keyHash,
		Image:         key.Image,
		CreatedAt:     now,
		LastUsed:      now,
		WorkspaceRoot: workspaceRootFromBinds(key.Binds),
	}
	if err := writeContainerMeta(metaPath(p.cfg.PoolDir, id), meta); err != nil {
		_ = lockFile.Close()
		_ = p.removeContainerAndArtifacts(context.Background(), id)
		return nil, err
	}

	return &Lease{
		ContainerID: id,
		lockFile:    lockFile,
		metaPath:    metaPath(p.cfg.PoolDir, id),
	}, nil
}

// createWarmContainer creates and starts one warm container for key.
func (p *WarmPool) createWarmContainer(ctx context.Context, key PoolKey) (string, error) {
	containerConfig := &container.Config{
		Image:           key.Image,
		Cmd:             []string{"sleep", "infinity"},
		Tty:             false,
		NetworkDisabled: key.NoNetwork,
		User:            key.User,
		Labels:          key.Labels(),
	}

	hostConfig := &container.HostConfig{
		ReadonlyRootfs: true,
		Tmpfs: map[string]string{
			"/tmp": "size=64m,noexec",
		},
		Binds: key.Binds,
	}

	if key.Memory > 0 {
		hostConfig.Resources.Memory = key.Memory
	}
	if key.CPUs > 0 {
		hostConfig.Resources.NanoCPUs = int64(key.CPUs * 1e9)
	}

	resp, err := p.client.ContainerCreate(ctx, containerConfig, hostConfig, nil, nil, "")
	if err != nil {
		return "", fmt.Errorf("failed to create warm container: %w", err)
	}

	if err := p.client.ContainerStart(ctx, resp.ID, container.StartOptions{}); err != nil {
		_ = p.client.ContainerRemove(context.Background(), resp.ID, container.RemoveOptions{Force: true, RemoveVolumes: true})
		return "", fmt.Errorf("failed to start warm container: %w", err)
	}

	return resp.ID, nil
}

// evictOldest evicts the oldest idle unlocked pooled container.
func (p *WarmPool) evictOldest(ctx context.Context) error {
	all, err := p.allPoolContainers(ctx)
	if err != nil {
		return err
	}

	type candidate struct {
		id       string
		lastUsed time.Time
		lockFile *os.File
	}

	candidates := make([]candidate, 0, len(all))

	for _, c := range all {
		if c.State == "exited" || c.State == "dead" {
			_ = p.removeContainerAndArtifacts(ctx, c.ID)
			continue
		}

		lockFile, err := tryLock(p.cfg.PoolDir, c.ID)
		if err != nil {
			if errors.Is(err, ErrLocked) {
				continue
			}
			continue
		}

		meta, err := readContainerMeta(metaPath(p.cfg.PoolDir, c.ID))
		lastUsed := time.Time{}
		if err == nil {
			lastUsed = meta.LastUsed
		}

		candidates = append(candidates, candidate{id: c.ID, lastUsed: lastUsed, lockFile: lockFile})
	}

	if len(candidates) == 0 {
		return nil
	}

	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].lastUsed.Before(candidates[j].lastUsed)
	})

	victim := candidates[0]
	_ = p.removeContainerAndArtifacts(ctx, victim.id)

	for _, c := range candidates {
		_ = c.lockFile.Close()
	}

	return nil
}

// GC removes dead/exited containers and TTL-expired idle containers.
func (p *WarmPool) GC(ctx context.Context) (int, error) {
	all, err := p.allPoolContainers(ctx)
	if err != nil {
		return 0, err
	}

	removed := 0
	now := time.Now().UTC()

	for _, c := range all {
		if c.State == "exited" || c.State == "dead" {
			if err := p.removeContainerAndArtifacts(ctx, c.ID); err == nil {
				removed++
			}
			continue
		}

		lockFile, err := tryLock(p.cfg.PoolDir, c.ID)
		if err != nil {
			if errors.Is(err, ErrLocked) {
				continue
			}
			continue
		}

		meta, err := readContainerMeta(metaPath(p.cfg.PoolDir, c.ID))
		if err != nil {
			_ = lockFile.Close()
			continue
		}

		if !meta.LastUsed.IsZero() && now.Sub(meta.LastUsed) > p.cfg.TTL {
			if err := p.removeContainerAndArtifacts(ctx, c.ID); err == nil {
				removed++
			}
		}

		_ = lockFile.Close()
	}

	return removed, nil
}

// Status returns warm pool container status entries.
func (p *WarmPool) Status(ctx context.Context) ([]ContainerStatus, error) {
	all, err := p.allPoolContainers(ctx)
	if err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	out := make([]ContainerStatus, 0, len(all))

	for _, c := range all {
		st := ContainerStatus{
			ContainerID: c.ID,
			Image:       c.Image,
			State:       c.State,
		}

		meta, err := readContainerMeta(metaPath(p.cfg.PoolDir, c.ID))
		if err == nil {
			st.KeyHash = meta.KeyHash
			st.CreatedAt = meta.CreatedAt
			st.LastUsed = meta.LastUsed
			st.WorkspaceRoot = meta.WorkspaceRoot
			if !meta.LastUsed.IsZero() {
				st.IdleFor = now.Sub(meta.LastUsed)
			}
		}

		lockFile, err := tryLock(p.cfg.PoolDir, c.ID)
		if err != nil {
			if errors.Is(err, ErrLocked) {
				st.Locked = true
			}
		} else {
			st.Locked = false
			_ = lockFile.Close()
		}

		out = append(out, st)
	}

	sort.Slice(out, func(i, j int) bool {
		return out[i].ContainerID < out[j].ContainerID
	})

	return out, nil
}

func (p *WarmPool) containersByKey(ctx context.Context, keyHash string) ([]container.Summary, error) {
	args := filters.NewArgs(filters.Arg("label", "tuprwre.pool.key="+keyHash))
	containers, err := p.client.ContainerList(ctx, container.ListOptions{All: true, Filters: args})
	if err != nil {
		return nil, fmt.Errorf("failed to list pool containers by key: %w", err)
	}
	return containers, nil
}

func (p *WarmPool) allPoolContainers(ctx context.Context) ([]container.Summary, error) {
	args := filters.NewArgs(filters.Arg("label", "tuprwre.pool=true"))
	containers, err := p.client.ContainerList(ctx, container.ListOptions{All: true, Filters: args})
	if err != nil {
		return nil, fmt.Errorf("failed to list pool containers: %w", err)
	}
	return containers, nil
}

func (p *WarmPool) totalPoolContainers(ctx context.Context) (int, error) {
	all, err := p.allPoolContainers(ctx)
	if err != nil {
		return 0, err
	}
	return len(all), nil
}

func (p *WarmPool) removeContainerAndArtifacts(ctx context.Context, containerID string) error {
	_ = p.client.ContainerRemove(ctx, containerID, container.RemoveOptions{Force: true, RemoveVolumes: true})
	_ = os.Remove(lockPath(p.cfg.PoolDir, containerID))
	_ = os.Remove(metaPath(p.cfg.PoolDir, containerID))
	return nil
}

func workspaceRootFromBinds(binds []string) string {
	if len(binds) == 0 {
		return ""
	}

	bind := binds[0]
	for i := 0; i < len(bind); i++ {
		if bind[i] == ':' {
			return CanonicalizePath(bind[:i])
		}
	}

	return CanonicalizePath(bind)
}
