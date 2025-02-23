# remarkable-sync

CLI tool for syncing files between Obsidian and reMarkable tablet.

## Install

```bash
make build
```

## Usage

```bash
./remarkable-sync to-remarkable [files...]    # Upload files to reMarkable
./remarkable-sync from-remarkable             # Download from reMarkable to Obsidian
./remarkable-sync obsidian [files...]         # Convert and upload markdown
./remarkable-sync cleanup --except "pattern"  # Clean reMarkable files
```

## Requirements

- Go 1.21+
- SSH access to reMarkable
