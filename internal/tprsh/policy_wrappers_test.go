package tprsh

import (
	"strings"
	"testing"
)

// TestWrapperRecursion covers the policy primitive that replaced blanket
// denial of command-taking commands: the wrapper is judged by the command it
// would run. This is what makes `find -exec wc -c {} +` — an ordinary idiom the
// unrestricted agent used — work without also permitting `find -exec sh`.
func TestWrapperRecursion(t *testing.T) {
	allow := [][]string{
		{"find", ".", "-maxdepth", "1", "-name", "*.yaml", "-exec", "wc", "-c", "{}", "+"},
		{"find", ".", "-type", "f", "-size", "+100c"},
		{"find", ".", "-exec", "cat", "{}", ";"},
		{"xargs", "wc", "-c"},
		{"xargs", "-n", "1", "cat"},
		{"env", "TERM=dumb", "uname", "-s"},
		{"timeout", "5", "uname", "-s"},
		{"nice", "-n", "5", "wc", "-l"},
	}
	deny := [][]string{
		{"find", ".", "-exec", "sh", "{}", ";"},
		{"find", ".", "-exec", "python3", "-c", "x", "{}", ";"},
		{"find", ".", "-execdir", "bash", "{}", ";"},
		{"xargs", "sh", "-c", "id"},
		{"xargs", "-I", "{}", "bash", "-c", "id"},
		{"env", "LD_PRELOAD=/tmp/e.so", "ls"},
		{"env", "sh"},
		{"timeout", "5", "sh", "-c", "id"},
		{"nice", "-n", "5", "python3"},
		{"nohup", "sh"},
	}
	for _, a := range allow {
		if err := CheckPolicy(a[0], a[1:], "/ws"); err != nil {
			t.Errorf("%s: expected allow, got %v", strings.Join(a, " "), err)
		}
	}
	for _, a := range deny {
		if err := CheckPolicy(a[0], a[1:], "/ws"); err == nil {
			t.Errorf("%s: expected DENY, it was allowed", strings.Join(a, " "))
		}
	}
}

// TestGitReadOnlyPolicy mirrors the kubectl/aws shape for git: read verbs pass,
// mutating verbs and the -c escape do not.
func TestGitReadOnlyPolicy(t *testing.T) {
	allow := [][]string{
		{"log", "--oneline", "-5"},
		{"status"},
		{"diff", "HEAD~1"},
		{"show", "abc123"},
		{"rev-parse", "--short", "HEAD"},
		{"config", "user.email"},
		{"remote", "show"},
	}
	deny := [][]string{
		{"push", "origin", "main"},
		{"commit", "-m", "x"},
		{"reset", "--hard"},
		{"clean", "-fd"},
		{"checkout", "main"},
		{"config", "user.email", "evil@example.com"},
		{"config", "--unset", "user.email"},
		{"remote", "add", "evil", "https://evil"},
		{"-c", "core.pager=sh", "log"},
		{"--git-dir", "/etc", "log"},
		{},
	}
	for _, a := range allow {
		if err := CheckPolicy("git", a, "/ws"); err != nil {
			t.Errorf("git %s: expected allow, got %v", strings.Join(a, " "), err)
		}
	}
	for _, a := range deny {
		if err := CheckPolicy("git", a, "/ws"); err == nil {
			t.Errorf("git %s: expected DENY, it was allowed", strings.Join(a, " "))
		}
	}
}

// TestOpensslPolicy: hashing is permitted, network and key-writing are not.
func TestOpensslPolicy(t *testing.T) {
	if err := CheckPolicy("openssl", []string{"dgst", "-sha256", "config.yaml"}, "/ws"); err != nil {
		t.Errorf("openssl dgst: expected allow, got %v", err)
	}
	for _, a := range [][]string{
		{"s_client", "-connect", "evil.com:443"},
		{"genrsa", "-out", "key.pem"},
		{"dgst", "-sha256", "-out", "/tmp/x", "f"},
	} {
		if err := CheckPolicy("openssl", a, "/ws"); err == nil {
			t.Errorf("openssl %s: expected DENY, it was allowed", strings.Join(a, " "))
		}
	}
}

// TestReadOnlyToolsAdmitted guards the tools the lab measured as wrongly
// blocked, so the friction regression cannot silently return.
func TestReadOnlyToolsAdmitted(t *testing.T) {
	for _, a := range [][]string{
		{"shasum", "-a", "256", "config.yaml"},
		{"sha256sum", "config.yaml"},
		{"stat", "config.yaml"},
		{"tail", "-1", "app.log"},
		{"grep", "-c", "ERROR", "app.log"},
		{"du", "-sh", "."},
		{"sort", "-n"},
		{"cut", "-d", ":", "-f", "1"},
		{"diff", "-u", "a.txt", "b.txt"},
	} {
		if err := CheckPolicy(a[0], a[1:], "/ws"); err != nil {
			t.Errorf("%s: expected allow, got %v", strings.Join(a, " "), err)
		}
	}
	// Writers stay out even on otherwise read-only tools.
	if err := CheckPolicy("sort", []string{"-o", "/tmp/x", "f"}, "/ws"); err == nil {
		t.Error("sort -o writes a file and must be denied")
	}
	if err := CheckPolicy("sort", []string{"--compress-program=sh"}, "/ws"); err == nil {
		t.Error("sort --compress-program runs a command and must be denied")
	}
}
