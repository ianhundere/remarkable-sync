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

func (c *Client) UploadFile(localPath string, visibleName string) error {
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
		{localPath, filepath.Join(c.Dir, id+string(fileType))},
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

func (c *Client) ListFiles() ([]string, error) {
	// get metadata files
	output, err := c.RunCommand(fmt.Sprintf("ls %s/*.metadata", c.Dir))
	if err != nil {
		return nil, fmt.Errorf("failed to list files: %w", err)
	}

	var files []string
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
			files = append(files, matches[1])
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
	if err := c.TransferFile(filepath.Join(c.Dir, uuid+".pdf"), localPath); err != nil {
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
