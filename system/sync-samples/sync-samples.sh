#!/bin/bash
# Sync samples from external Samsung T7 to NAS
# Triggered by udev when drive is plugged in

LOGFILE="/var/log/sync-samples.log"
SRC="/mnt/samps/"
DST="/mnt/sound/samples/"

log() {
    echo "$(date '+%Y-%m-%d %H:%M:%S') $1" >> "$LOGFILE"
}

log "Sync triggered — drive plugged in"

# Wait for the drive to mount (fstab handles it, but give it a moment)
for i in $(seq 1 30); do
    mountpoint -q "$SRC" && break
    sleep 1
done

if ! mountpoint -q "$SRC"; then
    log "ERROR: $SRC not mounted after 30s, aborting"
    exit 1
fi

# Check if NAS is reachable
if ! ping -c 1 -W 3 192.168.3.85 > /dev/null 2>&1; then
    log "NAS not reachable, skipping sync"
    exit 0
fi

# Check if NAS mount is available
if ! mountpoint -q /mnt/sound; then
    log "NAS mount /mnt/sound not available, attempting mount"
    mount /mnt/sound 2>/dev/null
    sleep 2
    if ! mountpoint -q /mnt/sound; then
        log "ERROR: Could not mount /mnt/sound, aborting"
        exit 1
    fi
fi

log "Starting rsync..."
rsync -r --exclude='.Trash-1000' --exclude='._*' "$SRC" "$DST" >> "$LOGFILE" 2>&1
EXIT_CODE=$?

if [ $EXIT_CODE -eq 0 ]; then
    log "Sync completed successfully"
else
    log "Sync failed with exit code $EXIT_CODE"
fi

exit $EXIT_CODE
