#!/bin/bash
# First boot of an SSH host. Installs and starts accountsd and nothing else:
# the daemon reconciles packages, users, slices and the rest. Managed by
# Terraform via the <domain>-hosts launch template.
#
# accounts tags the instance AccountsURL=<accounts base URL> at launch; the
# daemon binary is served at <AccountsURL>/accountsd. Both are retried until
# they succeed, so a host launched while accounts is unreachable comes up once
# it is back.
set -euo pipefail

imds=http://169.254.169.254/latest
until token=$(curl -fsS --max-time 5 -X PUT "$imds/api/token" -H "X-aws-ec2-metadata-token-ttl-seconds: 300") &&
  url=$(curl -fsS --max-time 5 -H "X-aws-ec2-metadata-token: $token" "$imds/meta-data/tags/instance/AccountsURL"); do
  sleep 5
done
url=${url%/}

tmp=/usr/local/bin/.accountsd.download
# No redirects and HTTPS only; anything but an ELF binary is retried, so a
# sign-in page is never installed as the daemon.
until curl -fsS --proto =https --connect-timeout 5 --max-time 300 -o "$tmp" "$url/accountsd" &&
  [ "$(head -c4 "$tmp" | tail -c3)" = ELF ]; do
  sleep 10
done
chmod 0755 "$tmp"
mv -f "$tmp" /usr/local/bin/accountsd

printf 'ACCOUNTS_URL=%s\n' "$url" > /etc/accountsd.env
chmod 0644 /etc/accountsd.env

cat > /etc/systemd/system/accountsd.service <<'UNIT'
[Unit]
Description=accountsd: reconciles this host with accounts
Wants=network-online.target
After=network-online.target

[Service]
EnvironmentFile=/etc/accountsd.env
ExecStart=/usr/local/bin/accountsd
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
UNIT

systemctl daemon-reload
systemctl enable --now accountsd.service
