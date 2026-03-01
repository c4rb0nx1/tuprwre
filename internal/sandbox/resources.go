package sandbox

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/go-units"
)

// ResourcePolicy holds resolved container resource limits.
// Zero values mean no limit is enforced.
type ResourcePolicy struct {
	Memory int64   // bytes; 0 = no limit
	CPUs   float64 // number of CPUs; 0 = no limit
}

// ResourceSpec holds raw resource specifications that may include
// host-relative percentages (e.g., "25%", "50%") or absolute values
// (e.g., "512m", "2.0").
type ResourceSpec struct {
	Memory string // "512m", "1g", "25%", or ""
	CPUs   string // "2.0", "50%", or ""
}

// IsZero returns true if no limits are specified.
func (p ResourcePolicy) IsZero() bool {
	return p.Memory == 0 && p.CPUs == 0
}

// MergeResourceSpec merges CLI flag values with config defaults.
// Flag values take precedence when non-empty/non-zero.
func MergeResourceSpec(flagMemory string, flagCPUs float64, defaultMemory, defaultCPUs string) ResourceSpec {
	spec := ResourceSpec{
		Memory: defaultMemory,
		CPUs:   defaultCPUs,
	}
	if flagMemory != "" {
		spec.Memory = flagMemory
	}
	if flagCPUs > 0 {
		spec.CPUs = fmt.Sprintf("%g", flagCPUs)
	}
	return spec
}

// applyResourceLimits sets resource limits on a HostConfig.
func applyResourceLimits(hc *container.HostConfig, p ResourcePolicy) {
	if p.Memory > 0 {
		hc.Resources.Memory = p.Memory
	}
	if p.CPUs > 0 {
		hc.Resources.NanoCPUs = int64(p.CPUs * 1e9)
	}
}

// HostResources holds host machine resource info for resolving relative specs.
type HostResources struct {
	MemoryTotal int64
	CPUCount    int
}

// ResolveResourceSpec resolves a ResourceSpec into concrete ResourcePolicy
// values. Percentage-based specs (e.g., "25%") are resolved against the
// Docker host's total memory and CPU count.
func (d *DockerRuntime) ResolveResourceSpec(ctx context.Context, spec ResourceSpec) (ResourcePolicy, error) {
	if spec.Memory == "" && spec.CPUs == "" {
		return ResourcePolicy{}, nil
	}

	needsHostInfo := isPercentage(spec.Memory) || isPercentage(spec.CPUs)

	var host HostResources
	if needsHostInfo {
		if err := d.initClient(); err != nil {
			return ResourcePolicy{}, fmt.Errorf("failed to query host resources: %w", err)
		}
		info, err := d.client.Info(ctx)
		if err != nil {
			return ResourcePolicy{}, fmt.Errorf("failed to query Docker host info: %w", err)
		}
		host = HostResources{
			MemoryTotal: info.MemTotal,
			CPUCount:    info.NCPU,
		}
	}

	return ResolveResourceSpecWithHost(spec, host)
}

// ResolveResourceSpecWithHost resolves a ResourceSpec using provided host info.
// Exported for testability without requiring a Docker connection.
func ResolveResourceSpecWithHost(spec ResourceSpec, host HostResources) (ResourcePolicy, error) {
	var policy ResourcePolicy

	if spec.Memory != "" {
		mem, err := resolveMemory(spec.Memory, host.MemoryTotal)
		if err != nil {
			return policy, fmt.Errorf("invalid memory spec %q: %w", spec.Memory, err)
		}
		policy.Memory = mem
	}

	if spec.CPUs != "" {
		cpus, err := resolveCPUs(spec.CPUs, host.CPUCount)
		if err != nil {
			return policy, fmt.Errorf("invalid CPU spec %q: %w", spec.CPUs, err)
		}
		policy.CPUs = cpus
	}

	return policy, nil
}

func isPercentage(s string) bool {
	return strings.HasSuffix(strings.TrimSpace(s), "%")
}

func resolveMemory(spec string, hostTotal int64) (int64, error) {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return 0, nil
	}

	if strings.HasSuffix(spec, "%") {
		pct, err := parsePercentage(spec)
		if err != nil {
			return 0, err
		}
		if hostTotal <= 0 {
			return 0, fmt.Errorf("cannot resolve percentage: host memory info unavailable")
		}
		return int64(float64(hostTotal) * pct / 100), nil
	}

	return units.RAMInBytes(spec)
}

func resolveCPUs(spec string, hostCPUs int) (float64, error) {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return 0, nil
	}

	if strings.HasSuffix(spec, "%") {
		pct, err := parsePercentage(spec)
		if err != nil {
			return 0, err
		}
		if hostCPUs <= 0 {
			return 0, fmt.Errorf("cannot resolve percentage: host CPU count unavailable")
		}
		return float64(hostCPUs) * pct / 100, nil
	}

	v, err := strconv.ParseFloat(spec, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid CPU value: %w", err)
	}
	if v < 0 {
		return 0, fmt.Errorf("CPU limit must be non-negative, got %g", v)
	}
	return v, nil
}

func parsePercentage(s string) (float64, error) {
	s = strings.TrimSpace(s)
	if !strings.HasSuffix(s, "%") {
		return 0, fmt.Errorf("not a percentage: %q", s)
	}
	numStr := strings.TrimSuffix(s, "%")
	val, err := strconv.ParseFloat(numStr, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid percentage %q: %w", s, err)
	}
	if val <= 0 || val > 100 {
		return 0, fmt.Errorf("percentage must be greater than 0 and at most 100, got %g", val)
	}
	return val, nil
}
