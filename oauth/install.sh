#!/bin/sh
set -eu

REPO=ikigenba/ikigenba
BINARY=oauth
OAUTH_VERSION=${OAUTH_VERSION:-latest}
BINDIR=${BINDIR:-${PREFIX:-$HOME/.local}/bin}

case "$(uname -s)" in
    Linux) os=linux ;;
    Darwin) os=darwin ;;
    *) echo "oauth: unsupported operating system: $(uname -s)" >&2; exit 1 ;;
esac

case "$(uname -m)" in
    x86_64 | amd64) arch=amd64 ;;
    arm64 | aarch64) arch=arm64 ;;
    *) echo "oauth: unsupported architecture: $(uname -m)" >&2; exit 1 ;;
esac

asset="${BINARY}_${os}_${arch}.tar.gz"
if [ "$OAUTH_VERSION" = latest ]; then
    # This is a monorepo: the repo's "latest release" may belong to another
    # tool, and the API does not order releases by version -- it returns them
    # in lexical tag order, where v0.9.0 outranks v0.10.0. Collect every
    # oauth release carrying this asset and take the highest version.
    OAUTH_VERSION=v$(curl -fsSL "https://api.github.com/repos/${REPO}/releases?per_page=100" \
        | grep -oE "https://github.com/${REPO}/releases/download/oauth(%2F|/)v[0-9]+\.[0-9]+\.[0-9]+/${asset}" \
        | sed -E 's#.*/oauth(%2F|/)v##; s#/.*##' \
        | sort -t. -k1,1n -k2,2n -k3,3n \
        | tail -1)
    if [ "$OAUTH_VERSION" = v ]; then
        echo "oauth: no oauth release with asset ${asset} found" >&2
        exit 1
    fi
fi

# Tags are oauth/vX.Y.Z; the slash is percent-encoded in download URLs.
url="https://github.com/${REPO}/releases/download/oauth%2F${OAUTH_VERSION}/${asset}"

tmpdir=$(mktemp -d)
trap 'rm -rf "$tmpdir"' 0 HUP INT TERM

curl -fsSL "$url" -o "$tmpdir/$asset"
tar -xzf "$tmpdir/$asset" -C "$tmpdir" "$BINARY"
mkdir -p "$BINDIR"
install -m 0755 "$tmpdir/$BINARY" "$BINDIR/$BINARY"
echo "Installed $BINARY to $BINDIR/$BINARY"

case ":${PATH:-}:" in
    *:"$BINDIR":*) ;;
    *) echo "oauth: warning: $BINDIR is not on PATH" >&2 ;;
esac
