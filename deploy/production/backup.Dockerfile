FROM postgres:16.15-alpine3.24@sha256:721873c34ceb9f8d8fc265984940dc982404c105f19ad51be9fdc5970a6080ea
RUN apk add --no-cache age && mkdir /backups && chown 10001:10001 /backups
COPY backup.sh /app/backup.sh
COPY backup-health.sh /app/backup-health.sh
USER 10001:10001
ENTRYPOINT ["/bin/sh", "/app/backup.sh"]
