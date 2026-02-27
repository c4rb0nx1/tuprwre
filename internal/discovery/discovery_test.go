package discovery

import (
	"testing"
)

func TestFilterSystemBinaries_RemovesKnownBins(t *testing.T) {
	d := &Discoverer{}
	input := []Binary{
		{Name: "sh", Path: "/bin/sh"},
		{Name: "bash", Path: "/bin/bash"},
		{Name: "jq", Path: "/usr/local/bin/jq"},
		{Name: "curl", Path: "/usr/bin/curl"},
		{Name: "ripgrep", Path: "/usr/local/bin/ripgrep"},
	}

	result := d.FilterSystemBinaries(input)

	if len(result) != 2 {
		t.Fatalf("expected 2 binaries, got %d: %+v", len(result), result)
	}
	for _, b := range result {
		if b.Name == "sh" || b.Name == "bash" || b.Name == "curl" {
			t.Fatalf("system binary %q should have been filtered", b.Name)
		}
	}
}

func TestFilterSystemBinaries_PreservesAllNonSystem(t *testing.T) {
	d := &Discoverer{}
	input := []Binary{
		{Name: "mytool", Path: "/usr/local/bin/mytool"},
		{Name: "yq", Path: "/usr/local/bin/yq"},
	}

	result := d.FilterSystemBinaries(input)

	if len(result) != 2 {
		t.Fatalf("expected 2 binaries, got %d", len(result))
	}
}

func TestFilterSystemBinaries_EmptyInput(t *testing.T) {
	d := &Discoverer{}
	result := d.FilterSystemBinaries(nil)

	if len(result) != 0 {
		t.Fatalf("expected 0 binaries, got %d", len(result))
	}
}

func TestFilterSystemBinaries_AllSystem(t *testing.T) {
	d := &Discoverer{}
	input := []Binary{
		{Name: "sh", Path: "/bin/sh"},
		{Name: "bash", Path: "/bin/bash"},
		{Name: "curl", Path: "/usr/bin/curl"},
	}

	result := d.FilterSystemBinaries(input)

	if len(result) != 0 {
		t.Fatalf("expected 0 binaries, got %d: %+v", len(result), result)
	}
}

func TestDifference_BasicDiff(t *testing.T) {
	current := []string{"/usr/bin/a", "/usr/bin/b", "/usr/bin/c"}
	baseline := []string{"/usr/bin/a", "/usr/bin/b"}

	result := difference(current, baseline)

	if len(result) != 1 {
		t.Fatalf("expected 1 diff, got %d: %v", len(result), result)
	}
	if result[0] != "/usr/bin/c" {
		t.Fatalf("expected /usr/bin/c, got %s", result[0])
	}
}

func TestDifference_NoDiff(t *testing.T) {
	current := []string{"/usr/bin/a", "/usr/bin/b"}
	baseline := []string{"/usr/bin/a", "/usr/bin/b"}

	result := difference(current, baseline)

	if len(result) != 0 {
		t.Fatalf("expected 0 diff, got %d: %v", len(result), result)
	}
}

func TestDifference_AllNew(t *testing.T) {
	current := []string{"/usr/bin/x", "/usr/bin/y"}
	baseline := []string{"/usr/bin/a"}

	result := difference(current, baseline)

	if len(result) != 2 {
		t.Fatalf("expected 2 diff, got %d: %v", len(result), result)
	}
}

func TestDifference_EmptyBaseline(t *testing.T) {
	current := []string{"/usr/bin/a"}
	var baseline []string

	result := difference(current, baseline)

	if len(result) != 1 {
		t.Fatalf("expected 1 diff, got %d", len(result))
	}
}

func TestDifference_EmptyCurrent(t *testing.T) {
	var current []string
	baseline := []string{"/usr/bin/a"}

	result := difference(current, baseline)

	if len(result) != 0 {
		t.Fatalf("expected 0 diff, got %d", len(result))
	}
}

func TestExtractNameFromPath(t *testing.T) {
	tests := []struct {
		path string
		want string
	}{
		{"/usr/local/bin/jq", "jq"},
		{"/usr/bin/tool", "tool"},
		{"/opt/bin/my-tool", "my-tool"},
		{"tool", "tool"},
	}

	for _, tc := range tests {
		got := extractNameFromPath(tc.path)
		if got != tc.want {
			t.Errorf("extractNameFromPath(%q) = %q, want %q", tc.path, got, tc.want)
		}
	}
}

func TestDiscoverFromFilesystemDiff_NotImplemented(t *testing.T) {
	d := &Discoverer{}
	_, err := d.DiscoverFromFilesystemDiff("container-id", "base-image")
	if err == nil {
		t.Fatal("expected error for unimplemented method")
	}
}

func TestGetBinaryVersion_ReturnsEmpty(t *testing.T) {
	d := &Discoverer{}
	version, err := d.GetBinaryVersion("/usr/local/bin/tool", "container-id")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if version != "" {
		t.Fatalf("expected empty version, got %q", version)
	}
}
