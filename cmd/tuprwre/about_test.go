package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestRunAboutIncludesVersionAndQuickStart(t *testing.T) {
	var out bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetOut(&out)

	if err := runAbout(cmd, nil); err != nil {
		t.Fatalf("runAbout returned error: %v", err)
	}

	body := out.String()
	if !strings.Contains(body, "tuprwre "+version) {
		t.Fatalf("about output missing version: %q", body)
	}

	for _, expected := range []string{
		"tuprwre shell",
		"tuprwre install --",
		"tuprwre --help",
	} {
		if !strings.Contains(body, expected) {
			t.Fatalf("about output missing %q: %q", expected, body)
		}
	}
}
