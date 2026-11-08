#!/bin/bash
# First boot of a space host. Packages only; the host learns nothing about
# itself here. Managed by Terraform via the ikigenba-space launch template.
set -euo pipefail
dnf install -y -q nginx certbot awscli-2 jq git python3.12
systemctl enable nginx

# litestream is not in the distribution's repositories. Its RPM brings the
# binary (/usr/bin/litestream) and litestream.service; opsctl init writes
# /etc/litestream.yml and enables the unit, so nothing is enabled here.
litestream_version=0.5.17
litestream_sha256=9c4c0fdc39d44c2dcd3d1d8f9d783e906208d35a25ec2d8f0f8002e7e8e423d9
rpm=/tmp/litestream-${litestream_version}-linux-arm64.rpm
curl -fsSL -o "$rpm" "https://github.com/benbjohnson/litestream/releases/download/v${litestream_version}/litestream-${litestream_version}-linux-arm64.rpm"
echo "${litestream_sha256}  ${rpm}" | sha256sum -c --quiet
dnf install -y -q "$rpm"
rm -f "$rpm"
