# Sample Sync (Samsung T7 → NAS)

Auto-syncs `/mnt/samps/` (external Samsung T7 SSD) to `/mnt/sound/samples/` (NAS at 192.168.3.85) when the drive is plugged in. Skips sync silently if NAS is unreachable.

## Install

```bash
sudo cp sync-samples.sh /usr/local/bin/sync-samples.sh
sudo chmod +x /usr/local/bin/sync-samples.sh
sudo cp sync-samples.service /etc/systemd/system/sync-samples.service
sudo cp 99-sync-samples.rules /etc/udev/rules.d/99-sync-samples.rules
sudo systemctl daemon-reload
sudo udevadm control --reload-rules
```

## Manual trigger

```bash
sudo systemctl start sync-samples.service
```

## Logs

```bash
cat /var/log/sync-samples.log
```
