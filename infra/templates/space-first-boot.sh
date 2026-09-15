#!/bin/bash
# First boot of a space host. Packages only; the host learns nothing about
# itself here. Managed by Terraform via the ikigenba-space launch template.
set -euo pipefail
dnf install -y -q nginx certbot awscli-2 jq
systemctl enable nginx

# litestream is not in the distribution's repositories. Its RPM brings the
# binary (/usr/bin/litestream) and litestream.service; opsctl init writes
# /etc/litestream.yml and enables the unit, so nothing is enabled here.
litestream_version=0.5.17
litestream_sha256=70c85d09df1e9d2ee6a0fb67914133aff38f73cda23330b99b8f6b425760abe1
rpm=/tmp/litestream-${litestream_version}-linux-x86_64.rpm
curl -fsSL -o "$rpm" "https://github.com/benbjohnson/litestream/releases/download/v${litestream_version}/litestream-${litestream_version}-linux-x86_64.rpm"
echo "${litestream_sha256}  ${rpm}" | sha256sum -c --quiet
dnf install -y -q "$rpm"
rm -f "$rpm"
