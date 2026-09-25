package sensor

import "testing"

func TestIsSensitivePath(t *testing.T) {
	yes := []string{
		"/home/agent/.aws/credentials",
		"~/.aws/config",
		"$HOME/.kube/config",
		"/root/.ssh/id_ed25519",
		"/home/agent/.ssh/id_rsa",
		"/home/agent/.config/gcloud/application_default_credentials.json",
		"/home/agent/.docker/config.json",
		"/home/agent/.netrc",
		"/home/agent/.git-credentials",
		"/etc/shadow",
		"/home/agent/work/.env",
		"work/.env.production",
		"/home/agent/work/tls/server.key",
		"/home/agent/.aws/../.aws/credentials",
		"/usr/share/elasticsearch/config/elasticsearch.keystore",
		"/opt/app/conf/truststore.jks",
		"/home/agent/vault.kdbx",
		"/home/agent/keys/server.ppk",
	}
	no := []string{
		"",
		"/home/agent/.ssh/id_rsa.pub",
		"/home/agent/.ssh/known_hosts",
		"/home/agent/work/main.go",
		"/home/agent/work/.env.example",
		"/etc/ssl/private/ssl-cert-snakeoil.key",
		"/etc/ssl/certs/ca-certificates.crt",
		"/home/agent/work/docs/aws-credentials.md",
		"/etc/passwd",
	}
	for _, p := range yes {
		if !IsSensitivePath(p) {
			t.Errorf("IsSensitivePath(%q) = false, want true", p)
		}
	}
	for _, p := range no {
		if IsSensitivePath(p) {
			t.Errorf("IsSensitivePath(%q) = true, want false", p)
		}
	}
}
