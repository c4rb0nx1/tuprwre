package main

import (
	"strings"
	"testing"
)

func TestRunRuntimeValidation(t *testing.T) {
	t.Run("DockerRuntimeIsAccepted", func(t *testing.T) {
		if err := validateRunRuntime("docker"); err != nil {
			t.Fatalf("expected docker runtime to be accepted, got: %v", err)
		}
		if err := validateRunRuntime("DoCkEr"); err != nil {
			t.Fatalf("expected case-insensitive docker runtime to be accepted, got: %v", err)
		}
	})

	t.Run("ContainerdRuntimeIsNotImplementedYet", func(t *testing.T) {
		if err := validateRunRuntime("containerd"); err == nil {
			t.Fatal("expected containerd runtime to fail")
		} else if !strings.Contains(err.Error(), "not implemented yet in run path") {
			t.Fatalf("unexpected containerd runtime error: %v", err)
		}
	})

	t.Run("UnknownRuntimeFailsFast", func(t *testing.T) {
		err := validateRunRuntime("weird-runtime")
		if err == nil {
			t.Fatal("expected unknown runtime to fail")
		}
		if !strings.Contains(err.Error(), "is not supported") {
			t.Fatalf("unexpected unknown runtime error: %v", err)
		}
	})
}
