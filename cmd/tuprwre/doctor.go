package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"github.com/c4rb0nx1/tuprwre/internal/config"
)

const (
	doctorStatusPass = "PASS"
	doctorStatusFail = "FAIL"
)

var doctorJSON bool

var doctorLookPath = exec.LookPath
var doctorRunCommand = func(name string, args ...string) ([]byte, error) {
	cmd := exec.Command(name, args...)
	return cmd.CombinedOutput()
}

var doctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Run preflight environment checks",
	Long: `Verifies critical runtime and environment assumptions required by tuprwre.

Checks include binary discovery, version command sanity, runtime config validity,
shim directory visibility, Docker daemon reachability, and writable state dirs.`,
	RunE: runDoctor,
}

type doctorCheck struct {
	Name     string `json:"name"`
	Status   string `json:"status"`
	Critical bool   `json:"critical"`
	Message  string `json:"message"`
}

type doctorReport struct {
	Healthy bool          `json:"healthy"`
	Checks  []doctorCheck `json:"checks"`
}

func init() {
	doctorCmd.Flags().BoolVar(&doctorJSON, "json", false, "Emit machine-readable JSON output")
}

func runDoctor(cmd *cobra.Command, _ []string) error {
	out := cmd.OutOrStdout()
	report := doctorReport{}

	activePath, activePathCheck := doctorCheckActiveBinary()
	addDoctorCheck := func(c doctorCheck) {
		report.Checks = append(report.Checks, c)
	}
	addDoctorCheck(activePathCheck)

	versionCheck := doctorCheckVersion(activePath)
	addDoctorCheck(versionCheck)

	cfg, cfgErr := config.Load()
	if cfgErr != nil {
		addDoctorCheck(doctorCheck{
			Name:     "Runtime config",
			Status:   doctorStatusFail,
			Critical: true,
			Message:  fmt.Sprintf("could not load config: %v", cfgErr),
		})
	} else {
		runtimeCheck := doctorCheckRuntime(cfg)
		addDoctorCheck(runtimeCheck)

		addDoctorCheck(doctorCheckShimDir(cfg.ShimDir))
		addDoctorCheck(doctorCheckWritableDir(cfg.BaseDir, "state directory"))
		addDoctorCheck(doctorCheckWritableDir(cfg.ShimDir, "shim directory"))

		if runtimeCheck.Status == doctorStatusPass && cfg.ContainerRuntime == "docker" {
			addDoctorCheck(doctorCheckDockerReachable())
		}
	}

	criticalFailures := 0
	for _, check := range report.Checks {
		if check.Status == doctorStatusFail && check.Critical {
			criticalFailures++
		}
	}
	report.Healthy = criticalFailures == 0

	if doctorJSON {
		payload, err := json.MarshalIndent(report, "", "  ")
		if err != nil {
			return fmt.Errorf("failed to build doctor JSON report: %w", err)
		}
		_, _ = out.Write(payload)
		_, _ = fmt.Fprintln(out)
		if report.Healthy {
			return nil
		}
		return fmt.Errorf("critical preflight checks failed")
	}

	for _, check := range report.Checks {
		prefix := "PASS"
		if check.Status != doctorStatusPass {
			prefix = "FAIL"
		}
		criticalMarker := ""
		if check.Critical {
			criticalMarker = "[critical] "
		}
		fmt.Fprintf(out, "[%s] %s%s: %s\n", prefix, criticalMarker, check.Name, check.Message)
	}

	if report.Healthy {
		_, _ = fmt.Fprintln(out, "[PASS] All critical checks passed.")
		return nil
	}
	return fmt.Errorf("critical preflight checks failed")
}

func doctorCheckActiveBinary() (string, doctorCheck) {
	path, err := doctorLookPath("tuprwre")
	if err != nil {
		return "", doctorCheck{
			Name:     "Active binary path",
			Status:   doctorStatusFail,
			Critical: true,
			Message:  "tuprwre is not discoverable from PATH",
		}
	}

	return path, doctorCheck{
		Name:     "Active binary path",
		Status:   doctorStatusPass,
		Critical: true,
		Message:  fmt.Sprintf("command -v found %s", path),
	}
}

func doctorCheckVersion(binaryPath string) doctorCheck {
	if binaryPath == "" {
		return doctorCheck{
			Name:     "CLI version",
			Status:   doctorStatusFail,
			Critical: false,
			Message:  "skipped version check: active binary path unavailable",
		}
	}

	output, err := doctorRunCommand(binaryPath, "--version")
	if err != nil {
		return doctorCheck{
			Name:     "CLI version",
			Status:   doctorStatusFail,
			Critical: false,
			Message:  fmt.Sprintf("failed to run %s --version: %v", binaryPath, err),
		}
	}

	out := strings.TrimSpace(string(output))
	if out == "" {
		return doctorCheck{
			Name:     "CLI version",
			Status:   doctorStatusFail,
			Critical: false,
			Message:  fmt.Sprintf("%s --version produced empty output", binaryPath),
		}
	}
	if !strings.Contains(strings.ToLower(out), "tuprwre") {
		return doctorCheck{
			Name:     "CLI version",
			Status:   doctorStatusFail,
			Critical: false,
			Message:  fmt.Sprintf("version output did not include command name: %q", out),
		}
	}

	return doctorCheck{
		Name:     "CLI version",
		Status:   doctorStatusPass,
		Critical: false,
		Message:  out,
	}
}

func doctorCheckRuntime(cfg *config.Config) doctorCheck {
	runtime := strings.ToLower(strings.TrimSpace(cfg.ContainerRuntime))
	switch runtime {
	case "docker":
		return doctorCheck{
			Name:     "Runtime config",
			Status:   doctorStatusPass,
			Critical: true,
			Message:  fmt.Sprintf("TUPRWRE_RUNTIME=%s", runtime),
		}
	case "containerd":
		return doctorCheck{
			Name:     "Runtime config",
			Status:   doctorStatusFail,
			Critical: true,
			Message:  "runtime containerd is not implemented yet in run path",
		}
	}

	return doctorCheck{
		Name:     "Runtime config",
		Status:   doctorStatusFail,
		Critical: true,
		Message:  fmt.Sprintf("runtime %q is not supported (supported: docker, containerd)", cfg.ContainerRuntime),
	}
}

func doctorCheckShimDir(shimDir string) doctorCheck {
	if _, err := os.Stat(shimDir); err != nil {
		return doctorCheck{
			Name:     "Shim directory",
			Status:   doctorStatusFail,
			Critical: true,
			Message:  fmt.Sprintf("shim directory missing: %v", err),
		}
	}

	if !isPathEntry(shimDir, os.Getenv("PATH")) {
		return doctorCheck{
			Name:     "Shim directory",
			Status:   doctorStatusFail,
			Critical: true,
			Message:  fmt.Sprintf("shim directory %q is not on PATH", shimDir),
		}
	}

	return doctorCheck{
		Name:     "Shim directory",
		Status:   doctorStatusPass,
		Critical: true,
		Message:  fmt.Sprintf("shim directory on PATH: %q", shimDir),
	}
}

func doctorCheckDockerReachable() doctorCheck {
	output, err := doctorRunCommand("docker", "version", "--format", "{{.Server.Version}}")
	if err != nil {
		return doctorCheck{
			Name:     "Docker daemon",
			Status:   doctorStatusFail,
			Critical: true,
			Message:  fmt.Sprintf("docker daemon ping failed: %v", err),
		}
	}

	out := strings.TrimSpace(string(output))
	if out == "" {
		return doctorCheck{
			Name:     "Docker daemon",
			Status:   doctorStatusFail,
			Critical: true,
			Message:  "docker daemon ping returned no version output",
		}
	}

	return doctorCheck{
		Name:     "Docker daemon",
		Status:   doctorStatusPass,
		Critical: true,
		Message:  fmt.Sprintf("docker daemon reachable (server=%s)", out),
	}
}

func doctorCheckWritableDir(path string, label string) doctorCheck {
	if err := os.MkdirAll(path, 0o755); err != nil {
		return doctorCheck{
			Name:     fmt.Sprintf("Writable state dir (%s)", label),
			Status:   doctorStatusFail,
			Critical: true,
			Message:  fmt.Sprintf("failed to create %q: %v", path, err),
		}
	}

	tmp, err := os.CreateTemp(path, ".tuprwre-doctor-check-*")
	if err != nil {
		return doctorCheck{
			Name:     fmt.Sprintf("Writable state dir (%s)", label),
			Status:   doctorStatusFail,
			Critical: true,
			Message:  fmt.Sprintf("directory not writable: %v", err),
		}
	}
	_ = os.Remove(tmp.Name())

	return doctorCheck{
		Name:     fmt.Sprintf("Writable state dir (%s)", label),
		Status:   doctorStatusPass,
		Critical: true,
		Message:  fmt.Sprintf("writable directory: %s", path),
	}
}

func isPathEntry(target string, pathEnv string) bool {
	target = filepath.Clean(target)
	for _, entry := range filepath.SplitList(pathEnv) {
		if filepath.Clean(entry) == target {
			return true
		}
	}
	return false
}
