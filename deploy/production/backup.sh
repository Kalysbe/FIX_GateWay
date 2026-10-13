#!/bin/sh
set -eu
set -o pipefail
umask 077
: "${BACKUP_AGE_RECIPIENT:?provide the operator's age public encryption recipient}"
: "${DATABASE_URL_FILE:?database secret file required}"
interval="${BACKUP_INTERVAL_SECONDS:-3600}"
case "$interval" in ''|*[!0-9]*) exit 2 ;; esac
[ "$interval" -ge 60 ] || exit 2
mkdir -p /backups
while :; do
  stamp="$(date -u +%Y%m%dT%H%M%SZ)"
  pending="/backups/gateway-$stamp.dump.age.pending"
  final="/backups/gateway-$stamp.dump.age"
  if pg_dump --dbname="$(cat "$DATABASE_URL_FILE")" --format=custom --no-owner | age -r "$BACKUP_AGE_RECIPIENT" -o "$pending"; then
    mv "$pending" "$final"
    date -u +%s > /backups/last-success
    find /backups -maxdepth 1 -type f -name 'gateway-*.dump.age' -mtime +7 -delete
    echo "Encrypted database backup completed: $stamp"
  else
    rm -f "$pending"
    echo "Database backup failed" >&2
  fi
  sleep "$interval"
done
