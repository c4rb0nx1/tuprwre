package sandbox

import (
	"math"
	"testing"

	"github.com/docker/docker/api/types/container"
)

func TestParsePercentage(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    float64
		wantErr bool
	}{
		{name: "25%", input: "25%", want: 25},
		{name: "50%", input: "50%", want: 50},
		{name: "99.9%", input: "99.9%", want: 99.9},
		{name: "100%", input: "100%", want: 100},
		{name: "0.5%", input: "0.5%", want: 0.5},
		{name: "with spaces", input: "  25%  ", want: 25},
		{name: "zero", input: "0%", wantErr: true},
		{name: "negative", input: "-5%", wantErr: true},
		{name: "over 100", input: "101%", wantErr: true},
		{name: "not percentage", input: "512m", wantErr: true},
		{name: "empty", input: "", wantErr: true},
		{name: "just percent", input: "%", wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parsePercentage(tc.input)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got %g", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Fatalf("got %g, want %g", got, tc.want)
			}
		})
	}
}

func TestResolveMemory(t *testing.T) {
	hostMemory := int64(16 * 1024 * 1024 * 1024) // 16 GiB

	tests := []struct {
		name      string
		spec      string
		hostTotal int64
		want      int64
		wantErr   bool
	}{
		{name: "empty", spec: "", hostTotal: hostMemory, want: 0},
		{name: "absolute 512m", spec: "512m", hostTotal: hostMemory, want: 512 * 1024 * 1024},
		{name: "absolute 1g", spec: "1g", hostTotal: hostMemory, want: 1024 * 1024 * 1024},
		{name: "25% of 16GiB", spec: "25%", hostTotal: hostMemory, want: 4 * 1024 * 1024 * 1024},
		{name: "50% of 16GiB", spec: "50%", hostTotal: hostMemory, want: 8 * 1024 * 1024 * 1024},
		{name: "percentage without host info", spec: "25%", hostTotal: 0, wantErr: true},
		{name: "invalid spec", spec: "not-a-size", hostTotal: hostMemory, wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := resolveMemory(tc.spec, tc.hostTotal)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got %d", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Fatalf("got %d, want %d", got, tc.want)
			}
		})
	}
}

func TestResolveCPUs(t *testing.T) {
	hostCPUs := 8

	tests := []struct {
		name     string
		spec     string
		hostCPUs int
		want     float64
		wantErr  bool
	}{
		{name: "empty", spec: "", hostCPUs: hostCPUs, want: 0},
		{name: "absolute 2.0", spec: "2.0", hostCPUs: hostCPUs, want: 2.0},
		{name: "absolute 0.5", spec: "0.5", hostCPUs: hostCPUs, want: 0.5},
		{name: "50% of 8", spec: "50%", hostCPUs: hostCPUs, want: 4.0},
		{name: "25% of 8", spec: "25%", hostCPUs: hostCPUs, want: 2.0},
		{name: "percentage without host info", spec: "50%", hostCPUs: 0, wantErr: true},
		{name: "invalid spec", spec: "abc", hostCPUs: hostCPUs, wantErr: true},
		{name: "negative", spec: "-1", hostCPUs: hostCPUs, wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := resolveCPUs(tc.spec, tc.hostCPUs)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got %g", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if math.Abs(got-tc.want) > 0.001 {
				t.Fatalf("got %g, want %g", got, tc.want)
			}
		})
	}
}

func TestResolveResourceSpecWithHost(t *testing.T) {
	host := HostResources{
		MemoryTotal: 16 * 1024 * 1024 * 1024, // 16 GiB
		CPUCount:    8,
	}

	tests := []struct {
		name       string
		spec       ResourceSpec
		host       HostResources
		wantMem    int64
		wantCPUs   float64
		wantErr    bool
		wantIsZero bool
	}{
		{
			name:       "empty spec",
			spec:       ResourceSpec{},
			host:       host,
			wantIsZero: true,
		},
		{
			name:     "absolute memory only",
			spec:     ResourceSpec{Memory: "512m"},
			host:     host,
			wantMem:  512 * 1024 * 1024,
			wantCPUs: 0,
		},
		{
			name:     "absolute cpus only",
			spec:     ResourceSpec{CPUs: "2.0"},
			host:     host,
			wantMem:  0,
			wantCPUs: 2.0,
		},
		{
			name:     "both absolute",
			spec:     ResourceSpec{Memory: "1g", CPUs: "4"},
			host:     host,
			wantMem:  1024 * 1024 * 1024,
			wantCPUs: 4.0,
		},
		{
			name:     "both percentage",
			spec:     ResourceSpec{Memory: "25%", CPUs: "50%"},
			host:     host,
			wantMem:  4 * 1024 * 1024 * 1024,
			wantCPUs: 4.0,
		},
		{
			name:     "mixed absolute and percentage",
			spec:     ResourceSpec{Memory: "512m", CPUs: "50%"},
			host:     host,
			wantMem:  512 * 1024 * 1024,
			wantCPUs: 4.0,
		},
		{
			name:    "percentage without host info",
			spec:    ResourceSpec{Memory: "25%"},
			host:    HostResources{},
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ResolveResourceSpecWithHost(tc.spec, tc.host)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got %+v", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tc.wantIsZero && !got.IsZero() {
				t.Fatalf("expected zero policy, got %+v", got)
			}
			if got.Memory != tc.wantMem {
				t.Fatalf("memory: got %d, want %d", got.Memory, tc.wantMem)
			}
			if math.Abs(got.CPUs-tc.wantCPUs) > 0.001 {
				t.Fatalf("cpus: got %g, want %g", got.CPUs, tc.wantCPUs)
			}
		})
	}
}

func TestMergeResourceSpec(t *testing.T) {
	tests := []struct {
		name          string
		flagMemory    string
		flagCPUs      float64
		defaultMemory string
		defaultCPUs   string
		wantMemory    string
		wantCPUs      string
	}{
		{
			name:          "no flags no defaults",
			flagMemory:    "",
			flagCPUs:      0,
			defaultMemory: "",
			defaultCPUs:   "",
			wantMemory:    "",
			wantCPUs:      "",
		},
		{
			name:          "defaults only",
			flagMemory:    "",
			flagCPUs:      0,
			defaultMemory: "25%",
			defaultCPUs:   "50%",
			wantMemory:    "25%",
			wantCPUs:      "50%",
		},
		{
			name:          "flags override defaults",
			flagMemory:    "512m",
			flagCPUs:      2.0,
			defaultMemory: "25%",
			defaultCPUs:   "50%",
			wantMemory:    "512m",
			wantCPUs:      "2",
		},
		{
			name:          "partial flag override",
			flagMemory:    "1g",
			flagCPUs:      0,
			defaultMemory: "25%",
			defaultCPUs:   "50%",
			wantMemory:    "1g",
			wantCPUs:      "50%",
		},
		{
			name:          "flags only no defaults",
			flagMemory:    "256m",
			flagCPUs:      1.5,
			defaultMemory: "",
			defaultCPUs:   "",
			wantMemory:    "256m",
			wantCPUs:      "1.5",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := MergeResourceSpec(tc.flagMemory, tc.flagCPUs, tc.defaultMemory, tc.defaultCPUs)
			if got.Memory != tc.wantMemory {
				t.Fatalf("memory: got %q, want %q", got.Memory, tc.wantMemory)
			}
			if got.CPUs != tc.wantCPUs {
				t.Fatalf("cpus: got %q, want %q", got.CPUs, tc.wantCPUs)
			}
		})
	}
}

func TestApplyResourceLimits(t *testing.T) {
	tests := []struct {
		name         string
		policy       ResourcePolicy
		wantMemory   int64
		wantNanoCPUs int64
	}{
		{
			name:         "zero policy",
			policy:       ResourcePolicy{},
			wantMemory:   0,
			wantNanoCPUs: 0,
		},
		{
			name:         "memory only",
			policy:       ResourcePolicy{Memory: 512 * 1024 * 1024},
			wantMemory:   512 * 1024 * 1024,
			wantNanoCPUs: 0,
		},
		{
			name:         "cpus only",
			policy:       ResourcePolicy{CPUs: 2.0},
			wantMemory:   0,
			wantNanoCPUs: 2_000_000_000,
		},
		{
			name:         "both limits",
			policy:       ResourcePolicy{Memory: 1024 * 1024 * 1024, CPUs: 0.5},
			wantMemory:   1024 * 1024 * 1024,
			wantNanoCPUs: 500_000_000,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			hc := &container.HostConfig{}
			applyResourceLimits(hc, tc.policy)
			if hc.Resources.Memory != tc.wantMemory {
				t.Fatalf("memory: got %d, want %d", hc.Resources.Memory, tc.wantMemory)
			}
			if hc.Resources.NanoCPUs != tc.wantNanoCPUs {
				t.Fatalf("nanocpus: got %d, want %d", hc.Resources.NanoCPUs, tc.wantNanoCPUs)
			}
		})
	}
}

func TestIsPercentage(t *testing.T) {
	if !isPercentage("25%") {
		t.Fatal("expected 25% to be a percentage")
	}
	if !isPercentage("  50%  ") {
		t.Fatal("expected '  50%  ' to be a percentage")
	}
	if isPercentage("512m") {
		t.Fatal("expected 512m to not be a percentage")
	}
	if isPercentage("") {
		t.Fatal("expected empty string to not be a percentage")
	}
}

func TestResourcePolicyIsZero(t *testing.T) {
	if !(ResourcePolicy{}).IsZero() {
		t.Fatal("empty policy should be zero")
	}
	if (ResourcePolicy{Memory: 1}).IsZero() {
		t.Fatal("policy with memory should not be zero")
	}
	if (ResourcePolicy{CPUs: 0.5}).IsZero() {
		t.Fatal("policy with cpus should not be zero")
	}
}
