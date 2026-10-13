#!/bin/sh
set -eu
[ -s /backups/last-success ] || exit 1
last="$(cat /backups/last-success)"
now="$(date -u +%s)"
[ "$((now - last))" -le "$((${BACKUP_INTERVAL_SECONDS:-3600} * 2))" ]
