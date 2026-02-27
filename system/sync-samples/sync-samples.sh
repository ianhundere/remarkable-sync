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

# Wait for the drive to mount (fstab handles it, but give it time)
for i in $(seq 1 60); do
    mountpoint -q "$SRC" && break
    # Try explicit mount after 10s in case fstab automount is slow
    if [ "$i" -eq 10 ]; then
        mount "$SRC" 2>/dev/null
    fi
    sleep 1
done

if ! mountpoint -q "$SRC"; then
    log "ERROR: $SRC not mounted after 60s, aborting"
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
FAILED=0

# Sync top-level items individually to keep memory low
for item in "$SRC"*; do
    basename="$(basename "$item")"
    # Skip trash and macOS dot files
    [[ "$basename" == .Trash-1000 ]] && continue
    [[ "$basename" == .* ]] && continue

    rsync -r --bwlimit=10000 --exclude='.Trash-1000' --exclude='._*' "$item" "$DST" > /dev/null 2>&1
    if [ $? -ne 0 ]; then
        log "ERROR: failed to sync $basename"
        FAILED=1
    fi
done

if [ $FAILED -eq 0 ]; then
    log "Sync completed successfully"
else
    log "Sync completed with errors"
fi

exit $FAILED
