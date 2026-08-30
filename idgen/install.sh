#!/bin/sh
set -eu

REPO=ikigenba/ikigenba
BINARY=idgen
IDGEN_VERSION=${IDGEN_VERSION:-latest}
BINDIR=${BINDIR:-${PREFIX:-$HOME/.local}/bin}

case "$(uname -s)" in
    Linux) os=linux ;;
    Darwin) os=darwin ;;
    *) echo "idgen: unsupported operating system: $(uname -s)" >&2; exit 1 ;;
esac

case "$(uname -m)" in
    x86_64 | amd64) arch=amd64 ;;
    arm64 | aarch64) arch=arm64 ;;
    *) echo "idgen: unsupported architecture: $(uname -m)" >&2; exit 1 ;;
esac

asset="${BINARY}_${os}_${arch}.tar.gz"
if [ "$IDGEN_VERSION" = latest ]; then
    # This is a monorepo: the repo's "latest release" may belong to another
    # tool, so resolve the newest idgen/v* release through the API instead of
    # the releases/latest shortcut.
    url=$(curl -fsSL "https://api.github.com/repos/${REPO}/releases?per_page=100" \
        | grep -oE "https://github.com/${REPO}/releases/download/idgen(%2F|/)v[^\"]*/${asset}" \
        | head -1)
    if [ -z "$url" ]; then
        echo "idgen: no idgen release with asset ${asset} found" >&2
        exit 1
    fi
else
    # Tags are idgen/vX.Y.Z; the slash is percent-encoded in download URLs.
    url="https://github.com/${REPO}/releases/download/idgen%2F${IDGEN_VERSION}/${asset}"
fi

tmpdir=$(mktemp -d)
trap 'rm -rf "$tmpdir"' 0 HUP INT TERM

curl -fsSL "$url" -o "$tmpdir/$asset"
tar -xzf "$tmpdir/$asset" -C "$tmpdir" "$BINARY"
mkdir -p "$BINDIR"
install -m 0755 "$tmpdir/$BINARY" "$BINDIR/$BINARY"
echo "Installed $BINARY to $BINDIR/$BINARY"
