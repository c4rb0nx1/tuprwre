package sensor

import (
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/c4rb0nx1/tuprwre/internal/event"
	"github.com/c4rb0nx1/tuprwre/internal/gateway"
)

// ProcessKey identifies the process behind an effect: its exec id, or its pid
// when the sensor supplies no exec id.
func ProcessKey(p *event.Process) string {
	if p.ExecID != "" {
		return p.ExecID
	}
	return fmt.Sprintf("pid:%d", p.PID)
}

// ParentKey identifies the parent process in the same key space as
// ProcessKey, or "" when the parent is unknown.
func ParentKey(p *event.Process) string {
	switch {
	case p.ParentExecID != "":
		return p.ParentExecID
	case p.PPID > 0:
		return fmt.Sprintf("pid:%d", p.PPID)
	}
	return ""
}

// SubtreeFilter is a gateway.Sink that forwards only effects of one process
// subtree: the process with a given pid and everything it starts. It lets one
// host-wide sensor stream be scoped to a single agent session.
//
// A process joins the subtree when its pid is the root pid, when its parent
// is already in the subtree, or when its parent pid is the root pid (which
// covers a root whose own exec happened before the sensor started). Once the
// root exits, its pid is no longer matched, so a later process that reuses
// the pid is not adopted. Processes are only adopted through events the
// filter sees, so a stream should start before or with the root's children.
type SubtreeFilter struct {
	rootPID int
	next    gateway.Sink

	mu         sync.Mutex
	members    map[string]bool
	rootExited bool

	dropped atomic.Int64
}

// NewSubtreeFilter returns a filter for the subtree rooted at rootPID that
// forwards matching events to next.
func NewSubtreeFilter(rootPID int, next gateway.Sink) *SubtreeFilter {
	return &SubtreeFilter{rootPID: rootPID, next: next, members: map[string]bool{}}
}

// Dropped returns the number of events outside the subtree.
func (f *SubtreeFilter) Dropped() int { return int(f.dropped.Load()) }

// Emit implements gateway.Sink.
func (f *SubtreeFilter) Emit(e event.Event) error {
	if !f.admit(e) {
		f.dropped.Add(1)
		return nil
	}
	return f.next.Emit(e)
}

func (f *SubtreeFilter) admit(e event.Event) bool {
	p := e.Process
	if p == nil {
		return false
	}
	key := ProcessKey(p)

	f.mu.Lock()
	defer f.mu.Unlock()
	in := f.members[key]
	if !in {
		isRoot := p.PID == f.rootPID && !f.rootExited
		childOfRoot := p.PPID == f.rootPID && !f.rootExited
		if pk := ParentKey(p); isRoot || childOfRoot || (pk != "" && f.members[pk]) {
			f.members[key] = true
			in = true
		}
	}
	if in && e.Kind == event.KindProcExit {
		if p.PID == f.rootPID {
			f.rootExited = true
		}
		// A pid-only key can be reused by an unrelated process later.
		if p.ExecID == "" {
			delete(f.members, key)
		}
	}
	return in
}
