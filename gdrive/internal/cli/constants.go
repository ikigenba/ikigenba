package cli

import (
	"net"
	"net/http"
	"time"
)

// ExitSuccess means the operation succeeded.
const ExitSuccess = 0

// ExitServerFailed means startup or request draining failed.
const ExitServerFailed = 1

// ExitUsage means arguments, settings or socket activation were invalid.
const ExitUsage = 2

// DialTimeout bounds connecting to Drive.
const DialTimeout = 10 * time.Second

// TLSHandshakeTimeout bounds the Drive TLS handshake.
const TLSHandshakeTimeout = 10 * time.Second

// ResponseHeaderTimeout bounds waiting for Drive response headers.
const ResponseHeaderTimeout = 60 * time.Second

// SweepInterval is the interval between transfer prunes.
const SweepInterval = time.Hour

// Usage is the command help.
const Usage = "Usage: gdrive [command]\n\nGoogle Drive files, pulled into and pushed from artifacts: MCP tools at\n/mcp record transfers that move the bytes on the host, with a page of\ntransfers at /, on the socket systemd passes in.\nWith no command, serve.\n\nCommands:\n  manifest    print the app manifest\n  db status   print applied and pending migrations\n\nOptions:\n  --help      print this help\n  --version   print the version\n\nExit codes:\n  0  success\n  1  failure\n  2  usage error\n"

// Manifest is the published app manifest.
const Manifest = `app = "gdrive"
description = "Google Drive files, pulled into and pushed from artifacts"
default = false
mcp = true
guests = false
secrets = []

[env]
TRANSFER_MAX_BYTES = "1073741824"
TRANSFER_MAX_ACTIVE = "2"
TRANSFER_MAX_QUEUED = "10"
TRANSFER_KEEP_DAYS = "15"
TRANSFER_KEEP_COUNT = "100"

[database]
engine = "sqlite"
path = "state/gdrive.db"

[resources]
memory_max = "128M"

[home]
group = "core"
`

// NginxConf keeps bus endpoints off the public host.
const NginxConf = "location = /events { return 404; }\nlocation = /declarations { return 404; }\n"

// DefaultHTTP returns a fresh streaming client with bounded setup phases.
func DefaultHTTP() *http.Client {
	return &http.Client{Transport: &http.Transport{Proxy: http.ProxyFromEnvironment, DialContext: (&net.Dialer{Timeout: DialTimeout, KeepAlive: 30 * time.Second}).DialContext, TLSHandshakeTimeout: TLSHandshakeTimeout, ResponseHeaderTimeout: ResponseHeaderTimeout, ForceAttemptHTTP2: true}}
}
