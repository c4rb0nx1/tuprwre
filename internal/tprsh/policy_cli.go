package tprsh

import "strings"

// This file encodes read-only argument policies for the infra-watcher's real
// tools. These are subcommand CLIs where the mutating power is the verb
// (kubectl exec, aws s3 cp), so a flat flag allowlist is the wrong shape — we
// resolve the verb/operation and allow only the read-only set, deny-by-default.
//
// Two structural rules beyond the verb set:
//   - Dangerous global flags are refused anywhere on the line, because a read
//     verb with --kubeconfig / --as / --endpoint-url repoints trust or
//     impersonates even while "only reading".
//   - Verb resolution skips value-taking global flags so `kubectl -n ns get`
//     still resolves the verb as `get`, not `ns`.

// ---- kubectl ----

// kubectlDangerousFlags repoint the API target or impersonate; refused even on
// a read verb.
var kubectlDangerousFlags = set(
	"--kubeconfig", "--token", "--as", "--as-group", "--as-uid",
	"--server", "-s", "--insecure-skip-tls-verify",
	"--client-certificate", "--client-key", "--tls-server-name",
)

// kubectlValueFlags are global flags that consume the following token, so verb
// resolution can skip their values. (Dangerous value-flags are refused
// separately; these are the benign ones.)
var kubectlValueFlags = set(
	"-n", "--namespace", "--context", "--cluster", "--user",
	"-v", "--v", "--request-timeout", "--cache-dir", "--chunk-size",
	"-l", "--selector", "--field-selector", "-o", "--output",
)

// kubectlReadVerbs are read-only and need no sub-verb restriction.
var kubectlReadVerbs = set(
	"get", "describe", "logs", "top", "explain", "version",
	"api-resources", "api-versions", "events", "wait", "diff", "kustomize",
)

// kubectlReadSubVerbs constrains verbs whose read/write split is one level
// deeper: only the listed sub-verbs are read-only.
var kubectlReadSubVerbs = map[string]map[string]bool{
	"config":       set("view", "get-contexts", "current-context", "get-clusters"),
	"auth":         set("can-i", "whoami"),
	"rollout":      set("status", "history"),
	"cluster-info": set("", "dump"), // bare `cluster-info` and `cluster-info dump`
}

func kubectlValidate(_ string, rest []string, _ string) error {
	for _, a := range rest {
		if kubectlDangerousFlags[flagBase(a)] {
			return deny("kubectl flag %q repoints or impersonates the API and is not permitted", flagBase(a))
		}
	}
	verb, idx, ok := resolveVerb(rest, kubectlValueFlags)
	if !ok {
		return deny("kubectl requires a read-only subcommand")
	}
	if kubectlReadVerbs[verb] {
		return nil
	}
	if subs, ok := kubectlReadSubVerbs[verb]; ok {
		sub, _, _ := resolveVerb(rest[idx+1:], kubectlValueFlags)
		if subs[sub] {
			return nil
		}
		return deny("kubectl %s %s is not a read-only operation", verb, sub)
	}
	return deny("kubectl %s is not a read-only operation", verb)
}

// ---- aws ----

// awsDangerousFlags redirect the API endpoint to an attacker-controlled host.
var awsDangerousFlags = set("--endpoint-url", "--endpoint")

// awsValueFlags consume the next token during service/operation resolution.
var awsValueFlags = set(
	"--region", "--profile", "--output", "--query", "--color",
	"--ca-bundle", "--cli-read-timeout", "--cli-connect-timeout", "--endpoint-url",
)

// awsReadOpPrefixes mark read-only operations across most services.
var awsReadOpPrefixes = []string{"describe-", "list-", "get-", "lookup-", "search-", "batch-get-"}

// awsReadOpExact are read-only operations that do not match a prefix.
var awsReadOpExact = set("query", "scan", "help")

func awsValidate(_ string, rest []string, _ string) error {
	for _, a := range rest {
		if awsDangerousFlags[flagBase(a)] {
			return deny("aws flag %q redirects the API endpoint and is not permitted", flagBase(a))
		}
	}
	service, idx, ok := resolveVerb(rest, awsValueFlags)
	if !ok {
		return deny("aws requires a service and a read-only operation")
	}
	if service == "help" {
		return nil
	}
	op, _, ok := resolveVerb(rest[idx+1:], awsValueFlags)
	if !ok {
		return deny("aws %s requires a read-only operation", service)
	}
	if op == "help" {
		return nil
	}

	// The s3 high-level command uses filesystem-style verbs, not API operations.
	if service == "s3" {
		if op == "ls" {
			return nil
		}
		return deny("aws s3 %s is not read-only (only `ls` is permitted)", op)
	}
	if service == "s3api" {
		if strings.HasPrefix(op, "get-") || strings.HasPrefix(op, "list-") || strings.HasPrefix(op, "head-") {
			return nil
		}
		return deny("aws s3api %s is not a read-only operation", op)
	}

	for _, p := range awsReadOpPrefixes {
		if strings.HasPrefix(op, p) {
			return nil
		}
	}
	if awsReadOpExact[op] {
		return nil
	}
	return deny("aws %s %s is not a recognized read-only operation", service, op)
}

// ---- shared helpers ----

// flagBase strips a =value suffix so --as=admin matches --as.
func flagBase(tok string) string {
	if i := strings.IndexByte(tok, '='); i >= 0 {
		return tok[:i]
	}
	return tok
}

// resolveVerb returns the first positional token (the verb/service), skipping
// flags and the values consumed by value-taking flags. idx is its position in
// rest. This keeps `-n ns get` from resolving the verb as `ns`.
func resolveVerb(rest []string, valueFlags map[string]bool) (verb string, idx int, ok bool) {
	for i := 0; i < len(rest); i++ {
		tok := rest[i]
		if strings.HasPrefix(tok, "-") && tok != "-" {
			// --flag=value is self-contained; --flag value consumes the next token.
			if !strings.ContainsRune(tok, '=') && valueFlags[tok] {
				i++
			}
			continue
		}
		return tok, i, true
	}
	return "", 0, false
}
