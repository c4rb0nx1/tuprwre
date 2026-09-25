package sensor

import (
	"testing"

	"github.com/c4rb0nx1/tuprwre/internal/event"
	"github.com/c4rb0nx1/tuprwre/internal/gateway"
)

func proc(kind event.Kind, pid, ppid int, execID, parentExecID string) event.Event {
	e := event.New(event.SourceSensor, kind, fixedTime)
	e.Sensor = "t"
	e.Process = &event.Process{PID: pid, PPID: ppid, ExecID: execID, ParentExecID: parentExecID}
	return e
}

func ids(evs []event.Event) []int {
	var out []int
	for _, e := range evs {
		out = append(out, e.Process.PID)
	}
	return out
}

func TestSubtreeFilterFollowsDescendants(t *testing.T) {
	sink := gateway.NewMemorySink()
	f := NewSubtreeFilter(100, sink)
	in := []event.Event{
		proc(event.KindExec, 100, 1, "e100", "e1"),     // root
		proc(event.KindExec, 200, 1, "e200", "e1"),     // unrelated sibling
		proc(event.KindExec, 101, 100, "e101", "e100"), // child
		proc(event.KindExec, 102, 101, "e102", "e101"), // grandchild
		proc(event.KindFileWrite, 102, 101, "e102", "e101"),
		proc(event.KindExec, 201, 200, "e201", "e200"), // unrelated child
		proc(event.KindNetConnect, 201, 200, "e201", "e200"),
	}
	for _, e := range in {
		if err := f.Emit(e); err != nil {
			t.Fatal(err)
		}
	}
	got := ids(sink.Events())
	want := []int{100, 101, 102, 102}
	if len(got) != len(want) {
		t.Fatalf("forwarded pids %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("forwarded pids %v, want %v", got, want)
		}
	}
	if f.Dropped() != 3 {
		t.Errorf("dropped = %d, want 3", f.Dropped())
	}
}

// TestSubtreeFilterRootPredatesSensor covers a root whose exec was never
// seen: its children are adopted by parent pid.
func TestSubtreeFilterRootPredatesSensor(t *testing.T) {
	sink := gateway.NewMemorySink()
	f := NewSubtreeFilter(100, sink)
	_ = f.Emit(proc(event.KindExec, 101, 100, "e101", "e100"))
	_ = f.Emit(proc(event.KindExec, 102, 101, "e102", "e101"))
	_ = f.Emit(proc(event.KindExec, 300, 7, "e300", "e7"))
	if got := ids(sink.Events()); len(got) != 2 || got[0] != 101 || got[1] != 102 {
		t.Errorf("forwarded pids %v", got)
	}
}

// TestSubtreeFilterPIDReuse proves the root pid stops matching after the
// root exits, and pid-only members are forgotten on exit.
func TestSubtreeFilterPIDReuse(t *testing.T) {
	sink := gateway.NewMemorySink()
	f := NewSubtreeFilter(100, sink)
	_ = f.Emit(proc(event.KindExec, 100, 1, "", ""))
	_ = f.Emit(proc(event.KindProcExit, 100, 1, "", ""))
	_ = f.Emit(proc(event.KindExec, 100, 1, "", ""))   // reused pid
	_ = f.Emit(proc(event.KindExec, 150, 100, "", "")) // child of the reused pid
	if n := len(sink.Events()); n != 2 {
		t.Errorf("forwarded %d events, want the original exec and exit only", n)
	}
}

func TestProcessAndParentKey(t *testing.T) {
	if ProcessKey(&event.Process{PID: 3, ExecID: "x"}) != "x" || ProcessKey(&event.Process{PID: 3}) != "pid:3" {
		t.Error("ProcessKey")
	}
	if ParentKey(&event.Process{PPID: 2, ParentExecID: "p"}) != "p" || ParentKey(&event.Process{PPID: 2}) != "pid:2" ||
		ParentKey(&event.Process{}) != "" {
		t.Error("ParentKey")
	}
}
