package sensor

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/c4rb0nx1/tuprwre/internal/event"
)

const contractDir = "testdata/contract"

// decodeStrict decodes one fixture event, rejecting fields the schema does
// not know so a fixture cannot silently drift from the struct.
func decodeStrict(t *testing.T, raw []byte) event.Event {
	t.Helper()
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var e event.Event
	if err := dec.Decode(&e); err != nil {
		t.Fatalf("decode: %v\n%s", err, raw)
	}
	return e
}

// TestContractFixturesValid proves every valid fixture passes Validate and
// round-trips byte-for-byte through the event schema.
func TestContractFixturesValid(t *testing.T) {
	files, err := filepath.Glob(filepath.Join(contractDir, "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	sess, err := os.ReadFile(filepath.Join(contractDir, "session.jsonl"))
	if err != nil {
		t.Fatal(err)
	}

	type sample struct {
		name string
		raw  []byte
	}
	var samples []sample
	for _, f := range files {
		raw, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		samples = append(samples, sample{filepath.Base(f), raw})
	}
	for i, line := range bytes.Split(bytes.TrimSpace(sess), []byte("\n")) {
		samples = append(samples, sample{fmt.Sprintf("session.jsonl#%d", i+1), line})
	}
	if len(samples) < 6+10 {
		t.Fatalf("found %d samples; fixtures missing", len(samples))
	}

	seenKinds := map[event.Kind]bool{}
	for _, s := range samples {
		t.Run(s.name, func(t *testing.T) {
			e := decodeStrict(t, s.raw)
			if err := Validate(e); err != nil {
				t.Fatalf("Validate: %v", err)
			}
			seenKinds[e.Kind] = true

			got, err := json.Marshal(e)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			var want bytes.Buffer
			if err := json.Compact(&want, s.raw); err != nil {
				t.Fatalf("compact: %v", err)
			}
			if !bytes.Equal(got, want.Bytes()) {
				t.Errorf("round trip changed the record\n got: %s\nwant: %s", got, want.Bytes())
			}
		})
	}
	for _, k := range EffectKinds {
		if !seenKinds[k] {
			t.Errorf("no valid fixture for effect kind %q", k)
		}
	}
}

// TestContractFixturesInvalid proves each invalid fixture is rejected for the
// reason it was written to exercise.
func TestContractFixturesInvalid(t *testing.T) {
	want := map[string]string{
		"wrong_source.json":          `source "gateway"`,
		"intent_kind.json":           "not an effect kind",
		"missing_process.json":       "missing process",
		"zero_pid.json":              "pid 0",
		"relative_binary.json":       "not an absolute path",
		"relative_file_path.json":    "not an absolute path",
		"net_hostname.json":          "not an IP address",
		"net_bad_port.json":          "dst_port 0 out of range",
		"exit_empty.json":            "neither code nor signal",
		"payload_on_wrong_kind.json": "file payload on exec",
		"gateway_field.json":         "gateway-only field",
		"missing_sensor.json":        "missing sensor name",
		"wrong_version.json":         `version "99"`,
	}
	files, err := filepath.Glob(filepath.Join(contractDir, "invalid", "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != len(want) {
		t.Errorf("found %d invalid fixtures, table has %d", len(files), len(want))
	}
	for _, f := range files {
		name := filepath.Base(f)
		t.Run(name, func(t *testing.T) {
			substr, ok := want[name]
			if !ok {
				t.Fatalf("no expected reason for %s", name)
			}
			raw, err := os.ReadFile(f)
			if err != nil {
				t.Fatal(err)
			}
			err = Validate(decodeStrict(t, raw))
			if err == nil {
				t.Fatal("Validate accepted an invalid event")
			}
			if !strings.Contains(err.Error(), substr) {
				t.Errorf("error %q does not contain %q", err, substr)
			}
		})
	}
}

// TestValidateRejectsMissingKindPayload covers the kind-specific payloads
// that the fixtures above do not remove.
func TestValidateRejectsMissingKindPayload(t *testing.T) {
	base := func(k event.Kind) event.Event {
		e := event.New(event.SourceSensor, k, fixedTime)
		e.Sensor = "test"
		e.Process = &event.Process{PID: 1, Binary: "/bin/true"}
		return e
	}
	cases := map[event.Kind]string{
		event.KindFileWrite:         "missing file payload",
		event.KindFileReadSensitive: "missing file payload",
		event.KindNetConnect:        "missing net payload",
		event.KindProcExit:          "missing exit payload",
	}
	for k, substr := range cases {
		err := Validate(base(k))
		if err == nil || !strings.Contains(err.Error(), substr) {
			t.Errorf("%s: err = %v, want %q", k, err, substr)
		}
	}

	e := base(event.KindNetConnect)
	e.Net = &event.Net{Protocol: "icmp", DstAddr: "203.0.113.1", DstPort: 1}
	if err := Validate(e); err == nil || !strings.Contains(err.Error(), "want tcp or udp") {
		t.Errorf("icmp: err = %v", err)
	}
	e.Net = &event.Net{Protocol: "udp", DstAddr: "2001:db8::1", DstPort: 53, SrcAddr: "nope"}
	if err := Validate(e); err == nil || !strings.Contains(err.Error(), "src_addr") {
		t.Errorf("bad src: err = %v", err)
	}
	e.Net.SrcAddr = "2001:db8::2"
	if err := Validate(e); err != nil {
		t.Errorf("ipv6 udp: %v", err)
	}
}
