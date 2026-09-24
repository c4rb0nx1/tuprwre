package sensor

import (
	"errors"
	"fmt"
	"net/netip"
	"path/filepath"

	"github.com/c4rb0nx1/tuprwre/internal/event"
)

// EffectKinds lists the event kinds a sensor may emit.
var EffectKinds = []event.Kind{
	event.KindExec,
	event.KindFileWrite,
	event.KindFileReadSensitive,
	event.KindNetConnect,
	event.KindProcExit,
}

// IsEffect reports whether k is an effect kind.
func IsEffect(k event.Kind) bool {
	for _, e := range EffectKinds {
		if k == e {
			return true
		}
	}
	return false
}

// Validate checks e against the effect-event contract every adapter must
// satisfy and every downstream consumer may rely on:
//
//   - Version is event.SchemaVersion; ID, Time and Sensor are set; Source is
//     event.SourceSensor and Kind is an effect kind.
//   - Process is set with a positive PID.
//   - exec carries an absolute Process.Binary.
//   - file_write and file_read_sensitive carry File with an absolute Path.
//   - net_connect carries Net with protocol "tcp" or "udp", an IP DstAddr and
//     a DstPort in 1..65535 (and a parseable SrcAddr, SrcPort in range, when
//     present).
//   - proc_exit carries Exit with a Code or a Signal.
//   - Kind-specific payloads appear only on their own Kind, and gateway-only
//     fields (protocol, tool call, arguments, result) are absent.
//
// It returns an error naming the first violation found.
func Validate(e event.Event) error {
	switch {
	case e.Version != event.SchemaVersion:
		return fmt.Errorf("version %q, want %q", e.Version, event.SchemaVersion)
	case e.ID == "":
		return errors.New("missing id")
	case e.Time.IsZero():
		return errors.New("missing time")
	case e.Source != event.SourceSensor:
		return fmt.Errorf("source %q, want %q", e.Source, event.SourceSensor)
	case !IsEffect(e.Kind):
		return fmt.Errorf("kind %q is not an effect kind", e.Kind)
	case e.Sensor == "":
		return errors.New("missing sensor name")
	case e.Protocol != "" || e.ToolCallID != "" || e.ToolName != "" ||
		len(e.Arguments) > 0 || len(e.Result) > 0 ||
		e.Complete || e.Truncated || e.IsError:
		return errors.New("gateway-only field set on effect event")
	case e.Process == nil:
		return errors.New("missing process")
	case e.Process.PID <= 0:
		return fmt.Errorf("process pid %d, want > 0", e.Process.PID)
	case e.Process.PPID < 0:
		return fmt.Errorf("process ppid %d, want >= 0", e.Process.PPID)
	}

	isFile := e.Kind == event.KindFileWrite || e.Kind == event.KindFileReadSensitive
	switch {
	case e.File != nil && !isFile:
		return fmt.Errorf("file payload on %s event", e.Kind)
	case e.Net != nil && e.Kind != event.KindNetConnect:
		return fmt.Errorf("net payload on %s event", e.Kind)
	case e.Exit != nil && e.Kind != event.KindProcExit:
		return fmt.Errorf("exit payload on %s event", e.Kind)
	}

	switch e.Kind {
	case event.KindExec:
		if !filepath.IsAbs(e.Process.Binary) {
			return fmt.Errorf("exec binary %q is not an absolute path", e.Process.Binary)
		}
	case event.KindFileWrite, event.KindFileReadSensitive:
		if e.File == nil {
			return fmt.Errorf("missing file payload on %s event", e.Kind)
		}
		if !filepath.IsAbs(e.File.Path) {
			return fmt.Errorf("file path %q is not an absolute path", e.File.Path)
		}
	case event.KindNetConnect:
		return validateNet(e.Net)
	case event.KindProcExit:
		if e.Exit == nil {
			return errors.New("missing exit payload on proc_exit event")
		}
		if e.Exit.Code == nil && e.Exit.Signal == "" {
			return errors.New("exit payload has neither code nor signal")
		}
	}
	return nil
}

func validateNet(n *event.Net) error {
	if n == nil {
		return errors.New("missing net payload on net_connect event")
	}
	if n.Protocol != "tcp" && n.Protocol != "udp" {
		return fmt.Errorf("net protocol %q, want tcp or udp", n.Protocol)
	}
	if _, err := netip.ParseAddr(n.DstAddr); err != nil {
		return fmt.Errorf("net dst_addr %q is not an IP address", n.DstAddr)
	}
	if n.DstPort < 1 || n.DstPort > 65535 {
		return fmt.Errorf("net dst_port %d out of range", n.DstPort)
	}
	if n.SrcAddr != "" {
		if _, err := netip.ParseAddr(n.SrcAddr); err != nil {
			return fmt.Errorf("net src_addr %q is not an IP address", n.SrcAddr)
		}
	}
	if n.SrcPort < 0 || n.SrcPort > 65535 {
		return fmt.Errorf("net src_port %d out of range", n.SrcPort)
	}
	return nil
}
