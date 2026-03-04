package pool

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

// Lease is an exclusive lock on a warm container.
type Lease struct {
	ContainerID string
	lockFile    *os.File
	metaPath    string
	unhealthy   bool
}

// ContainerMeta stores per-container pool metadata.
type ContainerMeta struct {
	KeyHash       string    `json:"key_hash"`
	Image         string    `json:"image"`
	CreatedAt     time.Time `json:"created_at"`
	LastUsed      time.Time `json:"last_used"`
	WorkspaceRoot string    `json:"workspace_root"`
}

// Release updates metadata last_used and releases the container flock.
func (l *Lease) Release() {
	if l == nil {
		return
	}

	if l.metaPath != "" {
		if meta, err := readContainerMeta(l.metaPath); err == nil {
			meta.LastUsed = time.Now().UTC()
			_ = writeContainerMeta(l.metaPath, meta)
		}
	}

	if l.lockFile != nil {
		_ = l.lockFile.Close()
		l.lockFile = nil
	}
}

// MarkUnhealthy marks this lease as unhealthy.
func (l *Lease) MarkUnhealthy() {
	if l == nil {
		return
	}
	l.unhealthy = true
}

func readContainerMeta(path string) (ContainerMeta, error) {
	var meta ContainerMeta
	payload, err := os.ReadFile(path)
	if err != nil {
		return meta, err
	}
	if err := json.Unmarshal(payload, &meta); err != nil {
		return meta, fmt.Errorf("failed to unmarshal container metadata: %w", err)
	}
	return meta, nil
}

func writeContainerMeta(path string, meta ContainerMeta) error {
	payload, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal container metadata: %w", err)
	}
	payload = append(payload, '\n')

	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, payload, 0o644); err != nil {
		return fmt.Errorf("failed to write container metadata temp file: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("failed to atomically replace container metadata: %w", err)
	}
	return nil
}

func lockPath(poolDir, containerID string) string {
	return filepath.Join(poolDir, containerID+".lock")
}

func metaPath(poolDir, containerID string) string {
	return filepath.Join(poolDir, containerID+".json")
}

func tryLock(poolDir, containerID string) (*os.File, error) {
	if err := os.MkdirAll(poolDir, 0o755); err != nil {
		return nil, fmt.Errorf("failed to create pool directory: %w", err)
	}

	path := lockPath(poolDir, containerID)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, fmt.Errorf("failed to open lock file: %w", err)
	}

	err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	if err == nil {
		return f, nil
	}

	_ = f.Close()
	if errors.Is(err, syscall.EWOULDBLOCK) || errors.Is(err, syscall.EAGAIN) {
		return nil, ErrLocked
	}

	return nil, err
}
