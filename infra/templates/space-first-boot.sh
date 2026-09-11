#!/bin/bash
# First boot of a space host. Packages only; the host learns nothing about
# itself here. Managed by Terraform via the ikigenba-space launch template.
set -euo pipefail
dnf install -y -q nginx certbot awscli-2 jq
systemctl enable nginx
