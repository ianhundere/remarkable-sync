# remarkable-sync

A comprehensive CLI tool for bidirectional syncing between Obsidian vault and reMarkable tablet. Convert markdown files to beautifully formatted PDFs, upload documents with folder organization, and extract annotated PDFs back to markdown.

## Features

- **Bidirectional Sync**: Upload files to reMarkable and download annotated PDFs back to Obsidian
- **Markdown to PDF**: Convert Obsidian markdown files to formatted PDFs with customizable styling
- **Folder Organization**: Upload files to specific folders on your reMarkable (creates folders automatically)
- **PDF Text Extraction**: Convert PDFs from reMarkable back to markdown with YAML frontmatter
- **Safe Cleanup**: Remove files from reMarkable with pattern-based preservation and dry-run mode
- **Batch Operations**: Upload multiple files or entire directories at once
- **Customizable PDF Generation**: Control fonts, sizes, margins, colors, and table of contents

## Installation

### Build from Source

```bash
make build
```

This creates the `remarkable-sync` executable in the project root.

### Requirements

- Go 1.24+
- SSH access to your reMarkable tablet
- reMarkable tablet connected to the same network

## Quick Start

```bash
# Upload a PDF to reMarkable
./remarkable-sync to-remarkable document.pdf

# Upload files to a specific folder
./remarkable-sync to-remarkable --folder "Work Documents" document.pdf

# Convert markdown to PDF and upload
./remarkable-sync obsidian note.md

# Download and convert PDFs from reMarkable to markdown
./remarkable-sync from-remarkable

# Clean up reMarkable, keeping specific files
./remarkable-sync cleanup --except "Quick sheets|Important"
```

## Usage

### Global Flags

Available for all commands:

- `--host string` - reMarkable tablet hostname/IP (default: "remarkable")
- `--remarkable-dir string` - reMarkable documents directory (default: "/home/root/.local/share/remarkable/xochitl")
- `-f, --force` - Overwrite existing files without prompting
- `-q, --quiet` - Suppress non-error output
- `-r, --restart` - Restart xochitl after transfer (default: true)

### Commands

#### `to-remarkable` - Upload Files

Transfer PDF and EPUB files to reMarkable tablet. Supports individual files or entire directories.

```bash
# Upload single file
./remarkable-sync to-remarkable document.pdf

# Upload multiple files
./remarkable-sync to-remarkable file1.pdf file2.epub

# Upload entire directory
./remarkable-sync to-remarkable ~/Documents/papers/

# Upload to specific folder (creates if doesn't exist)
./remarkable-sync to-remarkable --folder "Research" paper.pdf

# Upload from Obsidian vault (if no arguments provided)
./remarkable-sync to-remarkable
```

**Flags:**

- `--folder string` - Upload files to this folder on reMarkable

#### `obsidian` - Markdown to PDF

Convert markdown files from Obsidian vault to PDF and upload to reMarkable.

```bash
# Convert and upload markdown file
./remarkable-sync obsidian note.md

# Convert multiple files
./remarkable-sync obsidian note1.md note2.md

# Convert directory of markdown files
./remarkable-sync obsidian ~/notes/projects/

# Upload to specific folder with custom styling
./remarkable-sync obsidian --folder "Notes" --pdf-fontsize 12 note.md
```

**PDF Styling Flags:**

- `--pdf-font string` - Main font (default: "Arial")
- `--pdf-monofont string` - Monospace font for code (default: "Courier")
- `--pdf-fontsize float` - Base font size (default: 11)
- `--pdf-margins float` - Page margins in mm (default: 20)
- `--pdf-pagesize string` - Page size (default: "A4")
- `--pdf-toc` - Include table of contents (default: true)
- `--pdf-colorlinks` - Use colored links (default: true)
- `--pdf-highlight` - Highlight code blocks (default: true)
- `--vault string` - Path to Obsidian vault (default: "/Users/ianfundere/notes")
- `--folder string` - Upload files to this folder on reMarkable

#### `from-remarkable` - Download and Convert

Download PDFs from reMarkable and convert them to markdown in your Obsidian vault.

```bash
# Download all PDFs from reMarkable
./remarkable-sync from-remarkable

# Customize markdown conversion
./remarkable-sync from-remarkable --md-header-adjust 2

# Skip frontmatter
./remarkable-sync from-remarkable --md-frontmatter=false
```

**Flags:**

- `--vault string` - Path to Obsidian vault (default: "/Users/ianfundere/notes")
- `--md-frontmatter` - Add YAML frontmatter (default: true)
- `--md-cleanup` - Clean up extracted text (default: true)
- `--md-header-adjust int` - Adjust header levels (default: 1)

#### `cleanup` - Safe Removal

Remove files from reMarkable with pattern-based preservation and dry-run capability.

```bash
# Preview what would be deleted (dry-run)
./remarkable-sync cleanup --dry-run

# Clean up everything except specific patterns
./remarkable-sync cleanup --except "Quick sheets|Notebook tutorial"

# Clean up with force (no prompts)
./remarkable-sync cleanup --except "Important" --force
```

**Flags:**

- `--except string` - Pattern to preserve (supports regex patterns separated by |)
- `--dry-run` - Preview what would be deleted without actually deleting

#### `remove` - Remove Single File

Remove a specific file from reMarkable by its visible name.

```bash
# Remove a specific file
./remarkable-sync remove "Meeting Notes"

# Remove with force (no prompt)
./remarkable-sync remove "Old Document" --force
```

## Configuration

### SSH Access

Ensure you have SSH access to your reMarkable tablet. You can set up passwordless SSH using:

```bash
ssh-keygen -t rsa
ssh-copy-id root@remarkable
```

### Custom Hostname

If your reMarkable has a different hostname or IP:

```bash
./remarkable-sync --host 10.11.99.1 to-remarkable document.pdf
```

### Obsidian Vault Path

Set your default Obsidian vault path:

```bash
./remarkable-sync obsidian --vault ~/my-vault note.md
```

## Examples

### Complete Workflow

```bash
# 1. Convert your notes to PDF and upload to reMarkable
./remarkable-sync obsidian --folder "Daily Notes" daily-note.md

# 2. Read and annotate on your reMarkable

# 3. Download annotated PDFs back to Obsidian as markdown
./remarkable-sync from-remarkable

# 4. Clean up old files, keeping important ones
./remarkable-sync cleanup --except "Quick sheets|Templates" --dry-run
./remarkable-sync cleanup --except "Quick sheets|Templates"
```

### Batch Upload Research Papers

```bash
# Upload entire directory of papers to organized folder
./remarkable-sync to-remarkable --folder "Research/AI Papers" ~/Downloads/papers/
```

### Custom PDF Styling for Markdown

```bash
# Create large-font PDFs with custom margins
./remarkable-sync obsidian \
  --pdf-fontsize 14 \
  --pdf-margins 25 \
  --pdf-pagesize "Letter" \
  --folder "Reading" \
  article.md
```

## Troubleshooting

### Connection Issues

If you can't connect to your reMarkable:

```bash
# Test SSH connection
ssh root@remarkable

# Try with IP address instead
./remarkable-sync --host 10.11.99.1 to-remarkable test.pdf
```

### Files Not Appearing

The reMarkable interface may need to be restarted. The tool does this automatically, but you can disable it:

```bash
./remarkable-sync to-remarkable --restart=false document.pdf
```

## License

See [LICENSE](LICENSE) file for details.

## Contributing

Contributions welcome! Please feel free to submit issues or pull requests.
