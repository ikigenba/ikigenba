package cli

import "github.com/ikigenba/ikigenba/repos/internal/web"

// Exit codes distinguish a successful run, host trouble and usage errors.
const (
	ExitSuccess      = 0
	ExitServerFailed = 1
	ExitUsage        = 2
)

// Usage is the command's complete help text.
const Usage = "Usage: repos [command]\n\nServe git repositories: smart HTTP at /<name>.git, MCP tools at /mcp, and a\nlanding page at /, on the socket systemd passes in. With no command, serve.\n\nCommands:\n  manifest    print the app manifest\n  db status   print applied and pending migrations\n\nOptions:\n  --help      print this help\n  --version   print the version\n\nExit codes:\n  0  success\n  1  failure\n  2  usage error\n"

// Manifest describes the binary to the host's app tooling.
const Manifest = "app = \"repos\"\ndescription = \"" + web.Description + "\"\ndefault = false\nmcp = true\nsecrets = []\n\n[env]\nREAD_SLOTS = \"8\"\nWRITE_SLOTS = \"2\"\nQUEUE_LENGTH = \"16\"\nQUEUE_SECONDS = \"30\"\nOPERATION_SECONDS = \"600\"\nPUSH_MAX_BYTES = \"104857600\"\nREPO_MAX_BYTES = \"1073741824\"\nMAINTENANCE_HOURS = \"24\"\n\n[database]\nengine = \"sqlite\"\npath = \"state/repos.db\"\n\n[resources]\nmemory_max = \"256M\"\ngo_memory_limit = \"128M\"\noom_policy = \"continue\"\n\n[home]\ngroup = \"core\"\n"

// NginxConf lets git request bodies stream and gives operations room to finish.
const NginxConf = "client_max_body_size    0;\nproxy_request_buffering off;\nproxy_http_version      1.1;\nproxy_buffering         on;\nproxy_read_timeout      3600s;\nproxy_send_timeout      3600s;\nlocation = /events { return 404; }\nlocation = /declarations { return 404; }\n"
