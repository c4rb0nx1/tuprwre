// Package tetragon adapts Cilium Tetragon's JSON event export to the tprsh
// effect-event schema.
//
// Tetragon writes one JSON object per line (GetEventsResponse, protojson with
// snake_case names) both to its export file and from `tetra getevents -o json`.
// This adapter reads that stream and translates:
//
//   - process_exec   -> exec
//   - process_exit   -> proc_exit
//   - process_kprobe -> net_connect   (connect hooks carrying a sock/sockaddr arg)
//   - process_kprobe -> file_write    (security_file_permission with MAY_WRITE,
//     security_path_truncate)
//   - process_kprobe -> file_read_sensitive (security_file_permission with
//     MAY_READ, security_file_open, fd_install on a path IsSensitivePath accepts)
//
// Everything else is counted as ignored. This package is the only code that
// knows Tetragon's format; nothing downstream may depend on it.
//
// The mapping is written against Tetragon's documented export format and is
// tested with synthetic fixtures only; it has not been verified against a live
// Tetragon agent.
package tetragon

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"sync/atomic"
	"time"

	"github.com/c4rb0nx1/tuprwre/internal/event"
	"github.com/c4rb0nx1/tuprwre/internal/gateway"
	"github.com/c4rb0nx1/tuprwre/internal/sensor"
)

// Name is the adapter name stamped on every event.
const Name = "tetragon"

// MaxLineBytes bounds one Tetragon record. Longer lines are skipped and
// counted as errors.
const MaxLineBytes = 4 << 20 // 4 MiB

// Linux permission mask bits passed to security_file_permission.
const (
	mayWrite = 0x2
	mayRead  = 0x4
)

// Options configures the adapter.
type Options struct {
	// SessionID is stamped on every event, attributing the whole stream to
	// one harness session. Leave empty when the stream is not scoped to a
	// single session.
	SessionID string
}

// Sensor reads Tetragon JSON export lines from a reader. It implements
// sensor.Sensor.
type Sensor struct {
	r       io.Reader
	opts    Options
	errors  atomic.Int64
	ignored atomic.Int64
}

var _ sensor.Sensor = (*Sensor)(nil)

// New returns a Sensor reading Tetragon JSON export lines from r.
func New(r io.Reader, opts Options) *Sensor { return &Sensor{r: r, opts: opts} }

// Name implements sensor.Sensor.
func (*Sensor) Name() string { return Name }

// Errors returns the number of records that were malformed or lacked a field
// the translation requires (time, pid, path, address).
func (s *Sensor) Errors() int { return int(s.errors.Load()) }

// Ignored returns the number of well-formed records that map to no effect
// kind: other event types, hooks without a mapping, pre-existing processes
// discovered from /proc, and reads of non-sensitive files.
func (s *Sensor) Ignored() int { return int(s.ignored.Load()) }

// Run implements sensor.Sensor. Cancellation is checked between records.
func (s *Sensor) Run(ctx context.Context, out gateway.Sink) error {
	br := bufio.NewReaderSize(s.r, 64<<10)
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		line, tooLong, err := sensor.ReadLine(br, MaxLineBytes)
		switch {
		case tooLong:
			s.errors.Add(1)
		case len(bytes.TrimSpace(line)) > 0:
			ev, res := s.translate(line)
			switch res {
			case resultEvent:
				_ = out.Emit(ev)
			case resultIgnored:
				s.ignored.Add(1)
			case resultError:
				s.errors.Add(1)
			}
		}
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
	}
}

type result int

const (
	resultEvent result = iota
	resultIgnored
	resultError
)

// Native Tetragon shapes. Only the fields the mapping reads are declared.

type tgRecord struct {
	ProcessExec   *tgExec   `json:"process_exec"`
	ProcessExit   *tgExit   `json:"process_exit"`
	ProcessKprobe *tgKprobe `json:"process_kprobe"`
	Time          string    `json:"time"`
}

type tgProcess struct {
	ExecID       string  `json:"exec_id"`
	PID          *int    `json:"pid"`
	UID          *uint32 `json:"uid"`
	Cwd          string  `json:"cwd"`
	Binary       string  `json:"binary"`
	Arguments    string  `json:"arguments"`
	Flags        string  `json:"flags"`
	ParentExecID string  `json:"parent_exec_id"`
}

type tgExec struct {
	Process *tgProcess `json:"process"`
	Parent  *tgProcess `json:"parent"`
}

type tgExit struct {
	Process *tgProcess `json:"process"`
	Parent  *tgProcess `json:"parent"`
	Signal  string     `json:"signal"`
	Status  *int       `json:"status"`
	Time    string     `json:"time"`
}

type tgKprobe struct {
	Process      *tgProcess `json:"process"`
	Parent       *tgProcess `json:"parent"`
	FunctionName string     `json:"function_name"`
	Args         []tgArg    `json:"args"`
}

type tgArg struct {
	FileArg     *tgPath     `json:"file_arg"`
	PathArg     *tgPath     `json:"path_arg"`
	IntArg      *int64      `json:"int_arg"`
	SockArg     *tgSock     `json:"sock_arg"`
	SockaddrArg *tgSockaddr `json:"sockaddr_arg"`
}

type tgPath struct {
	Path string `json:"path"`
}

type tgSock struct {
	Protocol string `json:"protocol"`
	Saddr    string `json:"saddr"`
	Daddr    string `json:"daddr"`
	Sport    int    `json:"sport"`
	Dport    int    `json:"dport"`
}

type tgSockaddr struct {
	Addr string `json:"addr"`
	Port int    `json:"port"`
}

// translate maps one native line to an effect event.
func (s *Sensor) translate(line []byte) (event.Event, result) {
	var rec tgRecord
	if err := json.Unmarshal(line, &rec); err != nil {
		return event.Event{}, resultError
	}
	var (
		kind   event.Kind
		proc   *tgProcess
		parent *tgProcess
		ts     = rec.Time
		fill   func(*event.Event) result
	)
	switch {
	case rec.ProcessExec != nil:
		proc, parent = rec.ProcessExec.Process, rec.ProcessExec.Parent
		if proc != nil && hasFlag(proc.Flags, "procFS") {
			// Discovered from /proc at agent start: not an execution
			// observed during the run.
			return event.Event{}, resultIgnored
		}
		kind = event.KindExec
	case rec.ProcessExit != nil:
		x := rec.ProcessExit
		proc, parent = x.Process, x.Parent
		if ts == "" {
			ts = x.Time
		}
		kind = event.KindProcExit
		fill = func(e *event.Event) result {
			switch {
			case x.Signal != "":
				e.Exit = &event.Exit{Signal: x.Signal}
			case x.Status != nil:
				code := *x.Status
				e.Exit = &event.Exit{Code: &code}
			default:
				// protojson omits a zero status: a clean exit.
				code := 0
				e.Exit = &event.Exit{Code: &code}
			}
			return resultEvent
		}
	case rec.ProcessKprobe != nil:
		k := rec.ProcessKprobe
		proc, parent = k.Process, k.Parent
		var res result
		kind, fill, res = mapKprobe(k)
		if res != resultEvent {
			return event.Event{}, res
		}
	default:
		return event.Event{}, resultIgnored
	}

	if proc == nil || proc.PID == nil || *proc.PID <= 0 {
		return event.Event{}, resultError
	}
	at, err := time.Parse(time.RFC3339Nano, ts)
	if err != nil {
		return event.Event{}, resultError
	}

	e := event.New(event.SourceSensor, kind, at)
	// A content-derived ID makes re-reading the same export idempotent:
	// consumers can deduplicate on ID.
	sum := sha256.Sum256(bytes.TrimSpace(line))
	e.ID = "tg-" + hex.EncodeToString(sum[:16])
	e.SessionID = s.opts.SessionID
	e.Sensor = Name
	e.Process = convertProcess(proc, parent)
	if fill != nil {
		if res := fill(&e); res != resultEvent {
			return event.Event{}, res
		}
	}
	return e, resultEvent
}

// mapKprobe selects the effect kind for a kprobe event from its hook and
// arguments.
func mapKprobe(k *tgKprobe) (event.Kind, func(*event.Event) result, result) {
	fn := k.FunctionName
	if strings.Contains(fn, "connect") {
		for _, a := range k.Args {
			switch {
			case a.SockArg != nil:
				sa := a.SockArg
				proto := netProtocol(sa.Protocol, fn)
				if proto == "" || sa.Daddr == "" {
					return "", nil, resultError
				}
				return event.KindNetConnect, func(e *event.Event) result {
					e.Net = &event.Net{Protocol: proto, SrcAddr: sa.Saddr, SrcPort: sa.Sport, DstAddr: sa.Daddr, DstPort: sa.Dport}
					return resultEvent
				}, resultEvent
			case a.SockaddrArg != nil:
				sa := a.SockaddrArg
				proto := netProtocol("", fn)
				if proto == "" || sa.Addr == "" {
					return "", nil, resultError
				}
				return event.KindNetConnect, func(e *event.Event) result {
					e.Net = &event.Net{Protocol: proto, DstAddr: sa.Addr, DstPort: sa.Port}
					return resultEvent
				}, resultEvent
			}
		}
		return "", nil, resultIgnored
	}

	var (
		p     string
		write bool
	)
	switch fn {
	case "security_file_permission":
		mask, ok := intArg(k.Args)
		p = filePath(k.Args)
		switch {
		case !ok:
			return "", nil, resultError
		case mask&mayWrite != 0:
			write = true
		case mask&mayRead != 0:
		default:
			return "", nil, resultIgnored
		}
	case "security_path_truncate":
		p, write = filePath(k.Args), true
	case "security_file_open", "fd_install":
		p = filePath(k.Args)
	default:
		return "", nil, resultIgnored
	}
	if p == "" {
		return "", nil, resultError
	}
	kind := event.KindFileWrite
	if !write {
		if !sensor.IsSensitivePath(p) {
			return "", nil, resultIgnored
		}
		kind = event.KindFileReadSensitive
	}
	return kind, func(e *event.Event) result {
		e.File = &event.File{Path: p}
		return resultEvent
	}, resultEvent
}

// netProtocol maps a Tetragon socket protocol, or failing that the hook name,
// to "tcp" or "udp".
func netProtocol(proto, fn string) string {
	switch {
	case proto == "IPPROTO_TCP":
		return "tcp"
	case proto == "IPPROTO_UDP":
		return "udp"
	case proto != "":
		return ""
	case strings.HasPrefix(fn, "tcp"):
		return "tcp"
	case strings.HasPrefix(fn, "udp") || strings.Contains(fn, "datagram"):
		return "udp"
	}
	return ""
}

func filePath(args []tgArg) string {
	for _, a := range args {
		switch {
		case a.FileArg != nil:
			return a.FileArg.Path
		case a.PathArg != nil:
			return a.PathArg.Path
		}
	}
	return ""
}

func intArg(args []tgArg) (int64, bool) {
	for _, a := range args {
		if a.IntArg != nil {
			return *a.IntArg, true
		}
	}
	return 0, false
}

// convertProcess maps a Tetragon process and its parent to the schema.
func convertProcess(p, parent *tgProcess) *event.Process {
	out := &event.Process{
		ExecID:       p.ExecID,
		ParentExecID: p.ParentExecID,
		PID:          *p.PID,
		UID:          p.UID,
		Binary:       p.Binary,
		Argv:         splitArguments(p.Binary, p.Arguments),
	}
	if !hasFlag(p.Flags, "nocwd") {
		out.Cwd = p.Cwd
	}
	if parent != nil {
		if parent.PID != nil {
			out.PPID = *parent.PID
		}
		if out.ParentExecID == "" {
			out.ParentExecID = parent.ExecID
		}
	}
	return out
}

// splitArguments rebuilds argv from Tetragon's binary path and its
// space-joined argument string. The export does not preserve argument
// boundaries, so an argument containing whitespace is split into several;
// argv[0] is the binary path, not the name the process was invoked as.
func splitArguments(binary, args string) []string {
	if binary == "" {
		return nil
	}
	return append([]string{binary}, strings.Fields(args)...)
}

// hasFlag reports whether Tetragon's space-separated flags string contains f.
func hasFlag(flags, f string) bool {
	for _, x := range strings.Fields(flags) {
		if x == f {
			return true
		}
	}
	return false
}
