package tprsh

import (
	"strings"
	"testing"
)

// TestKubectlReadOnlyPolicy verifies kubectl read verbs pass and every
// mutating / escape / trust-repointing form is denied — tested through the
// pure CheckPolicy decision, so kubectl need not be installed.
func TestKubectlReadOnlyPolicy(t *testing.T) {
	allow := [][]string{
		{"get", "pods"},
		{"get", "pods", "-n", "kube-system"},
		{"-n", "kube-system", "get", "pods"}, // global flag before the verb
		{"describe", "node", "ip-10-1-208-43"},
		{"logs", "deploy/api"},
		{"top", "pods"},
		{"api-resources"},
		{"config", "view"},
		{"auth", "can-i", "get", "pods"},
		{"rollout", "status", "deploy/api"},
		{"cluster-info"},
		{"cluster-info", "dump"},
		{"get", "pods", "-o", "jsonpath={.items[*].metadata.name}"},
	}
	deny := [][]string{
		{"exec", "-it", "pod", "--", "sh"},
		{"proxy"},
		{"port-forward", "svc/api", "8080:80"},
		{"attach", "pod"},
		{"cp", "pod:/etc/passwd", "/tmp/x"},
		{"delete", "pod", "api"},
		{"apply", "-f", "evil.yaml"},
		{"patch", "deploy", "api", "-p", "{}"},
		{"edit", "deploy", "api"},
		{"scale", "deploy", "api", "--replicas=0"},
		{"drain", "node"},
		{"run", "x", "--image=busybox"},
		{"config", "set-context", "evil"},
		{"auth", "reconcile"},
		{"rollout", "restart", "deploy/api"},
		{"get", "pods", "--kubeconfig", "/tmp/evil"}, // trust repoint
		{"get", "pods", "--as", "cluster-admin"},     // impersonation
		{"get", "secrets", "--server", "https://evil"},
		{}, // no subcommand
	}
	for _, a := range allow {
		if err := CheckPolicy("kubectl", a, "/ws"); err != nil {
			t.Errorf("kubectl %s: expected allow, got %v", strings.Join(a, " "), err)
		}
	}
	for _, a := range deny {
		if err := CheckPolicy("kubectl", a, "/ws"); err == nil {
			t.Errorf("kubectl %s: expected DENY, it was allowed", strings.Join(a, " "))
		} else if !isDeny(err) {
			t.Errorf("kubectl %s: expected DenyError, got %T", strings.Join(a, " "), err)
		}
	}
}

// TestAwsReadOnlyPolicy verifies aws read operations pass and mutating ops /
// endpoint redirection are denied.
func TestAwsReadOnlyPolicy(t *testing.T) {
	allow := [][]string{
		{"ec2", "describe-instances"},
		{"s3", "ls"},
		{"s3", "ls", "s3://bucket"},
		{"s3api", "list-buckets"},
		{"s3api", "get-object", "--bucket", "b", "--key", "k", "out"},
		{"s3api", "head-bucket", "--bucket", "b"},
		{"iam", "list-users"},
		{"logs", "get-log-events", "--log-group-name", "g"},
		{"dynamodb", "scan", "--table-name", "t"},
		{"--region", "us-east-1", "ec2", "describe-instances"}, // global flag first
		{"sts", "get-caller-identity"},
		{"ec2", "help"},
	}
	deny := [][]string{
		{"ec2", "terminate-instances", "--instance-ids", "i-0"},
		{"ec2", "run-instances", "--image-id", "ami-0"},
		{"ec2", "stop-instances", "--instance-ids", "i-0"},
		{"s3", "cp", "s3://b/secret", "/tmp/x"},
		{"s3", "rm", "s3://b/obj"},
		{"s3", "sync", ".", "s3://b"},
		{"s3api", "put-object", "--bucket", "b", "--key", "k"},
		{"s3api", "delete-object", "--bucket", "b", "--key", "k"},
		{"iam", "create-user", "--user-name", "evil"},
		{"iam", "attach-user-policy", "--user-name", "x"},
		{"ec2", "describe-instances", "--endpoint-url", "https://evil"}, // endpoint redirect
		{"dynamodb", "delete-table", "--table-name", "t"},
		{},      // no service/op
		{"ec2"}, // service but no op
	}
	for _, a := range allow {
		if err := CheckPolicy("aws", a, "/ws"); err != nil {
			t.Errorf("aws %s: expected allow, got %v", strings.Join(a, " "), err)
		}
	}
	for _, a := range deny {
		if err := CheckPolicy("aws", a, "/ws"); err == nil {
			t.Errorf("aws %s: expected DENY, it was allowed", strings.Join(a, " "))
		} else if !isDeny(err) {
			t.Errorf("aws %s: expected DenyError, got %T", strings.Join(a, " "), err)
		}
	}
}
