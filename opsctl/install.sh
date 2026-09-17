#!/usr/bin/env bash

set -u

program=opsctl-install
repository=https://github.com/ikigenba/ikigenba/releases/download/opsctl
binary_path=/usr/local/bin/opsctl
installer_path=/usr/local/share/ikigenba/opsctl-install.sh

if [[ $# -eq 0 ]]; then
  printf '%s: needs a version\n' "$program" >&2
  exit 2
fi
if [[ $# -ne 1 ]]; then
  printf '%s: needs exactly one version\n' "$program" >&2
  exit 2
fi

version=$1
if [[ $EUID -ne 0 ]]; then
  printf '%s: must run as root\n' "$program" >&2
  exit 3
fi

asset=opsctl-$version-linux-amd64
release_url=$repository/$version
scratch=/tmp/opsctl-install.$$
downloaded_binary=$scratch/$asset
candidate=$scratch/opsctl
checksums=$scratch/checksums.txt
candidate_stderr=$scratch/candidate.stderr
downloaded_installer=$scratch/install.sh
staged_binary=/usr/local/bin/.opsctl-install.$$
staged_installer=/usr/local/share/ikigenba/.opsctl-install.$$

cleanup() {
  rm -rf -- "$scratch"
  rm -f -- "$staged_binary" "$staged_installer"
}
trap cleanup EXIT HUP INT TERM

fail_install() {
  local reason=$1
  reason=${reason//$'\r'/\\r}
  reason=${reason//$'\n'/\\n}
  printf 'install: failed: %s\n' "$reason"
  printf '%s: installation failed\n' "$program" >&2
  exit 1
}

if ! install -d -m 0700 -- "$scratch"; then
  fail_install 'could not create scratch directory'
fi

if ! curl --fail --silent --show-error --location --output "$checksums" "$release_url/checksums.txt"; then
  fail_install "could not download checksums for $version"
fi
if ! curl --fail --silent --show-error --location --output "$downloaded_binary" "$release_url/$asset"; then
  fail_install "could not download $asset"
fi

expected=
matches=0
while IFS= read -r line || [[ -n $line ]]; do
  if [[ $line =~ ^([[:xdigit:]]{64})[[:space:]][[:space:]](.+)$ ]] && [[ ${BASH_REMATCH[2]} == "$asset" ]]; then
    expected=${BASH_REMATCH[1],,}
    ((matches += 1))
  fi
done < "$checksums"
if [[ $matches -ne 1 ]]; then
  fail_install "no checksum for $asset"
fi

actual=
read -r actual _ < <(sha256sum "$downloaded_binary")
actual=${actual,,}
if [[ $actual != "$expected" ]]; then
  printf 'install: failed: checksum mismatch for %s\n' "$asset"
  printf '%s: installation failed\n\nexpected %s\n     got %s\n' "$program" "$expected" "$actual" >&2
  exit 1
fi

if ! install -m 0755 -- "$downloaded_binary" "$candidate"; then
  fail_install "could not prepare $asset"
fi
reported=
if ! reported=$("$candidate" version 2>"$candidate_stderr"); then
  fail_install "$asset version command failed"
fi
if [[ $reported != "$version" ]]; then
  printf 'install: failed: asked for %s but the binary reports %s\n' "$version" "$reported"
  printf '%s: installation failed\n' "$program" >&2
  exit 1
fi

installed_version=
if [[ -x $binary_path ]]; then
  installed_version=$("$binary_path" version 2>/dev/null || true)
fi
had_saved=0
if [[ -f $installer_path ]]; then
  had_saved=1
fi
if [[ $had_saved -eq 1 && $installed_version != "$version" ]]; then
  if ! curl --fail --silent --show-error --location --output "$downloaded_installer" "$release_url/install.sh"; then
    fail_install "could not download install.sh for $version"
  fi
fi

if ! install -d -m 0755 -- /usr/local/bin /usr/local/share/ikigenba; then
  fail_install 'could not create installation directories'
fi
if ! install -m 0755 -- "$candidate" "$staged_binary"; then
  fail_install 'could not stage opsctl'
fi
if [[ $had_saved -eq 0 ]]; then
  if ! install -m 0755 -- "$0" "$staged_installer"; then
    fail_install 'could not stage installer'
  fi
elif [[ $installed_version != "$version" ]]; then
  if ! install -m 0755 -- "$downloaded_installer" "$staged_installer"; then
    fail_install 'could not stage installer'
  fi
fi

if ! mv -f -- "$staged_binary" "$binary_path"; then
  fail_install 'could not publish opsctl'
fi
if [[ -f $staged_installer ]]; then
  if ! mv -f -- "$staged_installer" "$installer_path"; then
    fail_install 'could not publish installer'
  fi
fi

printf 'install: ok (opsctl %s -> /usr/local/bin/opsctl)\n' "$version"
