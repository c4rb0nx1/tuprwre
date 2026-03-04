//go:build integration

package pool

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/client"
)

const (
	testImage    = "alpine:3.19"
	testImageAlt = "alpine:3.18"
)

func requireDockerClient(t *testing.T) *client.Client {
	t.Helper()

	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		t.Skipf("docker client unavailable: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := cli.Ping(ctx); err != nil {
		_ = cli.Close()
		t.Skipf("docker daemon unavailable: %v", err)
	}

	t.Cleanup(func() {
		_ = cli.Close()
	})

	return cli
}

func requireImageAvailable(t *testing.T, cli *client.Client, imageName string) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	if _, _, err := cli.ImageInspectWithRaw(ctx, imageName); err == nil {
		return
	}

	rc, err := cli.ImagePull(ctx, imageName, image.PullOptions{})
	if err != nil {
		t.Skipf("unable to pull test image %q: %v", imageName, err)
	}
	defer rc.Close()
	_, _ = io.Copy(io.Discard, rc)
}

func cleanupPoolContainers(t *testing.T, cli *client.Client) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	args := filters.NewArgs(filters.Arg("label", "tuprwre.pool=true"))
	containers, err := cli.ContainerList(ctx, container.ListOptions{All: true, Filters: args})
	if err != nil {
		t.Logf("warning: failed to list pool containers for cleanup: %v", err)
		return
	}

	for _, c := range containers {
		if err := cli.ContainerRemove(ctx, c.ID, container.RemoveOptions{Force: true, RemoveVolumes: true}); err != nil {
			t.Logf("warning: failed to cleanup container %s: %v", c.ID, err)
		}
	}
}

func containerExists(ctx context.Context, cli *client.Client, id string) bool {
	_, err := cli.ContainerInspect(ctx, id)
	return err == nil
}

func testKey(imageName string) PoolKey {
	return PoolKey{
		Image:   imageName,
		Runtime: "docker",
	}
}

func TestWarmPool_AcquireCreatesContainer(t *testing.T) {
	cli := requireDockerClient(t)
	requireImageAvailable(t, cli, testImage)
	cleanupPoolContainers(t, cli)
	t.Cleanup(func() { cleanupPoolContainers(t, cli) })

	wp := NewWarmPool(cli, PoolConfig{PoolDir: t.TempDir()})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	key := testKey(testImage)
	lease, err := wp.Acquire(ctx, key)
	if err != nil {
		t.Fatalf("Acquire() failed: %v", err)
	}
	if lease == nil || lease.ContainerID == "" {
		t.Fatalf("Acquire() returned invalid lease: %#v", lease)
	}
	t.Cleanup(func() { lease.Release() })

	inspect, err := cli.ContainerInspect(ctx, lease.ContainerID)
	if err != nil {
		t.Fatalf("ContainerInspect(%s) failed: %v", lease.ContainerID, err)
	}
	if inspect.State == nil || !inspect.State.Running {
		t.Fatalf("container %s is not running", lease.ContainerID)
	}

	if inspect.Config == nil {
		t.Fatalf("container config is nil")
	}
	if len(inspect.Config.Cmd) != 2 || inspect.Config.Cmd[0] != "sleep" || inspect.Config.Cmd[1] != "infinity" {
		t.Fatalf("container cmd = %v, want [sleep infinity]", inspect.Config.Cmd)
	}

	wantLabels := key.Labels()
	for k, want := range wantLabels {
		if got := inspect.Config.Labels[k]; got != want {
			t.Fatalf("label %q = %q, want %q", k, got, want)
		}
	}
}

func TestWarmPool_AcquireReusesContainer(t *testing.T) {
	cli := requireDockerClient(t)
	requireImageAvailable(t, cli, testImage)
	cleanupPoolContainers(t, cli)
	t.Cleanup(func() { cleanupPoolContainers(t, cli) })

	wp := NewWarmPool(cli, PoolConfig{PoolDir: t.TempDir()})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	key := testKey(testImage)
	lease1, err := wp.Acquire(ctx, key)
	if err != nil {
		t.Fatalf("first Acquire() failed: %v", err)
	}
	id1 := lease1.ContainerID
	lease1.Release()

	lease2, err := wp.Acquire(ctx, key)
	if err != nil {
		t.Fatalf("second Acquire() failed: %v", err)
	}
	t.Cleanup(func() { lease2.Release() })

	if id1 != lease2.ContainerID {
		t.Fatalf("expected reuse of container %s, got %s", id1, lease2.ContainerID)
	}
}

func TestWarmPool_AcquireDifferentKeyCreatesNew(t *testing.T) {
	cli := requireDockerClient(t)
	requireImageAvailable(t, cli, testImage)
	requireImageAvailable(t, cli, testImageAlt)
	cleanupPoolContainers(t, cli)
	t.Cleanup(func() { cleanupPoolContainers(t, cli) })

	wp := NewWarmPool(cli, PoolConfig{PoolDir: t.TempDir()})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	leaseA, err := wp.Acquire(ctx, testKey(testImage))
	if err != nil {
		t.Fatalf("Acquire() for key A failed: %v", err)
	}
	t.Cleanup(func() { leaseA.Release() })

	leaseB, err := wp.Acquire(ctx, testKey(testImageAlt))
	if err != nil {
		t.Fatalf("Acquire() for key B failed: %v", err)
	}
	t.Cleanup(func() { leaseB.Release() })

	if leaseA.ContainerID == leaseB.ContainerID {
		t.Fatalf("expected different container IDs for different keys, both were %s", leaseA.ContainerID)
	}
}

func TestWarmPool_MaxPerKeyExhausted(t *testing.T) {
	cli := requireDockerClient(t)
	requireImageAvailable(t, cli, testImage)
	cleanupPoolContainers(t, cli)
	t.Cleanup(func() { cleanupPoolContainers(t, cli) })

	wp := NewWarmPool(cli, PoolConfig{PoolDir: t.TempDir(), MaxPerKey: 1, MaxTotal: 5})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	lease, err := wp.Acquire(ctx, testKey(testImage))
	if err != nil {
		t.Fatalf("first Acquire() failed: %v", err)
	}
	t.Cleanup(func() { lease.Release() })

	lease2, err := wp.Acquire(ctx, testKey(testImage))
	if !errors.Is(err, ErrPoolExhausted) {
		t.Fatalf("second Acquire() error = %v, want %v", err, ErrPoolExhausted)
	}
	if lease2 != nil {
		t.Fatalf("second Acquire() lease = %#v, want nil", lease2)
	}
}

func TestWarmPool_GCRemovesExpiredContainers(t *testing.T) {
	cli := requireDockerClient(t)
	requireImageAvailable(t, cli, testImage)
	cleanupPoolContainers(t, cli)
	t.Cleanup(func() { cleanupPoolContainers(t, cli) })

	wp := NewWarmPool(cli, PoolConfig{PoolDir: t.TempDir(), TTL: 1 * time.Second})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	lease, err := wp.Acquire(ctx, testKey(testImage))
	if err != nil {
		t.Fatalf("Acquire() failed: %v", err)
	}
	id := lease.ContainerID
	lease.Release()

	time.Sleep(2 * time.Second)

	removed, err := wp.GC(ctx)
	if err != nil {
		t.Fatalf("GC() failed: %v", err)
	}
	if removed == 0 {
		t.Fatalf("GC() removed = %d, want at least 1", removed)
	}

	if containerExists(ctx, cli, id) {
		t.Fatalf("expected container %s to be removed by GC", id)
	}
}

func TestWarmPool_EvictsOldestOnMaxTotal(t *testing.T) {
	cli := requireDockerClient(t)
	requireImageAvailable(t, cli, testImage)
	requireImageAvailable(t, cli, testImageAlt)
	cleanupPoolContainers(t, cli)
	t.Cleanup(func() { cleanupPoolContainers(t, cli) })

	wp := NewWarmPool(cli, PoolConfig{PoolDir: t.TempDir(), MaxPerKey: 1, MaxTotal: 1, TTL: 10 * time.Minute})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	leaseA, err := wp.Acquire(ctx, testKey(testImage))
	if err != nil {
		t.Fatalf("Acquire() for key A failed: %v", err)
	}
	idA := leaseA.ContainerID
	leaseA.Release()

	leaseB, err := wp.Acquire(ctx, testKey(testImageAlt))
	if err != nil {
		t.Fatalf("Acquire() for key B failed: %v", err)
	}
	t.Cleanup(func() { leaseB.Release() })

	if leaseB.ContainerID == idA {
		t.Fatalf("expected a different container for key B, got reused %s", idA)
	}

	if containerExists(ctx, cli, idA) {
		t.Fatalf("expected oldest container %s to be evicted when MaxTotal is reached", idA)
	}
}

func TestWarmPool_LeaseReleaseCleansUp(t *testing.T) {
	cli := requireDockerClient(t)
	requireImageAvailable(t, cli, testImage)
	cleanupPoolContainers(t, cli)
	t.Cleanup(func() { cleanupPoolContainers(t, cli) })

	wp := NewWarmPool(cli, PoolConfig{PoolDir: t.TempDir()})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	lease, err := wp.Acquire(ctx, testKey(testImage))
	if err != nil {
		t.Fatalf("Acquire() failed: %v", err)
	}
	id := lease.ContainerID
	lease.MarkUnhealthy()
	lease.Release()

	if containerExists(ctx, cli, id) {
		t.Fatalf("expected unhealthy lease release to remove container %s", id)
	}
}
