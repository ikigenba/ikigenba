#!/usr/bin/env bash

set -euo pipefail

if [[ $# -ne 1 ]]; then
  echo "usage: release.sh opsctl/vMAJOR.MINOR.PATCH" >&2
  exit 2
fi

tag=$1
if [[ $tag =~ ^opsctl/(v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*))$ ]]; then
  version=${BASH_REMATCH[1]}
else
  echo "invalid opsctl release tag: $tag" >&2
  exit 2
fi

project_dir=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
dist=${OPSCTL_RELEASE_DIST:-"$project_dir/dist"}
mkdir -p -- "$dist"
if find "$dist" -mindepth 1 -print -quit | grep -q .; then
  echo "release output directory is not empty: $dist" >&2
  exit 1
fi

binary="opsctl-$version-linux-amd64"
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
  go build -trimpath -o "$dist/$binary" "$project_dir/cmd/opsctl"

reported=$("$dist/$binary" version)
if [[ $reported != "$version" ]]; then
	rm -f -- "$dist/$binary"
  echo "tag $tag names $version but the binary reports $reported" >&2
  exit 1
fi

(
  cd -- "$dist"
  sha256sum "$binary" > checksums.txt
)
install -m 0755 "$project_dir/install.sh" "$dist/install.sh"

gh release create "$tag" \
  --title "$tag" \
  --verify-tag \
  --generate-notes \
  "$dist/$binary" \
  "$dist/checksums.txt" \
  "$dist/install.sh"
