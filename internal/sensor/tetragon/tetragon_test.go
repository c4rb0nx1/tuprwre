package tetragon

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/c4rb0nx1/tuprwre/internal/event"
	"github.com/c4rb0nx1/tuprwre/internal/gateway"
	"github.com/c4rb0nx1/tuprwre/internal/sensor"
)

var update = flag.Bool("update", false, "rewrite golden files in testdata")

// record runs the adapter over input through the full sensor.Record pipeline
// (validation + default redaction).
func record(t *testing.T, input string, opts Options) (*Sensor, sensor.Stats, []event.Event) {
	t.Helper()
	s := New(strings.NewReader(input), opts)
	sink := gateway.NewMemorySink()
	st, err := sensor.Record(context.Background(), s, sink, sensor.Options{})
	if err != nil {
		t.Fatalf("Record: %v", err)
	}
	return s, st, sink.Events()
}

// TestSessionGolden translates the synthetic session export and compares the
// normalized, redacted output with the golden file. Run with -update to
// rewrite it after an intentional mapping change.
func TestSessionGolden(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "session.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	s, st, events := record(t, string(raw), Options{SessionID: "sess-tg-1"})

	// 28 native lines: 1 procFS exec, 1 process_loader and 1 libc read are
	// ignored; the rest map to effects.
	if st.EventsEmitted != 25 || st.EventsInvalid != 0 || s.Errors() != 0 || s.Ignored() != 3 {
		t.Fatalf("stats = %+v errors = %d ignored = %d", st, s.Errors(), s.Ignored())
	}

	var got bytes.Buffer
	enc := json.NewEncoder(&got)
	for _, e := range events {
		if err := enc.Encode(e); err != nil {
			t.Fatal(err)
		}
	}
	golden := filepath.Join("testdata", "session.expected.jsonl")
	if *update {
		if err := os.WriteFile(golden, got.Bytes(), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatalf("read golden (run with -update to create): %v", err)
	}
	if !bytes.Equal(got.Bytes(), want) {
		t.Errorf("output differs from %s (run with -update after an intentional change)\n got:\n%s", golden, got.Bytes())
	}
	if bytes.Contains(got.Bytes(), []byte("fake-token-0123456789")) {
		t.Error("bearer token survived redaction")
	}
}

// TestIDsAreDeterministic proves re-reading the same export yields the same
// event IDs, so consumers can deduplicate replays.
func TestIDsAreDeterministic(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "session.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	_, _, a := record(t, string(raw), Options{})
	_, _, b := record(t, string(raw), Options{})
	seen := map[string]bool{}
	for i := range a {
		if a[i].ID != b[i].ID {
			t.Fatalf("event %d id %q != %q", i, a[i].ID, b[i].ID)
		}
		if seen[a[i].ID] {
			t.Fatalf("duplicate id %q", a[i].ID)
		}
		seen[a[i].ID] = true
	}
}

const proc = `"process":{"exec_id":"ZXg6MQ==","pid":77,"uid":0,"cwd":"/srv","binary":"/usr/bin/tool","arguments":"a b","flags":"execve"},"parent":{"exec_id":"ZXg6MA==","pid":1}`

func one(t *testing.T, line string) (*Sensor, []event.Event) {
	t.Helper()
	s, _, events := record(t, line+"\n", Options{})
	return s, events
}

func TestExecMapping(t *testing.T) {
	_, ev := one(t, `{"process_exec":{`+proc+`},"time":"2026-09-21T09:00:00.123456789Z"}`)
	if len(ev) != 1 {
		t.Fatalf("got %d events", len(ev))
	}
	p := ev[0].Process
	if ev[0].Kind != event.KindExec || p.PID != 77 || p.PPID != 1 || p.ExecID != "ZXg6MQ==" ||
		p.ParentExecID != "ZXg6MA==" || p.Cwd != "/srv" || p.UID == nil || *p.UID != 0 {
		t.Errorf("process = %+v", p)
	}
	if strings.Join(p.Argv, " ") != "/usr/bin/tool a b" {
		t.Errorf("argv = %q", p.Argv)
	}
	if ev[0].Time.Nanosecond() != 123456789 {
		t.Errorf("time = %v", ev[0].Time)
	}
}

func TestExecNoCwdFlag(t *testing.T) {
	line := strings.Replace(`{"process_exec":{`+proc+`},"time":"2026-09-21T09:00:00Z"}`, `"flags":"execve"`, `"flags":"execve nocwd"`, 1)
	_, ev := one(t, line)
	if len(ev) != 1 || ev[0].Process.Cwd != "" {
		t.Errorf("events = %+v", ev)
	}
}

func TestExitMapping(t *testing.T) {
	cases := []struct {
		extra  string
		code   *int
		signal string
	}{
		{``, intp(0), ""},
		{`,"status":2`, intp(2), ""},
		{`,"signal":"SIGTERM","status":15`, nil, "SIGTERM"},
	}
	for _, c := range cases {
		_, ev := one(t, `{"process_exit":{`+proc+c.extra+`},"time":"2026-09-21T09:00:00Z"}`)
		if len(ev) != 1 || ev[0].Kind != event.KindProcExit {
			t.Fatalf("%s: events = %+v", c.extra, ev)
		}
		x := ev[0].Exit
		if x.Signal != c.signal || (c.code == nil) != (x.Code == nil) || (c.code != nil && *c.code != *x.Code) {
			t.Errorf("%s: exit = %+v", c.extra, x)
		}
	}
}

func TestExitFallsBackToInnerTime(t *testing.T) {
	_, ev := one(t, `{"process_exit":{`+proc+`,"time":"2026-09-21T09:00:07Z"}}`)
	if len(ev) != 1 || ev[0].Time.Second() != 7 {
		t.Errorf("events = %+v", ev)
	}
}

func TestNetMapping(t *testing.T) {
	cases := []struct {
		fn, arg string
		want    event.Net
	}{
		{"tcp_connect", `{"sock_arg":{"protocol":"IPPROTO_TCP","saddr":"10.0.0.1","daddr":"2001:db8::1","sport":5000,"dport":443}}`,
			event.Net{Protocol: "tcp", SrcAddr: "10.0.0.1", SrcPort: 5000, DstAddr: "2001:db8::1", DstPort: 443}},
		{"ip4_datagram_connect", `{"sock_arg":{"protocol":"IPPROTO_UDP","daddr":"192.0.2.1","dport":53}}`,
			event.Net{Protocol: "udp", DstAddr: "192.0.2.1", DstPort: 53}},
		{"tcp_v4_connect", `{"sock_arg":{"daddr":"192.0.2.2","dport":22}},{"sockaddr_arg":{"addr":"192.0.2.9","port":1}}`,
			event.Net{Protocol: "tcp", DstAddr: "192.0.2.2", DstPort: 22}},
		{"tcp_v4_connect", `{"sockaddr_arg":{"family":"AF_INET","addr":"192.0.2.3","port":8080}}`,
			event.Net{Protocol: "tcp", DstAddr: "192.0.2.3", DstPort: 8080}},
	}
	for _, c := range cases {
		_, ev := one(t, `{"process_kprobe":{`+proc+`,"function_name":"`+c.fn+`","args":[`+c.arg+`]},"time":"2026-09-21T09:00:00Z"}`)
		if len(ev) != 1 || ev[0].Kind != event.KindNetConnect || *ev[0].Net != c.want {
			t.Errorf("%s: events = %+v", c.fn, ev)
			continue
		}
	}
}

func TestFileMapping(t *testing.T) {
	cases := []struct {
		fn, args string
		kind     event.Kind // "" means ignored
	}{
		{"security_file_permission", `{"file_arg":{"path":"/srv/out.txt"}},{"int_arg":2}`, event.KindFileWrite},
		{"security_file_permission", `{"file_arg":{"path":"/srv/out.txt"}},{"int_arg":6}`, event.KindFileWrite},
		{"security_file_permission", `{"file_arg":{"path":"/root/.ssh/id_ed25519"}},{"int_arg":4}`, event.KindFileReadSensitive},
		{"security_file_permission", `{"file_arg":{"path":"/srv/readme"}},{"int_arg":4}`, ""},
		{"security_file_permission", `{"file_arg":{"path":"/srv/readme"}},{"int_arg":1}`, ""},
		{"security_path_truncate", `{"path_arg":{"path":"/srv/log"}}`, event.KindFileWrite},
		{"security_file_open", `{"file_arg":{"path":"/srv/.env"}}`, event.KindFileReadSensitive},
		{"fd_install", `{"int_arg":3},{"file_arg":{"path":"/home/a/.kube/config"}}`, event.KindFileReadSensitive},
		{"security_mmap_file", `{"file_arg":{"path":"/srv/x"}}`, ""},
	}
	for _, c := range cases {
		s, ev := one(t, `{"process_kprobe":{`+proc+`,"function_name":"`+c.fn+`","args":[`+c.args+`]},"time":"2026-09-21T09:00:00Z"}`)
		if c.kind == "" {
			if len(ev) != 0 || s.Ignored() != 1 {
				t.Errorf("%s %s: want ignored, events = %+v", c.fn, c.args, ev)
			}
			continue
		}
		if len(ev) != 1 || ev[0].Kind != c.kind || ev[0].File == nil {
			t.Errorf("%s %s: events = %+v", c.fn, c.args, ev)
		}
	}
}

// TestMalformedAndIncompleteRecords proves bad native records are counted as
// errors, never emitted and never fatal.
func TestMalformedAndIncompleteRecords(t *testing.T) {
	lines := []string{
		`not json`,
		`{"process_exec":{` + proc + `}}`, // no time
		`{"process_exec":{` + proc + `},"time":"yesterday"}`,                         // bad time
		`{"process_exec":{"process":{"binary":"/x"}},"time":"2026-09-21T09:00:00Z"}`, // no pid
		`{"process_kprobe":{` + proc + `,"function_name":"tcp_connect","args":[{"sock_arg":{"protocol":"IPPROTO_SCTP","daddr":"192.0.2.1","dport":1}}]},"time":"2026-09-21T09:00:00Z"}`,
		`{"process_kprobe":{` + proc + `,"function_name":"security_file_permission","args":[{"file_arg":{"path":"/x"}}]},"time":"2026-09-21T09:00:00Z"}`, // no mask
		`{"process_kprobe":{` + proc + `,"function_name":"security_path_truncate","args":[]},"time":"2026-09-21T09:00:00Z"}`,                             // no path
	}
	s, st, ev := record(t, strings.Join(lines, "\n"), Options{})
	if len(ev) != 0 || s.Errors() != len(lines) || st.EventsInvalid != 0 {
		t.Errorf("events = %d errors = %d stats = %+v", len(ev), s.Errors(), st)
	}
}

// TestContractRejectsRelativePath proves a translation that violates the
// effect contract is dropped by sensor.Record rather than persisted.
func TestContractRejectsRelativePath(t *testing.T) {
	_, st, ev := record(t, `{"process_kprobe":{`+proc+`,"function_name":"security_path_truncate","args":[{"path_arg":{"path":"rel/log"}}]},"time":"2026-09-21T09:00:00Z"}`, Options{})
	if len(ev) != 0 || st.EventsInvalid != 1 {
		t.Errorf("events = %+v stats = %+v", ev, st)
	}
}

func TestRunStopsOnCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := New(strings.NewReader(`{"process_exec":{`+proc+`},"time":"2026-09-21T09:00:00Z"}`), Options{}).Run(ctx, gateway.NewMemorySink())
	if err != context.Canceled {
		t.Errorf("err = %v", err)
	}
}

func intp(i int) *int { return &i }
