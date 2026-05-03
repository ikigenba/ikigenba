#!/bin/bash
# One-time setup for dnd.prod.metaspot.org
# Run remotely: ssh -i ~/.ssh/id_ed25519_ai4mgreenly ec2-user@dnd.prod.metaspot.org 'sudo bash -s' < scripts/dnd/setup.sh
set -euo pipefail

DOMAIN="dnd.prod.metaspot.org"
WEBROOT="/var/www/dnd"
EMAIL="claude@logic-refinery.com"

# --- Install packages ---
dnf install -y nginx certbot python3-certbot-nginx

# --- Create content directory ---
mkdir -p "$WEBROOT"
chown ec2-user:ec2-user "$WEBROOT"

# --- Nginx config (HTTP only — certbot will add SSL) ---
cat > /etc/nginx/conf.d/dnd.conf <<EOF
server {
    listen 80;
    server_name ${DOMAIN};
    root ${WEBROOT};
    index index.html;
}
EOF

rm -f /etc/nginx/conf.d/default.conf
nginx -t
systemctl enable nginx
systemctl start nginx

# --- Let's Encrypt cert ---
certbot --nginx -d "$DOMAIN" --non-interactive --agree-tos -m "$EMAIL"

# certbot installs a renewal timer by default
systemctl enable certbot-renew.timer
systemctl start certbot-renew.timer

echo "Setup complete. Deploy content to ${WEBROOT} and it will be served at https://${DOMAIN}"
