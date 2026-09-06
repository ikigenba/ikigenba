#!/bin/sh
set -eu

REPO=ikigenba/ikigenba
BINARY=agent-repl
AGENT_REPL_VERSION=${AGENT_REPL_VERSION:-latest}
BINDIR=${BINDIR:-${PREFIX:-$HOME/.local}/bin}

case "$(uname -s)" in
    Linux) os=linux ;;
    Darwin) os=darwin ;;
    *) echo "agent-repl: unsupported operating system: $(uname -s)" >&2; exit 1 ;;
esac

case "$(uname -m)" in
    x86_64 | amd64) arch=amd64 ;;
    arm64 | aarch64) arch=arm64 ;;
    *) echo "agent-repl: unsupported architecture: $(uname -m)" >&2; exit 1 ;;
esac

asset="${BINARY}_${os}_${arch}.tar.gz"
if [ "$AGENT_REPL_VERSION" = latest ]; then
    # This is a monorepo: the repo's "latest release" may belong to another
    # tool, and the API does not order releases by version -- it returns them
    # in lexical tag order, where v0.9.0 outranks v0.10.0. Collect every
    # agent-repl release carrying this asset and take the highest version.
    AGENT_REPL_VERSION=v$(curl -fsSL "https://api.github.com/repos/${REPO}/releases?per_page=100" \
        | grep -oE "https://github.com/${REPO}/releases/download/agent-repl(%2F|/)v[0-9]+\.[0-9]+\.[0-9]+/${asset}" \
        | sed -E 's#.*/agent-repl(%2F|/)v##; s#/.*##' \
        | sort -t. -k1,1n -k2,2n -k3,3n \
        | tail -1)
    if [ "$AGENT_REPL_VERSION" = v ]; then
        echo "agent-repl: no agent-repl release with asset ${asset} found" >&2
        exit 1
    fi
fi

# Tags are agent-repl/vX.Y.Z; the slash is percent-encoded in download URLs.
url="https://github.com/${REPO}/releases/download/agent-repl%2F${AGENT_REPL_VERSION}/${asset}"

tmpdir=$(mktemp -d)
trap 'rm -rf "$tmpdir"' 0 HUP INT TERM

curl -fsSL "$url" -o "$tmpdir/$asset"
tar -xzf "$tmpdir/$asset" -C "$tmpdir" "$BINARY"
mkdir -p "$BINDIR"
install -m 0755 "$tmpdir/$BINARY" "$BINDIR/$BINARY"
echo "Installed $BINARY to $BINDIR/$BINARY"
