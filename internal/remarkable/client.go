package remarkable

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

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

func getSSHAuthMethods() []ssh.AuthMethod {
	var authMethods []ssh.AuthMethod

	// try SSH keys from default locations
	homeDir, err := os.UserHomeDir()
	if err == nil {
		keyFiles := []string{
			filepath.Join(homeDir, ".ssh", "id_rsa"),
			filepath.Join(homeDir, ".ssh", "id_ed25519"),
			filepath.Join(homeDir, ".ssh", "id_ecdsa"),
		}

		for _, keyFile := range keyFiles {
			if key, err := os.ReadFile(keyFile); err == nil {
				if signer, err := ssh.ParsePrivateKey(key); err == nil {
					authMethods = append(authMethods, ssh.PublicKeys(signer))
				}
			}
		}
	}

	// fallback to empty password
	authMethods = append(authMethods, ssh.Password(""))

	return authMethods
}

// test if we can connect to remarkable using system ssh
func testConnection(host string) error {
	cmd := exec.Command("ssh", "-o", "ConnectTimeout=2", fmt.Sprintf("root@%s", host), "exit")
	return cmd.Run()
}

func NewClient(host, dir string) (*Client, error) {
	config := &ssh.ClientConfig{
		User:            "root",
		Auth:            getSSHAuthMethods(),
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         TIMEOUT,
	}

	// determine which host to use
	targetHost := USB_HOST
	if testConnection(USB_HOST) != nil {
		// USB failed, try wifi
		targetHost = WIFI_HOST
		if host != "" {
			targetHost = host
		}
		if testConnection(targetHost) != nil {
			return nil, fmt.Errorf("failed to connect via USB (%s) or WiFi (%s)", USB_HOST, targetHost)
		}
	}

	// create client with working host
	client := &Client{
		Host:   targetHost,
		Dir:    dir,
		config: config,
	}

	// try to establish SSH connection (optional - used only for RunCommand)
	// if this fails, SCP operations will still work via system commands
	client.connect()

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
	// if Go SSH client is available, use it
	if c.client != nil {
		session, err := c.client.NewSession()
		if err == nil {
			defer session.Close()
			output, err := session.CombinedOutput(cmd)
			if err != nil {
				return "", fmt.Errorf("command failed: %w", err)
			}
			return filterSSHWarnings(string(output)), nil
		}
	}

	// fallback to system ssh
	sshCmd := exec.Command("ssh", fmt.Sprintf("root@%s", c.Host), cmd)
	output, err := sshCmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("command failed: %w", err)
	}

	return filterSSHWarnings(string(output)), nil
}

// filterSSHWarnings removes SSH warning messages from output
func filterSSHWarnings(output string) string {
	lines := strings.Split(output, "\n")
	var filtered []string
	for _, line := range lines {
		// Skip lines that are SSH warnings
		if !strings.HasPrefix(line, "** WARNING:") &&
			!strings.HasPrefix(line, "** This session") &&
			!strings.HasPrefix(line, "** The server") {
			filtered = append(filtered, line)
		}
	}
	return strings.Join(filtered, "\n")
}

func (c *Client) TransferFile(localPath, remotePath string) error {
	// use scp command directly - simpler and more reliable
	remoteTarget := fmt.Sprintf("root@%s:%s", c.Host, remotePath)
	cmd := exec.Command("scp", localPath, remoteTarget)

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("scp failed: %w", err)
	}

	return nil
}

// DownloadFile downloads a file from reMarkable to local path
func (c *Client) DownloadFromRemote(remotePath, localPath string) error {
	remoteSource := fmt.Sprintf("root@%s:%s", c.Host, remotePath)
	cmd := exec.Command("scp", remoteSource, localPath)

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("scp download failed: %w", err)
	}

	return nil
}
