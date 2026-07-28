package chclient_test

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/moeryomenko/packer-plugin-cloud-hypervisor/builder/cloud-hypervisor/chclient"
)

// startTestServer creates a Unix socket HTTP test server that routes requests
// through the provided handler function. Returns the socket path.
// Cleanup is automatic via t.Cleanup.
func startTestServer(t *testing.T, handler func(w http.ResponseWriter, r *http.Request)) string {
	t.Helper()
	socketPath := filepath.Join(t.TempDir(), "ch-test.sock")
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatalf("failed to listen on unix socket %s: %v", socketPath, err)
	}
	t.Cleanup(func() {
		listener.Close()
	})
	go func() {
		server := &http.Server{
			Handler:      http.HandlerFunc(handler),
			ReadTimeout:  5 * time.Second,
			WriteTimeout: 10 * time.Second,
		}
		if err := server.Serve(listener); err != nil {
			// "use of closed network connection" is expected during cleanup
			if !strings.Contains(err.Error(), "use of closed network connection") {
				t.Logf("test server error: %v", err)
			}
		}
	}()
	return socketPath
}

// routeHandler creates an http.HandlerFunc that dispatches requests based on
// method + path, returning 404 with a JSON error body for unrecognised routes.
func routeHandler(routes map[string]func(w http.ResponseWriter, r *http.Request)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		key := r.Method + " " + r.URL.Path
		if h, ok := routes[key]; ok {
			h(w, r)
			return
		}
		w.WriteHeader(http.StatusNotFound)
		writeUnchecked(w, []byte(`["route not found"]`))
	}
}

// writeUnchecked ignores the error from w.Write in test HTTP handlers.
// errcheck with check-blank:true flags even `writeUnchecked(w, )`, so this
// helper explicitly suppresses the lint.
func writeUnchecked(w http.ResponseWriter, data []byte) {
	_, _ = w.Write(data) //nolint:errcheck
}

// strPtr is a helper that returns a pointer to the given string.
func strPtr(s string) *string {
	return &s
}

// ---------------------------------------------------------------------------
// Happy-path tests — each API method returns the expected success code
// ---------------------------------------------------------------------------

func TestPing(t *testing.T) {
	t.Parallel()
	socketPath := startTestServer(t, routeHandler(map[string]func(w http.ResponseWriter, r *http.Request){
		"GET /api/v1/vmm.ping": func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		},
	}))
	client := chclient.New(socketPath)
	if err := client.Ping(context.Background()); err != nil {
		t.Fatalf("Ping() returned unexpected error: %v", err)
	}
}

func TestCreateVm(t *testing.T) {
	t.Parallel()
	var capturedMethod, capturedPath string
	var capturedCT string
	socketPath := startTestServer(t, routeHandler(map[string]func(w http.ResponseWriter, r *http.Request){
		"PUT /api/v1/vm.create": func(w http.ResponseWriter, r *http.Request) {
			capturedMethod = r.Method
			capturedPath = r.URL.Path
			capturedCT = r.Header.Get("Content-Type")
			w.WriteHeader(http.StatusNoContent)
		},
	}))
	client := chclient.New(socketPath)
	config := &chclient.VMConfig{
		Cpus:    chclient.CpusConfig{BootVcpus: 2, MaxVcpus: 4},
		Memory:  chclient.MemoryConfig{Size: 536870912},
		Payload: &chclient.PayloadConfig{Kernel: strPtr("/path/to/vmlinux")},
		Disks:   []chclient.DiskConfig{{Path: "/path/to/rootfs.img", Readonly: false, ImageType: "raw"}},
		Net:     []chclient.NetConfig{{Tap: "ch-tap-0", Mac: "de:ad:be:ef:00:01"}},
		Serial:  chclient.SerialConfig{Mode: "null"},
		Console: chclient.ConsoleConfig{Mode: "null"},
		Rng:     chclient.RngConfig{Src: "/dev/urandom"},
	}
	if err := client.CreateVM(context.Background(), config); err != nil {
		t.Fatalf("CreateVM() returned unexpected error: %v", err)
	}
	if capturedMethod != "PUT" {
		t.Errorf("request method = %q, want PUT", capturedMethod)
	}
	if capturedPath != "/api/v1/vm.create" {
		t.Errorf("request path = %q, want /api/v1/vm.create", capturedPath)
	}
	if capturedCT != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", capturedCT)
	}
}

func TestBootVm(t *testing.T) {
	t.Parallel()
	var capturedMethod, capturedPath string
	socketPath := startTestServer(t, routeHandler(map[string]func(w http.ResponseWriter, r *http.Request){
		"PUT /api/v1/vm.boot": func(w http.ResponseWriter, r *http.Request) {
			capturedMethod = r.Method
			capturedPath = r.URL.Path
			w.WriteHeader(http.StatusNoContent)
		},
	}))
	client := chclient.New(socketPath)
	if err := client.BootVM(context.Background()); err != nil {
		t.Fatalf("BootVM() returned unexpected error: %v", err)
	}
	if capturedMethod != "PUT" {
		t.Errorf("request method = %q, want PUT", capturedMethod)
	}
	if capturedPath != "/api/v1/vm.boot" {
		t.Errorf("request path = %q, want /api/v1/vm.boot", capturedPath)
	}
}

func TestShutdownVm(t *testing.T) {
	t.Parallel()
	socketPath := startTestServer(t, routeHandler(map[string]func(w http.ResponseWriter, r *http.Request){
		"PUT /api/v1/vm.shutdown": func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		},
	}))
	client := chclient.New(socketPath)
	if err := client.ShutdownVM(context.Background()); err != nil {
		t.Fatalf("ShutdownVM() returned unexpected error: %v", err)
	}
}

func TestDeleteVm(t *testing.T) {
	t.Parallel()
	socketPath := startTestServer(t, routeHandler(map[string]func(w http.ResponseWriter, r *http.Request){
		"PUT /api/v1/vm.delete": func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		},
	}))
	client := chclient.New(socketPath)
	if err := client.DeleteVM(context.Background()); err != nil {
		t.Fatalf("DeleteVM() returned unexpected error: %v", err)
	}
}

func TestVmInfo(t *testing.T) {
	t.Parallel()
	expectedBody := `{"state":"Running","config":{"cpus":{"boot_vcpus":2,"max_vcpus":4}}}`
	socketPath := startTestServer(t, routeHandler(map[string]func(w http.ResponseWriter, r *http.Request){
		"GET /api/v1/vm.info": func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
			writeUnchecked(w, []byte(expectedBody))
		},
	}))
	client := chclient.New(socketPath)
	body, err := client.VMInfo(context.Background())
	if err != nil {
		t.Fatalf("VMInfo() returned unexpected error: %v", err)
	}
	if body != expectedBody {
		t.Errorf("VMInfo() body = %q, want %q", body, expectedBody)
	}
}

// ---------------------------------------------------------------------------
// Error handling — non-2xx responses produce descriptive Go errors
// ---------------------------------------------------------------------------

func TestPingServerError(t *testing.T) {
	t.Parallel()
	socketPath := startTestServer(t, routeHandler(map[string]func(w http.ResponseWriter, r *http.Request){
		"GET /api/v1/vmm.ping": func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
			writeUnchecked(w, []byte(`["Internal server error"]`))
		},
	}))
	client := chclient.New(socketPath)
	err := client.Ping(context.Background())
	if err == nil {
		t.Fatal("Ping() expected error for 500, got nil")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("error should reference status 500, got: %v", err)
	}
	if !strings.Contains(err.Error(), "Internal server error") {
		t.Errorf("error should include CH error body, got: %v", err)
	}
}

func TestCreateVmBadRequest(t *testing.T) {
	t.Parallel()
	socketPath := startTestServer(t, routeHandler(map[string]func(w http.ResponseWriter, r *http.Request){
		"PUT /api/v1/vm.create": func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusBadRequest)
			writeUnchecked(w, []byte(`["Invalid memory size"]`))
		},
	}))
	client := chclient.New(socketPath)
	err := client.CreateVM(context.Background(), &chclient.VMConfig{})
	if err == nil {
		t.Fatal("CreateVM() expected error for 400, got nil")
	}
	if !strings.Contains(err.Error(), "400") {
		t.Errorf("error should reference status 400, got: %v", err)
	}
	if !strings.Contains(err.Error(), "Invalid memory size") {
		t.Errorf("error should include CH error body, got: %v", err)
	}
}

func TestBootVmNotFound(t *testing.T) {
	t.Parallel()
	socketPath := startTestServer(t, routeHandler(map[string]func(w http.ResponseWriter, r *http.Request){
		"PUT /api/v1/vm.boot": func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNotFound)
			writeUnchecked(w, []byte(`["Vm not found"]`))
		},
	}))
	client := chclient.New(socketPath)
	err := client.BootVM(context.Background())
	if err == nil {
		t.Fatal("BootVM() expected error for 404, got nil")
	}
	if !strings.Contains(err.Error(), "404") {
		t.Errorf("error should reference status 404, got: %v", err)
	}
	if !strings.Contains(err.Error(), "Vm not found") {
		t.Errorf("error should include CH error body, got: %v", err)
	}
}

func TestCreateVmServerError(t *testing.T) {
	t.Parallel()
	socketPath := startTestServer(t, routeHandler(map[string]func(w http.ResponseWriter, r *http.Request){
		"PUT /api/v1/vm.create": func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
			writeUnchecked(w, []byte(`["Internal error"]`))
		},
	}))
	client := chclient.New(socketPath)
	err := client.CreateVM(context.Background(), &chclient.VMConfig{})
	if err == nil {
		t.Fatal("CreateVM() expected error for 500, got nil")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("error should reference status 500, got: %v", err)
	}
}

func TestShutdownVmNotFound(t *testing.T) {
	t.Parallel()
	socketPath := startTestServer(t, routeHandler(map[string]func(w http.ResponseWriter, r *http.Request){
		"PUT /api/v1/vm.shutdown": func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNotFound)
			writeUnchecked(w, []byte(`["Vm not found"]`))
		},
	}))
	client := chclient.New(socketPath)
	err := client.ShutdownVM(context.Background())
	if err == nil {
		t.Fatal("ShutdownVM() expected error for 404, got nil")
	}
}

func TestDeleteVmNotFound(t *testing.T) {
	t.Parallel()
	socketPath := startTestServer(t, routeHandler(map[string]func(w http.ResponseWriter, r *http.Request){
		"PUT /api/v1/vm.delete": func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNotFound)
			writeUnchecked(w, []byte(`["Vm not found"]`))
		},
	}))
	client := chclient.New(socketPath)
	err := client.DeleteVM(context.Background())
	if err == nil {
		t.Fatal("DeleteVM() expected error for 404, got nil")
	}
}

// TestErrorBodyMultipleMessages verifies that CH error bodies containing
// a JSON array of multiple error strings are all surfaced in the Go error.
func TestErrorBodyMultipleMessages(t *testing.T) {
	t.Parallel()
	socketPath := startTestServer(t, routeHandler(map[string]func(w http.ResponseWriter, r *http.Request){
		"PUT /api/v1/vm.create": func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusBadRequest)
			writeUnchecked(w, []byte(`["DeviceManagerError: No such device", "VmError: DeviceManager(NoSuchDevice)"]`))
		},
	}))
	client := chclient.New(socketPath)
	err := client.CreateVM(context.Background(), &chclient.VMConfig{})
	if err == nil {
		t.Fatal("CreateVM() expected error, got nil")
	}
	if !strings.Contains(err.Error(), "DeviceManagerError") {
		t.Errorf("error should include first CH message, got: %v", err)
	}
	if !strings.Contains(err.Error(), "VmError") {
		t.Errorf("error should include second CH message, got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Edge cases — boundary conditions, empty responses, unusual inputs
// ---------------------------------------------------------------------------

// TestCreateVmEmptyConfig verifies that creating a VM with an empty/minimal
// config still sends a valid PUT request and does not cause panics.
func TestCreateVmEmptyConfig(t *testing.T) {
	t.Parallel()
	var capturedBody []byte
	socketPath := startTestServer(t, routeHandler(map[string]func(w http.ResponseWriter, r *http.Request){
		"PUT /api/v1/vm.create": func(w http.ResponseWriter, r *http.Request) {
			var err error
			capturedBody, err = io.ReadAll(r.Body)
			if err != nil {
				t.Fatalf("failed to read body: %v", err)
			}
			w.WriteHeader(http.StatusNoContent)
		},
	}))
	client := chclient.New(socketPath)
	if err := client.CreateVM(context.Background(), &chclient.VMConfig{}); err != nil {
		t.Fatalf("CreateVM() with empty config returned error: %v", err)
	}
	// Even an empty config should produce valid JSON
	if len(capturedBody) == 0 {
		t.Error("request body was empty, expected non-empty JSON")
	}
	if !json.Valid(capturedBody) {
		t.Errorf("request body is not valid JSON: %s", string(capturedBody))
	}
}

// TestVmInfoEmptyBody verifies the client handles an empty response body
// from vm.info without crashing (e.g. no Content-Type, no body).
func TestVmInfoEmptyBody(t *testing.T) {
	t.Parallel()
	socketPath := startTestServer(t, routeHandler(map[string]func(w http.ResponseWriter, r *http.Request){
		"GET /api/v1/vm.info": func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		},
	}))
	client := chclient.New(socketPath)
	body, err := client.VMInfo(context.Background())
	if err != nil {
		t.Fatalf("VMInfo() with empty body returned error: %v", err)
	}
	// Empty string is acceptable for a no-body 200 response
	if body != "" {
		t.Logf("VMInfo() returned body %q for empty response (acceptable)", body)
	}
}

// TestVmInfoNotFound verifies vm.info returns an error when the VM does
// not exist (404).
func TestVmInfoNotFound(t *testing.T) {
	t.Parallel()
	socketPath := startTestServer(t, routeHandler(map[string]func(w http.ResponseWriter, r *http.Request){
		"GET /api/v1/vm.info": func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNotFound)
			writeUnchecked(w, []byte(`["VM not found"]`))
		},
	}))
	client := chclient.New(socketPath)
	_, err := client.VMInfo(context.Background())
	if err == nil {
		t.Fatal("VMInfo() expected error for 404, got nil")
	}
}

// TestNonExistentSocket verifies the client returns an error when the Unix
// socket does not exist (network-level error).
func TestNonExistentSocket(t *testing.T) {
	t.Parallel()
	client := chclient.New("/nonexistent/ch-test.sock")
	err := client.Ping(context.Background())
	if err == nil {
		t.Fatal("Ping() expected error with nonexistent socket, got nil")
	}
}

// TestCreateVmWithPayloadConfig verifies that both kernel-boot and
// firmware-boot payload configs are serialised correctly in the body.
func TestCreateVmWithPayloadConfig(t *testing.T) {
	t.Parallel()
	t.Run("kernel payload", func(t *testing.T) {
		t.Parallel()
		var capturedBody []byte
		socketPath := startTestServer(t, routeHandler(map[string]func(w http.ResponseWriter, r *http.Request){
			"PUT /api/v1/vm.create": func(w http.ResponseWriter, r *http.Request) {
				var err error
				capturedBody, err = io.ReadAll(r.Body)
				if err != nil {
					t.Fatalf("failed to read body: %v", err)
				}
				w.WriteHeader(http.StatusNoContent)
			},
		}))
		client := chclient.New(socketPath)
		config := &chclient.VMConfig{
			Cpus:   chclient.CpusConfig{BootVcpus: 2, MaxVcpus: 2},
			Memory: chclient.MemoryConfig{Size: 268435456},
			Payload: &chclient.PayloadConfig{
				Kernel:    strPtr("/vmlinux"),
				Initramfs: strPtr("/initramfs"),
				Cmdline:   strPtr("console=ttyS0"),
			},
			Disks:   []chclient.DiskConfig{{Path: "/disk.img"}},
			Serial:  chclient.SerialConfig{Mode: "null"},
			Console: chclient.ConsoleConfig{Mode: "null"},
			Rng:     chclient.RngConfig{Src: "/dev/urandom"},
		}
		if err := client.CreateVM(context.Background(), config); err != nil {
			t.Fatalf("CreateVM() returned error: %v", err)
		}
		var raw map[string]interface{}
		if err := json.Unmarshal(capturedBody, &raw); err != nil {
			t.Fatalf("invalid JSON body: %v", err)
		}
		payload, ok := raw["payload"].(map[string]interface{})
		if !ok {
			t.Fatal("payload field missing or not an object")
		}
		if _, ok := payload["kernel"]; !ok {
			t.Error("payload.kernel is missing")
		}
		if _, ok := payload["initramfs"]; !ok {
			t.Error("payload.initramfs is missing")
		}
		if _, ok := payload["cmdline"]; !ok {
			t.Error("payload.cmdline is missing")
		}
		if _, ok := payload["firmware"]; ok {
			t.Error("payload.firmware present unexpectedly for kernel payload")
		}
	})

	t.Run("firmware payload", func(t *testing.T) {
		t.Parallel()
		var capturedBody []byte
		socketPath := startTestServer(t, routeHandler(map[string]func(w http.ResponseWriter, r *http.Request){
			"PUT /api/v1/vm.create": func(w http.ResponseWriter, r *http.Request) {
				var err error
				capturedBody, err = io.ReadAll(r.Body)
				if err != nil {
					t.Fatalf("failed to read body: %v", err)
				}
				w.WriteHeader(http.StatusNoContent)
			},
		}))
		client := chclient.New(socketPath)
		config := &chclient.VMConfig{
			Cpus:    chclient.CpusConfig{BootVcpus: 2, MaxVcpus: 2},
			Memory:  chclient.MemoryConfig{Size: 268435456},
			Payload: &chclient.PayloadConfig{Firmware: strPtr("/OVMF.fd")},
			Disks:   []chclient.DiskConfig{{Path: "/bootable.img"}},
			Serial:  chclient.SerialConfig{Mode: "null"},
			Console: chclient.ConsoleConfig{Mode: "null"},
			Rng:     chclient.RngConfig{Src: "/dev/urandom"},
		}
		if err := client.CreateVM(context.Background(), config); err != nil {
			t.Fatalf("CreateVM() returned error: %v", err)
		}
		var raw map[string]interface{}
		if err := json.Unmarshal(capturedBody, &raw); err != nil {
			t.Fatalf("invalid JSON body: %v", err)
		}
		payload, ok := raw["payload"].(map[string]interface{})
		if !ok {
			t.Fatal("payload field missing or not an object")
		}
		if _, ok := payload["firmware"]; !ok {
			t.Error("payload.firmware is missing")
		}
		if _, ok := payload["kernel"]; ok {
			t.Error("payload.kernel present unexpectedly for firmware payload")
		}
	})
}

// ---------------------------------------------------------------------------
// VMConfig JSON serialisation — field names must use snake_case
// ---------------------------------------------------------------------------

// TestVmConfigJSONSnakeCase verifies that marshalling a fully-populated
// VMConfig produces JSON with snake_case field names matching CH's serde
// convention, and no camelCase or PascalCase fields leak through.
func TestVmConfigJSONSnakeCase(t *testing.T) {
	t.Parallel()
	config := chclient.VMConfig{
		Cpus: chclient.CpusConfig{
			BootVcpus: 2,
			MaxVcpus:  4,
		},
		Memory: chclient.MemoryConfig{
			Size: 536870912,
		},
		Payload: &chclient.PayloadConfig{
			Kernel:    strPtr("/path/to/vmlinux"),
			Initramfs: strPtr("/path/to/initramfs"),
			Cmdline:   strPtr("console=ttyS0 root=/dev/vda rw"),
		},
		Disks: []chclient.DiskConfig{
			{Path: "/disk0.img", Readonly: false, ImageType: "raw"},
			{Path: "/disk1.img", Readonly: true, ImageType: "qcow2", ID: "disk1"},
		},
		Net: []chclient.NetConfig{
			{Tap: "ch-tap-0", Mac: "de:ad:be:ef:00:01", IP: "10.0.2.15", ID: "net0"},
		},
		Serial:  chclient.SerialConfig{Mode: "pty"},
		Console: chclient.ConsoleConfig{Mode: "null"},
		Rng:     chclient.RngConfig{Src: "/dev/urandom"},
	}
	data, err := json.Marshal(config)
	if err != nil {
		t.Fatalf("json.Marshal(VMConfig) failed: %v", err)
	}
	// Fail fast if any PascalCase or camelCase Go field names leaked
	leaked := []string{
		"BootVcpus", "bootVcpus", "MaxVcpus", "maxVcpus",
		"Payload", "Kernel", "Initramfs", "Cmdline", "Firmware",
		"Disks", "Readonly", "ImageType", "ID",
		"Net", "Tap", "Mac", "IP",
		"Serial", "Console", "Mode",
		"Rng", "Src",
	}
	jsonStr := string(data)
	for _, field := range leaked {
		if strings.Contains(jsonStr, field) {
			t.Errorf("JSON contains Go field name %q (expected snake_case)", field)
		}
	}
	// Unmarshal into generic map to verify all expected keys exist with
	// snake_case names
	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("failed to unmarshal VMConfig JSON: %v", err)
	}
	type fieldCheck struct {
		key      string
		subKeys  []string
		optional bool
	}
	checks := []fieldCheck{
		{key: "cpus", subKeys: []string{"boot_vcpus", "max_vcpus"}},
		{key: "memory", subKeys: []string{"size"}},
		{key: "payload", subKeys: []string{"kernel", "initramfs", "cmdline"}, optional: true},
		{key: "disks", optional: true},
		{key: "net", optional: true},
		{key: "serial", subKeys: []string{"mode"}},
		{key: "console", subKeys: []string{"mode"}},
		{key: "rng", subKeys: []string{"src"}},
	}
	for _, c := range checks {
		val, exists := raw[c.key]
		if !exists {
			if c.optional {
				continue
			}
			t.Errorf("top-level key %q is missing (required)", c.key)
			continue
		}
		if len(c.subKeys) > 0 {
			obj, ok := val.(map[string]interface{})
			if !ok {
				t.Errorf("key %q is not a JSON object", c.key)
				continue
			}
			for _, sk := range c.subKeys {
				if _, ok := obj[sk]; !ok {
					t.Errorf("field %s.%s is missing (should be snake_case)", c.key, sk)
				}
			}
		}
	}
}

// TestCreateVmRequestBodySnakeCase is an integration-level check that
// the body sent by CreateVM uses snake_case keys on the wire.
func TestCreateVmRequestBodySnakeCase(t *testing.T) {
	t.Parallel()
	var capturedBody []byte
	socketPath := startTestServer(t, routeHandler(map[string]func(w http.ResponseWriter, r *http.Request){
		"PUT /api/v1/vm.create": func(w http.ResponseWriter, r *http.Request) {
			var err error
			capturedBody, err = io.ReadAll(r.Body)
			if err != nil {
				t.Fatalf("failed to read body: %v", err)
			}
			w.WriteHeader(http.StatusNoContent)
		},
	}))
	client := chclient.New(socketPath)
	config := &chclient.VMConfig{
		Cpus:    chclient.CpusConfig{BootVcpus: 2, MaxVcpus: 4},
		Memory:  chclient.MemoryConfig{Size: 268435456},
		Payload: &chclient.PayloadConfig{Kernel: strPtr("/vmlinux")},
		Disks:   []chclient.DiskConfig{{Path: "/disk.img", Readonly: false}},
		Serial:  chclient.SerialConfig{Mode: "null"},
		Console: chclient.ConsoleConfig{Mode: "null"},
		Rng:     chclient.RngConfig{Src: "/dev/urandom"},
	}
	if err := client.CreateVM(context.Background(), config); err != nil {
		t.Fatalf("CreateVM() returned error: %v", err)
	}
	jsonStr := string(capturedBody)
	// Leaked camelCase or PascalCase field names
	if strings.Contains(jsonStr, "BootVcpus") || strings.Contains(jsonStr, "bootVcpus") {
		t.Error("request body contains camelCase field name 'BootVcpus' or 'bootVcpus'")
	}
	if strings.Contains(jsonStr, "MaxVcpus") || strings.Contains(jsonStr, "maxVcpus") {
		t.Error("request body contains camelCase field name 'MaxVcpus' or 'maxVcpus'")
	}
	// The correct snake_case names should be present
	if !strings.Contains(jsonStr, `boot_vcpus`) {
		t.Error("request body missing snake_case field 'boot_vcpus'")
	}
	if !strings.Contains(jsonStr, `max_vcpus`) {
		t.Error("request body missing snake_case field 'max_vcpus'")
	}
	if !strings.Contains(jsonStr, `"cpus"`) {
		t.Error("request body missing top-level 'cpus' field")
	}
	if !strings.Contains(jsonStr, `"memory"`) {
		t.Error("request body missing top-level 'memory' field")
	}
	if !strings.Contains(jsonStr, `"payload"`) {
		t.Error("request body missing top-level 'payload' field")
	}
	if !strings.Contains(jsonStr, `"serial"`) {
		t.Error("request body missing top-level 'serial' field")
	}
	if !strings.Contains(jsonStr, `"console"`) {
		t.Error("request body missing top-level 'console' field")
	}
	if !strings.Contains(jsonStr, `"rng"`) {
		t.Error("request body missing top-level 'rng' field")
	}
}

// ---------------------------------------------------------------------------
// Method routing verification — ensures each endpoint uses the correct HTTP
// method (e.g. GET for ping, PUT for create/etc.)
// ---------------------------------------------------------------------------

func TestPingUsesGet(t *testing.T) {
	t.Parallel()
	socketPath := startTestServer(t, routeHandler(map[string]func(w http.ResponseWriter, r *http.Request){
		"GET /api/v1/vmm.ping": func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		},
	}))
	client := chclient.New(socketPath)
	// Use the wrong method (POST) to ensure the actual client uses GET
	// This is checked by routeHandler returning 404 for wrong method
	if err := client.Ping(context.Background()); err != nil {
		t.Fatalf("Ping() failed: %v", err)
	}
}

func TestVmInfoUsesGet(t *testing.T) {
	t.Parallel()
	socketPath := startTestServer(t, routeHandler(map[string]func(w http.ResponseWriter, r *http.Request){
		"GET /api/v1/vm.info": func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
			writeUnchecked(w, []byte(`{}`))
		},
	}))
	client := chclient.New(socketPath)
	if _, err := client.VMInfo(context.Background()); err != nil {
		t.Fatalf("VMInfo() failed: %v", err)
	}
}
