#!/bin/bash
# Deploy static content from dnd/html/ to the remote instance
# Run locally: ./scripts/dnd/deploy.sh
set -euo pipefail

SSH_KEY="$HOME/.ssh/id_ed25519_ai4mgreenly"
HOST="dnd.prod.metaspot.org"
REMOTE_DIR="/var/www/dnd/"
LOCAL_DIR="/mnt/projects/dnd/html/"

rsync -avz --delete \
  -e "ssh -i ${SSH_KEY}" \
  "$LOCAL_DIR" \
  "ec2-user@${HOST}:${REMOTE_DIR}"
