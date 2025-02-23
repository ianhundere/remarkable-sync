package remarkable

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/cheggaaa/pb/v3"
	"golang.org/x/crypto/ssh"
)

const (
	USB_HOST  = "10.11.99.1"
	WIFI_HOST = "remarkable"
	TIMEOUT   = 5 * time.Second
)

// ssh client for remarkable
type Client struct {
	Host   string
	Dir    string
	client *ssh.Client
	config *ssh.ClientConfig
}

func NewClient(host, dir string) (*Client, error) {
	config := &ssh.ClientConfig{
		User: "root",
		Auth: []ssh.AuthMethod{
			ssh.Password(""), // empty password
		},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         TIMEOUT,
	}

	// try usb first
	client := &Client{
		Host:   USB_HOST,
		Dir:    dir,
		config: config,
	}
	if err := client.connect(); err == nil {
		return client, nil
	}

	// try wifi
	client.Host = WIFI_HOST
	if host != "" {
		client.Host = host
	}
	if err := client.connect(); err != nil {
		return nil, fmt.Errorf("failed to connect via USB (%s) or WiFi (%s)", USB_HOST, client.Host)
	}

	return client, nil
}

func (c *Client) connect() error {
	var err error
	c.client, err = ssh.Dial("tcp", c.Host+":22", c.config)
	return err
}

func (c *Client) Close() error {
	if c.client != nil {
		return c.client.Close()
	}
	return nil
}

func (c *Client) RunCommand(cmd string) (string, error) {
	session, err := c.client.NewSession()
	if err != nil {
		return "", fmt.Errorf("failed to create session: %w", err)
	}
	defer session.Close()

	output, err := session.CombinedOutput(cmd)
	if err != nil {
		return "", fmt.Errorf("command failed: %w", err)
	}

	return string(output), nil
}

func (c *Client) TransferFile(localPath, remotePath string) error {
	session, err := c.client.NewSession()
	if err != nil {
		return fmt.Errorf("failed to create session: %w", err)
	}
	defer session.Close()

	f, err := os.Open(localPath)
	if err != nil {
		return fmt.Errorf("failed to open local file: %w", err)
	}
	defer f.Close()

	stat, err := f.Stat()
	if err != nil {
		return fmt.Errorf("failed to stat file: %w", err)
	}

	// show progress
	bar := pb.Full.Start64(stat.Size())
	bar.Set(pb.Bytes, true)
	defer bar.Finish()

	// wrap reader with progress
	reader := bar.NewProxyReader(f)

	done := make(chan error, 1)
	go func() {
		w, _ := session.StdinPipe()
		defer w.Close()

		fmt.Fprintf(w, "C%#o %d %s\n", stat.Mode().Perm(), stat.Size(), filepath.Base(remotePath))
		_, err := io.Copy(w, reader)
		done <- err
	}()

	// wait with timeout
	select {
	case err := <-done:
		if err != nil {
			return fmt.Errorf("transfer failed: %w", err)
		}
	case <-time.After(TIMEOUT):
		return fmt.Errorf("transfer timed out after %v", TIMEOUT)
	}

	if err := session.Run(fmt.Sprintf("/usr/bin/scp -t %s", remotePath)); err != nil {
		return fmt.Errorf("scp failed: %w", err)
	}

	return nil
}
