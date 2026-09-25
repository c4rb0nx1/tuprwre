package sensor

import (
	"path"
	"strings"
)

// sensitiveSuffixes are path endings that name credential stores. They are
// matched against a slash-separated path, so "~/.aws/credentials" and
// "$HOME/.aws/credentials" match as well as absolute paths.
var sensitiveSuffixes = []string{
	"/.aws/credentials",
	"/.aws/config",
	"/.kube/config",
	"/.docker/config.json",
	"/.config/gh/hosts.yml",
	"/.netrc",
	"/.git-credentials",
	"/.pgpass",
	"/.npmrc",
	"/.pypirc",
	"/etc/shadow",
	"/etc/gshadow",
}

// sensitiveDirs are directory components whose contents are credentials.
var sensitiveDirs = []string{
	"/.config/gcloud/",
	"/.azure/",
	"/.gnupg/",
}

// IsSensitivePath reports whether p names a credential file: cloud and
// cluster credentials, SSH private keys, token stores, shadow password files,
// dotenv files, private-key material and keystores. p may be absolute, relative, or
// start with "~" or "$HOME"; it is matched lexically and never touches the
// filesystem. The list is a fixed, conservative default: it favours common
// credential locations over completeness.
func IsSensitivePath(p string) bool {
	if p == "" {
		return false
	}
	p = strings.ReplaceAll(p, "\\", "/")
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	p = path.Clean(p)
	for _, s := range sensitiveSuffixes {
		if strings.HasSuffix(p, s) {
			return true
		}
	}
	for _, d := range sensitiveDirs {
		if strings.Contains(p+"/", d) {
			return true
		}
	}
	base := path.Base(p)
	switch {
	case strings.Contains(p, "/.ssh/") && strings.HasPrefix(base, "id_") && !strings.HasSuffix(base, ".pub"):
		return true
	case base == ".env" || (strings.HasPrefix(base, ".env.") && !isEnvTemplate(base)):
		return true
	case hasAnySuffix(base, ".key", ".p12", ".pfx", ".keystore", ".jks", ".kdbx", ".ppk"):
		// Exclude system trust stores, which every TLS client reads.
		return !strings.HasPrefix(p, "/etc/ssl/") && !strings.HasPrefix(p, "/etc/pki/")
	}
	return false
}

// isEnvTemplate reports whether a ".env.*" basename is a committed template
// rather than a live secret file.
func isEnvTemplate(base string) bool {
	for _, s := range []string{".example", ".sample", ".template", ".dist"} {
		if strings.HasSuffix(base, s) {
			return true
		}
	}
	return false
}

func hasAnySuffix(s string, suffixes ...string) bool {
	for _, x := range suffixes {
		if strings.HasSuffix(s, x) {
			return true
		}
	}
	return false
}
