#!/bin/sh
set -eu

REPO=ikigenba/ikigenba
BINARY=agent-monitor
AGENT_MONITOR_VERSION=${AGENT_MONITOR_VERSION:-}
BINDIR=${BINDIR:-${PREFIX:-$HOME/.local}/bin}

# die <message> [detail-file]: a diagnostic on stderr, then exit 1. Another
# program's output (the detail file) is quoted after one empty line, every
# line prefixed "> ".
die() {
    echo "agent-monitor: $1" >&2
    if [ -n "${2:-}" ] && [ -s "$2" ]; then
        echo >&2
        sed 's/^/> /' "$2" >&2
    fi
    exit 1
}

case "$(uname -s)" in
    Linux) os=linux ;;
    Darwin) os=darwin ;;
    *) die "unsupported operating system: $(uname -s)" ;;
esac

case "$(uname -m)" in
    x86_64 | amd64) arch=amd64 ;;
    arm64 | aarch64) arch=arm64 ;;
    *) die "unsupported architecture: $(uname -m)" ;;
esac

asset="${BINARY}_${os}_${arch}.tar.gz"

tmpdir=$(mktemp -d)
trap 'rm -rf "$tmpdir"' 0 HUP INT TERM

# newest_stable: read a GitHub releases listing on stdin and print the newest
# stable agent-monitor version carrying $asset (vMAJOR.MINOR.PATCH), or
# nothing. This is a monorepo: the repo's "latest release" may belong to
# another tool, and the API does not order releases by version -- it returns
# them in lexical tag order, where v0.9.0 outranks v0.10.0. A prerelease tag
# (agent-monitor/v1.2.0-rc.1) never matches: the pattern needs the asset path
# right after PATCH. Stable versions order by MAJOR, MINOR, PATCH numerically.
newest_stable() {
    grep -oE "https://github.com/${REPO}/releases/download/agent-monitor(%2F|/)v[0-9]+\.[0-9]+\.[0-9]+/${asset}" \
        | sed -E 's#.*/agent-monitor(%2F|/)v##; s#/.*##' \
        | sort -t. -k1,1n -k2,2n -k3,3n \
        | tail -1 \
        | sed 's/^/v/'
}

if [ -z "$AGENT_MONITOR_VERSION" ]; then
    if ! curl -fsSL "https://api.github.com/repos/${REPO}/releases?per_page=100" \
        -o "$tmpdir/releases.json" 2>"$tmpdir/curl.err"; then
        die "cannot list releases of ${REPO}" "$tmpdir/curl.err"
    fi
    AGENT_MONITOR_VERSION=$(newest_stable <"$tmpdir/releases.json")
    if [ -z "$AGENT_MONITOR_VERSION" ]; then
        die "no stable agent-monitor release with asset ${asset} found"
    fi
else
    # A pinned version is installed exactly, a prerelease included. Release
    # tags never carry build metadata, so a version with "+..." names no
    # release.
    if ! printf '%s\n' "$AGENT_MONITOR_VERSION" \
        | grep -qE '^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-[0-9A-Za-z-]+(\.[0-9A-Za-z-]+)*)?$'; then
        die "invalid AGENT_MONITOR_VERSION '${AGENT_MONITOR_VERSION}': expected v<semver> without build metadata, e.g. v1.2.0 or v1.2.0-rc.1"
    fi
fi

# Tags are agent-monitor/v<semver>; the slash is percent-encoded in download
# URLs. The version itself holds only URL-safe characters ([0-9A-Za-z.-]).
url="https://github.com/${REPO}/releases/download/agent-monitor%2F${AGENT_MONITOR_VERSION}/${asset}"

if ! curl -fsSL "$url" -o "$tmpdir/$asset" 2>"$tmpdir/curl.err"; then
    die "cannot download ${url}" "$tmpdir/curl.err"
fi
tar -xzf "$tmpdir/$asset" -C "$tmpdir" "$BINARY"
mkdir -p "$BINDIR"
install -m 0755 "$tmpdir/$BINARY" "$BINDIR/$BINARY"
echo "Installed $BINARY $AGENT_MONITOR_VERSION to $BINDIR/$BINARY"

case ":${PATH:-}:" in
    *:"$BINDIR":*) ;;
    *) echo "agent-monitor: warning: $BINDIR is not on PATH" >&2 ;;
esac
