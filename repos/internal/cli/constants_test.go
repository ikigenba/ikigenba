package cli_test

import (
	"testing"

	"github.com/ikigenba/ikigenba/repos/internal/cli"
)

// R-SBKQ-JY43
func TestExitConstants(t *testing.T) {
	const (
		success = cli.ExitSuccess + 0
		failed  = cli.ExitServerFailed + 0
		usage   = cli.ExitUsage + 0
	)
	if success != 0 || failed != 1 || usage != 2 {
		t.Fatalf("exit codes = %d %d %d, want 0 1 2", success, failed, usage)
	}
}

// R-U1RM-28XI R-U2ZI-G0O7 R-U47E-TSEW
func TestTextConstants(t *testing.T) {
	const usage = cli.Usage + ""
	const manifest = cli.Manifest + ""
	const nginx = cli.NginxConf + ""
	wantUsage := "Usage: repos [command]\n\nServe git repositories: smart HTTP at /<name>.git, MCP tools at /mcp, and a\nlanding page at /, on the socket systemd passes in. With no command, serve.\n\nCommands:\n  manifest   print the app manifest\n\nOptions:\n  --help      print this help\n  --version   print the version\n\nExit codes:\n  0  success\n  1  the server failed\n  2  usage error\n"
	wantManifest := "app = \"repos\"\ndescription = \"Git repositories for the suite's content\"\ndefault = false\nmcp = true\nsecrets = []\n\n[env]\nREAD_SLOTS = \"8\"\nWRITE_SLOTS = \"2\"\nQUEUE_LENGTH = \"16\"\nQUEUE_SECONDS = \"30\"\nOPERATION_SECONDS = \"600\"\nPUSH_MAX_BYTES = \"104857600\"\nREPO_MAX_BYTES = \"1073741824\"\nMAINTENANCE_HOURS = \"24\"\n\n[database]\nengine = \"sqlite\"\npath = \"state/repos.db\"\n\n[resources]\ncpu_weight = 50\nmemory_max = \"1G\"\nio_weight = 50\n"
	wantNginx := "client_max_body_size    0;\nproxy_request_buffering off;\nproxy_http_version      1.1;\nproxy_buffering         on;\nproxy_read_timeout      3600s;\nproxy_send_timeout      3600s;\n"
	if usage != wantUsage || manifest != wantManifest || nginx != wantNginx {
		t.Fatalf("unexpected text constants: usage=%q manifest=%q nginx=%q", usage, manifest, nginx)
	}
}
