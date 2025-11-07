package remarkable

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
)

// file types
type FileType string

const (
	PDF  FileType = "pdf"
	EPUB FileType = "epub"
)

// file info for listing
type FileInfo struct {
	UUID string
	Name string
}

// metadata json structure
type Metadata struct {
	LastModified string `json:"lastModified"`
	Type         string `json:"type"`
	Version      int    `json:"version"`
	VisibleName  string `json:"visibleName"`
}

// content json structure
type Content struct {
	FileType   string     `json:"fileType"`
	Transform  *Transform `json:"transform,omitempty"`
	PageCount  int       `json:"pageCount,omitempty"`
	Margins    int       `json:"margins,omitempty"`
	TextScale  int       `json:"textScale,omitempty"`
}

// pdf transform matrix
type Transform struct {
	M11 int `json:"m11"`
	M12 int `json:"m12"`
	M13 int `json:"m13"`
	M21 int `json:"m21"`
	M22 int `json:"m22"`
	M23 int `json:"m23"`
	M31 int `json:"m31"`
	M32 int `json:"m32"`
	M33 int `json:"m33"`
}

// FileExists checks if a file with the given visibleName already exists on reMarkable
// Excludes files in trash
func (c *Client) FileExists(visibleName string) (bool, error) {
	// search for the visible name in metadata files
	cmd := fmt.Sprintf("grep -l \"%s\" %s/*.metadata 2>/dev/null", visibleName, c.Dir)
	output, err := c.RunCommand(cmd)
	if err != nil || strings.TrimSpace(output) == "" {
		return false, nil
	}

	// Check each matching file to see if it's in trash
	files := strings.Split(strings.TrimSpace(output), "\n")
	for _, file := range files {
		// Read the metadata to check if it's in trash
		content, err := c.RunCommand(fmt.Sprintf("cat %s", file))
		if err != nil {
			continue
		}

		// Check if parent is not "trash"
		if !strings.Contains(content, `"parent": "trash"`) {
			return true, nil
		}
	}

	return false, nil
}

func (c *Client) UploadFile(localPath string, visibleName string, forceOverwrite bool) error {
	// check if file already exists
	exists, err := c.FileExists(visibleName)
	if err != nil {
		return fmt.Errorf("failed to check if file exists: %w", err)
	}
	if exists && !forceOverwrite {
		return fmt.Errorf("file '%s' already exists on reMarkable (use --force to overwrite)", visibleName)
	}

	// If forcing overwrite, delete existing file first
	if exists && forceOverwrite {
		if err := c.DeleteFileByName(visibleName); err != nil {
			return fmt.Errorf("failed to delete existing file: %w", err)
		}
	}

	id := uuid.New().String()
	fileType := PDF
	if strings.HasSuffix(strings.ToLower(localPath), ".epub") {
		fileType = EPUB
	}

	metadata := Metadata{
		LastModified: fmt.Sprintf("%d000", time.Now().Unix()),
		Type:         "DocumentType",
		Version:      1,
		VisibleName:  visibleName,
	}

	content := Content{
		FileType: string(fileType),
	}

	if fileType == PDF {
		content.Margins = 100
		content.PageCount = 1
		content.TextScale = 1
		content.Transform = &Transform{
			M11: 1, M12: 0, M13: 0,
			M21: 0, M22: 1, M23: 0,
			M31: 0, M32: 0, M33: 1,
		}
	}

	// temp dir for metadata
	tmpDir, err := os.MkdirTemp("", "remarkable-*")
	if err != nil {
		return fmt.Errorf("failed to create temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	// write metadata
	metadataBytes, err := json.Marshal(metadata)
	if err != nil {
		return fmt.Errorf("failed to marshal metadata: %w", err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, id+".metadata"), metadataBytes, 0644); err != nil {
		return fmt.Errorf("failed to write metadata: %w", err)
	}

	// write content
	contentBytes, err := json.Marshal(content)
	if err != nil {
		return fmt.Errorf("failed to marshal content: %w", err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, id+".content"), contentBytes, 0644); err != nil {
		return fmt.Errorf("failed to write content: %w", err)
	}

	// transfer files
	for _, f := range []struct {
		src, dst string
	}{
		{localPath, filepath.Join(c.Dir, id+"."+string(fileType))},
		{filepath.Join(tmpDir, id+".metadata"), filepath.Join(c.Dir, id+".metadata")},
		{filepath.Join(tmpDir, id+".content"), filepath.Join(c.Dir, id+".content")},
	} {
		if err := c.TransferFile(f.src, f.dst); err != nil {
			return fmt.Errorf("failed to transfer %s: %w", filepath.Base(f.src), err)
		}
	}

	// make required dirs
	for _, dir := range []string{"thumbnails", "highlights", "cache"} {
		if _, err := c.RunCommand(fmt.Sprintf("mkdir -p %s/%s.%s", c.Dir, id, dir)); err != nil {
			return fmt.Errorf("failed to create %s: %w", dir, err)
		}
	}

	return nil
}

func (c *Client) ListFiles() ([]FileInfo, error) {
	// get metadata files
	output, err := c.RunCommand(fmt.Sprintf("ls %s/*.metadata", c.Dir))
	if err != nil {
		return nil, fmt.Errorf("failed to list files: %w", err)
	}

	var files []FileInfo
	for _, metadataPath := range strings.Split(strings.TrimSpace(output), "\n") {
		// get uuid from filename
		uuid := strings.TrimSuffix(filepath.Base(metadataPath), ".metadata")

		// check pdf exists
		if _, err := c.RunCommand(fmt.Sprintf("test -f %s/%s.pdf", c.Dir, uuid)); err != nil {
			continue
		}

		// read metadata
		content, err := c.RunCommand(fmt.Sprintf("cat %s", metadataPath))
		if err != nil {
			continue
		}

		// get visible name
		if matches := regexp.MustCompile(`"visibleName":\s*"([^"]+)"`).FindStringSubmatch(content); len(matches) > 1 {
			files = append(files, FileInfo{
				UUID: uuid,
				Name: matches[1],
			})
		}
	}

	return files, nil
}

func (c *Client) DownloadFile(uuid, name string) (string, error) {
	tmpDir, err := os.MkdirTemp("", "remarkable-*")
	if err != nil {
		return "", fmt.Errorf("failed to create temp dir: %w", err)
	}

	localPath := filepath.Join(tmpDir, name+".pdf")
	remotePath := filepath.Join(c.Dir, uuid+".pdf")
	if err := c.DownloadFromRemote(remotePath, localPath); err != nil {
		os.RemoveAll(tmpDir)
		return "", fmt.Errorf("failed to download file: %w", err)
	}

	return localPath, nil
}

func (c *Client) RemoveFile(uuid string) error {
	cmd := fmt.Sprintf("rm -rf %s/%s*", c.Dir, uuid)
	if _, err := c.RunCommand(cmd); err != nil {
		return fmt.Errorf("failed to remove file: %w", err)
	}
	return nil
}

// DeleteFileByName deletes all files (including those in trash) with the given visible name
func (c *Client) DeleteFileByName(visibleName string) error {
	// Get all metadata files first
	allFiles, err := c.RunCommand(fmt.Sprintf("ls %s/*.metadata 2>/dev/null", c.Dir))
	if err != nil || strings.TrimSpace(allFiles) == "" {
		return nil // No files
	}

	// Check each file individually for exact name match (not grep pattern)
	filePaths := strings.Split(strings.TrimSpace(allFiles), "\n")
	for _, filePath := range filePaths {
		// Read metadata
		content, err := c.RunCommand(fmt.Sprintf("cat %s", filePath))
		if err != nil {
			continue
		}

		// Parse JSON to check visibleName - exact match only
		var metadata Metadata
		if err := json.Unmarshal([]byte(content), &metadata); err != nil {
			continue
		}

		// Only delete if exact match
		if metadata.VisibleName == visibleName {
			uuid := strings.TrimSuffix(filepath.Base(filePath), ".metadata")
			if err := c.RemoveFile(uuid); err != nil {
				return err
			}
		}
	}

	return nil
}

func (c *Client) CleanupExcept(pattern string) error {
	// list metadata files and find ones to preserve
	output, err := c.RunCommand(fmt.Sprintf("ls %s/*.metadata", c.Dir))
	if err != nil {
		return fmt.Errorf("failed to list files: %w", err)
	}

	// track uuids to preserve
	preserveUUIDs := make(map[string]bool)
	for _, metadataPath := range strings.Split(strings.TrimSpace(output), "\n") {
		content, err := c.RunCommand(fmt.Sprintf("cat %s", metadataPath))
		if err != nil {
			continue
		}

		if matched, _ := regexp.MatchString(pattern, content); matched {
			uuid := strings.TrimSuffix(filepath.Base(metadataPath), ".metadata")
			preserveUUIDs[uuid] = true
		} else {
			// remove unpreserved files immediately
			uuid := strings.TrimSuffix(filepath.Base(metadataPath), ".metadata")
			if err := c.RemoveFile(uuid); err != nil {
				return fmt.Errorf("failed to remove %s: %w", uuid, err)
			}
		}
	}

	return nil
}
