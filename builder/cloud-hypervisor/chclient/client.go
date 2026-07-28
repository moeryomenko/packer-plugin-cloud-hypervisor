package chclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
)

const httpStatusError = 400

// Client communicates with a Cloud-Hypervisor instance over its HTTP API
// through a Unix socket.
type Client struct {
	socketPath string
	http       *http.Client
}

// New creates a new Client that connects to the Cloud-Hypervisor API at the
// given Unix socket path.
func New(socketPath string) *Client {
	return &Client{
		socketPath: socketPath,
		http: &http.Client{
			Transport: &http.Transport{
				DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
					var d net.Dialer
					return d.DialContext(ctx, "unix", socketPath)
				},
			},
		},
	}
}

// VMConfig represents the full VM configuration sent to Cloud-Hypervisor.
type VMConfig struct {
	Cpus    CpusConfig     `json:"cpus"`
	Memory  MemoryConfig   `json:"memory"`
	Payload *PayloadConfig `json:"payload,omitempty"`
	Disks   []DiskConfig   `json:"disks,omitempty"`
	Net     []NetConfig    `json:"net,omitempty"`
	Serial  SerialConfig   `json:"serial"`
	Console ConsoleConfig  `json:"console"`
	Rng     RngConfig      `json:"rng"`
}

// CpusConfig configures the CPU topology of the VM.
type CpusConfig struct {
	BootVcpus int `json:"boot_vcpus"`
	MaxVcpus  int `json:"max_vcpus"`
}

// MemoryConfig configures the memory size of the VM.
type MemoryConfig struct {
	Size int64 `json:"size"`
}

// PayloadConfig configures the boot payload (kernel or firmware).
type PayloadConfig struct {
	Kernel    *string `json:"kernel,omitempty"`
	Initramfs *string `json:"initramfs,omitempty"`
	Cmdline   *string `json:"cmdline,omitempty"`
	Firmware  *string `json:"firmware,omitempty"`
}

// DiskConfig configures a disk device attached to the VM.
type DiskConfig struct {
	Path      string `json:"path"`
	Readonly  bool   `json:"readonly,omitempty"`
	ImageType string `json:"image_type,omitempty"`
	ID        string `json:"id,omitempty"`
}

// NetConfig configures a network interface attached to the VM.
type NetConfig struct {
	Tap  string `json:"tap,omitempty"`
	Mac  string `json:"mac,omitempty"`
	IP   string `json:"ip,omitempty"`
	Mask string `json:"mask,omitempty"`
	ID   string `json:"id,omitempty"`
}

// SerialConfig configures the VM serial interface.
type SerialConfig struct {
	Mode string `json:"mode"`
}

// ConsoleConfig configures the VM console interface.
type ConsoleConfig struct {
	Mode string `json:"mode"`
}

// RngConfig configures the VM random number generator device.
type RngConfig struct {
	Src string `json:"src"`
}

// doRequest sends an HTTP request and processes the response. On success
// it returns the response body as a string. On error (status >= 400) it
// parses the CH error body (a JSON array of strings) and includes the
// messages in the returned error.
func (c *Client) doRequest(ctx context.Context, method, path, contentType string, body io.Reader) (string, error) {
	url := "http://localhost" + path
	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return "", fmt.Errorf("%s %s: %w", method, path, err)
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("%s %s: %w", method, path, err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("%s %s: reading response: %w", method, path, err)
	}

	if resp.StatusCode >= httpStatusError {
		var errMsgs []string
		if err := json.Unmarshal(respBody, &errMsgs); err == nil && len(errMsgs) > 0 {
			return "", fmt.Errorf("HTTP %d: %s", resp.StatusCode, strings.Join(errMsgs, ", "))
		}
		return "", fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(respBody))
	}

	return string(respBody), nil
}

// Ping checks whether the Cloud-Hypervisor API is ready. It sends a GET
// request to /api/v1/vmm.ping and returns nil on 200 OK.
func (c *Client) Ping(ctx context.Context) error {
	_, err := c.doRequest(ctx, "GET", "/api/v1/vmm.ping", "", nil)
	return err
}

// CreateVM sends a VM creation request with the given configuration. It
// PUTs the VMConfig as JSON to /api/v1/vm.create and returns nil on 204.
func (c *Client) CreateVM(ctx context.Context, config *VMConfig) error {
	body, err := json.Marshal(config)
	if err != nil {
		return fmt.Errorf("vm.create: marshaling config: %w", err)
	}
	_, err = c.doRequest(ctx, "PUT", "/api/v1/vm.create", "application/json", bytes.NewReader(body))
	return err
}

// BootVM starts the previously created VM. It sends PUT to /api/v1/vm.boot
// and returns nil on 204.
func (c *Client) BootVM(ctx context.Context) error {
	_, err := c.doRequest(ctx, "PUT", "/api/v1/vm.boot", "", nil)
	return err
}

// ShutdownVM sends a graceful shutdown request to the VM. It sends PUT to
// /api/v1/vm.shutdown and returns nil on 204.
func (c *Client) ShutdownVM(ctx context.Context) error {
	_, err := c.doRequest(ctx, "PUT", "/api/v1/vm.shutdown", "", nil)
	return err
}

// DeleteVM forcefully removes the VM. It sends PUT to /api/v1/vm.delete
// and returns nil on 204.
func (c *Client) DeleteVM(ctx context.Context) error {
	_, err := c.doRequest(ctx, "PUT", "/api/v1/vm.delete", "", nil)
	return err
}

// VMInfo returns the current VM status information. It sends GET to
// /api/v1/vm.info and returns the response body on 200.
func (c *Client) VMInfo(ctx context.Context) (string, error) {
	return c.doRequest(ctx, "GET", "/api/v1/vm.info", "", nil)
}
