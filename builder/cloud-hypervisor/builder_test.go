package cloudhypervisor_test

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hashicorp/packer-plugin-sdk/multistep"

	cloudhypervisor "github.com/moeryomenko/packer-plugin-cloud-hypervisor/builder/cloud-hypervisor"
	"github.com/moeryomenko/packer-plugin-cloud-hypervisor/builder/cloud-hypervisor/chclient"
)

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

// mockUI is a no-op implementation of packersdk.Ui for use in tests.
type mockUI struct{}

func (u *mockUI) Ask(_ string) (string, error)            { return "", nil }
func (u *mockUI) Say(_ string)                            {}
func (u *mockUI) Message(_ string)                        {}
func (u *mockUI) Error(_ string)                          {}
func (u *mockUI) Machine(_ string, _ ...string)           {}
func (u *mockUI) Askf(_ string, _ ...any) (string, error) { return "", nil }
func (u *mockUI) Sayf(_ string, _ ...any)                 {}
func (u *mockUI) Messagef(_ string, _ ...any)             {}
func (u *mockUI) Errorf(_ string, _ ...any)               {}
func (u *mockUI) TrackProgress(_ string, _, _ int64, stream io.ReadCloser) io.ReadCloser {
	return stream
}

// startTestServer creates a Unix socket HTTP test server that routes requests
// through the provided handler function. Returns the socket path.
// Cleanup is automatic via t.Cleanup.
func startTestServer(t *testing.T, handler http.HandlerFunc) string {
	t.Helper()
	socketPath := filepath.Join(t.TempDir(), "ch-test.sock")
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatalf("failed to listen on unix socket %s: %v", socketPath, err)
	}
	t.Cleanup(func() { listener.Close() })
	srv := &http.Server{
		Handler:      handler,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 10 * time.Second,
	}
	go func() {
		if err := srv.Serve(listener); err != nil {
			if !strings.Contains(err.Error(), "use of closed network connection") {
				t.Logf("test server error: %v", err)
			}
		}
	}()
	return socketPath
}

// writeUnchecked ignores the error from w.Write in test HTTP handlers.
// errcheck with check-blank:true flags even `writeUnchecked(w, )`, so this
// helper explicitly suppresses the lint.
func writeUnchecked(w http.ResponseWriter, data []byte) {
	_, _ = w.Write(data) //nolint:errcheck
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

var testCtx = context.Background() //nolint:gochecknoglobals

// ---------------------------------------------------------------------------
// StepCreateVM — Run behaviour
// ---------------------------------------------------------------------------
// REQ-010: VM Creation sends VMConfig and handles success/error.
// VC-12:   After CH is ready, sending a VMConfig returns HTTP 200 or 204.

func TestStepCreateVM_Run(t *testing.T) {
	t.Parallel()
	var capturedBody []byte
	socketPath := startTestServer(t, routeHandler(map[string]func(w http.ResponseWriter, r *http.Request){
		"PUT /api/v1/vm.create": func(w http.ResponseWriter, r *http.Request) {
			var err error
			capturedBody, err = io.ReadAll(r.Body)
			if err != nil {
				t.Fatalf("failed to read body: %v", err)
			}
			if r.Header.Get("Content-Type") != "application/json" {
				t.Errorf("Content-Type = %q, want application/json", r.Header.Get("Content-Type"))
			}
			w.WriteHeader(http.StatusNoContent)
		},
	}))

	client := chclient.New(socketPath)
	state := new(multistep.BasicStateBag)
	state.Put("ch_client", client)
	state.Put("ui", &mockUI{})

	vmConfig := &chclient.VMConfig{
		Cpus:   chclient.CpusConfig{BootVcpus: 2, MaxVcpus: 4},
		Memory: chclient.MemoryConfig{Size: 536870912},
		Payload: &chclient.PayloadConfig{
			Kernel:    new("/vmlinux"),
			Initramfs: new("/initramfs"),
			Cmdline:   new("console=ttyS0"),
		},
		Disks: []chclient.DiskConfig{
			{Path: "/disk0.img", Readonly: false, ImageType: "raw"},
		},
		Serial:  chclient.SerialConfig{Mode: "null"},
		Console: chclient.ConsoleConfig{Mode: "null"},
		Rng:     chclient.RngConfig{Src: "/dev/urandom"},
	}

	step := &cloudhypervisor.StepCreateVM{VMConfig: vmConfig}
	action := step.Run(testCtx, state)

	if action != multistep.ActionContinue {
		t.Fatalf("expected ActionContinue, got %v", action)
	}
	if len(capturedBody) == 0 {
		t.Fatal("expected non-empty request body")
	}

	// Validate the body deserialises correctly
	var sent chclient.VMConfig
	if err := json.Unmarshal(capturedBody, &sent); err != nil {
		t.Fatalf("failed to unmarshal captured body: %v", err)
	}
	if sent.Cpus.MaxVcpus != 4 {
		t.Errorf("cpus.max_vcpus = %d, want 4", sent.Cpus.MaxVcpus)
	}

	// Verify no error was put in state
	if _, ok := state.GetOk("error"); ok {
		t.Fatal("unexpected error in state bag")
	}
}

func TestStepCreateVM_Run_ErrorHalts(t *testing.T) {
	t.Parallel()
	socketPath := startTestServer(t, routeHandler(map[string]func(w http.ResponseWriter, r *http.Request){
		"PUT /api/v1/vm.create": func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusBadRequest)
			writeUnchecked(w, []byte(`["Invalid memory size"]`))
		},
	}))

	client := chclient.New(socketPath)
	state := new(multistep.BasicStateBag)
	state.Put("ch_client", client)
	state.Put("ui", &mockUI{})

	step := &cloudhypervisor.StepCreateVM{VMConfig: &chclient.VMConfig{
		Cpus:    chclient.CpusConfig{BootVcpus: 1, MaxVcpus: 1},
		Memory:  chclient.MemoryConfig{Size: 0},
		Serial:  chclient.SerialConfig{Mode: "null"},
		Console: chclient.ConsoleConfig{Mode: "null"},
		Rng:     chclient.RngConfig{Src: "/dev/urandom"},
	}}
	action := step.Run(testCtx, state)

	if action != multistep.ActionHalt {
		t.Fatalf("expected ActionHalt on CH error, got %v", action)
	}
	err, ok := state.GetOk("error")
	if !ok {
		t.Fatal("expected error in state bag on CH failure")
	}
	chErr, ok := err.(error)
	if !ok {
		t.Fatal("state error is not an error type")
	}
	errStr := chErr.Error()
	if !strings.Contains(errStr, "400") {
		t.Errorf("error should reference HTTP status 400, got: %s", errStr)
	}
	if !strings.Contains(errStr, "Invalid memory size") {
		t.Errorf("error should include CH error body, got: %s", errStr)
	}
}

// ---------------------------------------------------------------------------
// StepBootVM — Run behaviour
// ---------------------------------------------------------------------------
// REQ-011: After creation, the boot request is sent and errors halt.

func TestStepBootVM_Run(t *testing.T) {
	t.Parallel()
	socketPath := startTestServer(t, routeHandler(map[string]func(w http.ResponseWriter, r *http.Request){
		"PUT /api/v1/vm.boot": func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		},
	}))

	client := chclient.New(socketPath)
	state := new(multistep.BasicStateBag)
	state.Put("ch_client", client)
	state.Put("ui", &mockUI{})

	step := &cloudhypervisor.StepBootVM{}
	action := step.Run(testCtx, state)

	if action != multistep.ActionContinue {
		t.Fatalf("expected ActionContinue, got %v", action)
	}
	if _, ok := state.GetOk("error"); ok {
		t.Fatal("unexpected error in state bag")
	}
}

func TestStepBootVM_Run_ErrorHalts(t *testing.T) {
	t.Parallel()
	socketPath := startTestServer(t, routeHandler(map[string]func(w http.ResponseWriter, r *http.Request){
		"PUT /api/v1/vm.boot": func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNotFound)
			writeUnchecked(w, []byte(`["Vm not found"]`))
		},
	}))

	client := chclient.New(socketPath)
	state := new(multistep.BasicStateBag)
	state.Put("ch_client", client)
	state.Put("ui", &mockUI{})

	step := &cloudhypervisor.StepBootVM{}
	action := step.Run(testCtx, state)

	if action != multistep.ActionHalt {
		t.Fatalf("expected ActionHalt on CH error, got %v", action)
	}
	if _, ok := state.GetOk("error"); !ok {
		t.Fatal("expected error in state bag on CH failure")
	}
}

// ---------------------------------------------------------------------------
// StepShutdownVM — Run and Cleanup behaviour
// ---------------------------------------------------------------------------
// REQ-014: Graceful shutdown, poll vm.info until 404, force delete on timeout.
//          Shutdown errors are logged but not fatal.
// REQ-016: Cleanup removes the VM via DeleteVm.

func TestStepShutdownVM_Run(t *testing.T) {
	t.Parallel()
	var shutdownCalled, infoCalled bool
	socketPath := startTestServer(t, routeHandler(map[string]func(w http.ResponseWriter, r *http.Request){
		"PUT /api/v1/vm.shutdown": func(w http.ResponseWriter, _ *http.Request) {
			shutdownCalled = true
			w.WriteHeader(http.StatusNoContent)
		},
		"GET /api/v1/vm.info": func(w http.ResponseWriter, _ *http.Request) {
			infoCalled = true
			w.WriteHeader(http.StatusNotFound)
			writeUnchecked(w, []byte(`["VM not found"]`))
		},
	}))

	client := chclient.New(socketPath)
	state := new(multistep.BasicStateBag)
	state.Put("ch_client", client)
	state.Put("ui", &mockUI{})

	step := &cloudhypervisor.StepShutdownVM{}
	action := step.Run(testCtx, state)

	if action != multistep.ActionContinue {
		t.Fatalf("expected ActionContinue, got %v", action)
	}
	if !shutdownCalled {
		t.Error("expected vm.shutdown to be called")
	}
	if !infoCalled {
		t.Error("expected vm.info to be called during shutdown poll")
	}
}

func TestStepShutdownVM_Run_ShutdownErrorContinues(t *testing.T) {
	t.Parallel()
	// Per REQ-014 item 2: If CH returns an error on shutdown, log and continue.
	var infoCalled bool
	socketPath := startTestServer(t, routeHandler(map[string]func(w http.ResponseWriter, r *http.Request){
		"PUT /api/v1/vm.shutdown": func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNotFound)
			writeUnchecked(w, []byte(`["VM not found"]`))
		},
		"GET /api/v1/vm.info": func(w http.ResponseWriter, _ *http.Request) {
			infoCalled = true
			w.WriteHeader(http.StatusNotFound)
			writeUnchecked(w, []byte(`["VM not found"]`))
		},
	}))

	client := chclient.New(socketPath)
	state := new(multistep.BasicStateBag)
	state.Put("ch_client", client)
	state.Put("ui", &mockUI{})

	step := &cloudhypervisor.StepShutdownVM{}
	action := step.Run(testCtx, state)

	if action != multistep.ActionContinue {
		t.Fatalf("expected ActionContinue despite shutdown error, got %v", action)
	}
	if !infoCalled {
		t.Error("expected vm.info to be called after shutdown error")
	}
}

func TestStepShutdownVM_Cleanup(t *testing.T) {
	t.Parallel()
	var deleteCalled bool
	socketPath := startTestServer(t, routeHandler(map[string]func(w http.ResponseWriter, r *http.Request){
		"PUT /api/v1/vm.delete": func(w http.ResponseWriter, _ *http.Request) {
			deleteCalled = true
			w.WriteHeader(http.StatusNoContent)
		},
	}))

	client := chclient.New(socketPath)
	state := new(multistep.BasicStateBag)
	state.Put("ch_client", client)

	step := &cloudhypervisor.StepShutdownVM{}
	step.Cleanup(state)

	if !deleteCalled {
		t.Error("expected vm.delete to be called during Cleanup")
	}
}

// ---------------------------------------------------------------------------
// StepCollectArtifact — Run behaviour
// ---------------------------------------------------------------------------
// REQ-015: Non-readonly disk files are copied to output dir and an Artifact
//          is placed in the state bag.

func TestStepCollectArtifact_Run(t *testing.T) {
	t.Parallel()
	outputDir := t.TempDir()

	// Create source disk files
	srcDir := t.TempDir()
	srcRootfs := filepath.Join(srcDir, "rootfs.img")
	srcCloudInit := filepath.Join(srcDir, "cloud-init.img")
	if err := os.WriteFile(srcRootfs, []byte("rootfs-content"), 0o600); err != nil {
		t.Fatalf("failed to create src rootfs: %s", err)
	}
	if err := os.WriteFile(srcCloudInit, []byte("cloud-init-content"), 0o600); err != nil {
		t.Fatalf("failed to create src cloud-init: %s", err)
	}

	cfg := &cloudhypervisor.Config{
		DiskImages: []cloudhypervisor.DiskImage{
			{Path: srcRootfs, Readonly: false, ImageType: "raw"},
			{Path: srcCloudInit, Readonly: true, ImageType: "raw"},
		},
	}

	state := new(multistep.BasicStateBag)
	state.Put("config", cfg)
	state.Put("ui", &mockUI{})

	step := &cloudhypervisor.StepCollectArtifact{
		Config:    cfg,
		OutputDir: outputDir,
	}
	action := step.Run(testCtx, state)

	if action != multistep.ActionContinue {
		t.Fatalf("expected ActionContinue, got %v", action)
	}

	// Verify the writable disk was copied
	destPath := filepath.Join(outputDir, "rootfs.img")
	if _, err := os.Stat(destPath); os.IsNotExist(err) {
		t.Fatal("expected rootfs.img to be copied to output dir")
	}
	copiedContent, err := os.ReadFile(destPath)
	if err != nil {
		t.Fatalf("failed to read copied file: %s", err)
	}
	if string(copiedContent) != "rootfs-content" {
		t.Errorf("copied file content = %q, want %q", string(copiedContent), "rootfs-content")
	}

	// The readonly disk should NOT be copied
	readonlyDest := filepath.Join(outputDir, "cloud-init.img")
	if _, err := os.Stat(readonlyDest); !os.IsNotExist(err) {
		t.Error("readonly disk should not be copied to output dir")
	}

	// Artifact should exist in state bag
	rawArtifact, ok := state.GetOk("artifact")
	if !ok {
		t.Fatal("expected artifact in state bag")
	}
	art, ok := rawArtifact.(*cloudhypervisor.Artifact)
	if !ok {
		t.Fatalf("expected *cloudhypervisor.Artifact, got %T", rawArtifact)
	}

	// Artifact files should contain only the writable disk copy
	if len(art.Files()) != 1 {
		t.Fatalf("expected 1 file in artifact, got %d: %v", len(art.Files()), art.Files())
	}
	if art.Files()[0] != destPath {
		t.Errorf("artifact file = %q, want %q", art.Files()[0], destPath)
	}

	// Also test BuilderID and ID methods
	if art.BuilderId() != cloudhypervisor.BuilderID {
		t.Errorf("BuilderId() = %q, want %q", art.BuilderId(), cloudhypervisor.BuilderID)
	}
	if art.Id() == "" {
		t.Error("Id() should not be empty")
	}
}

func TestStepCollectArtifact_Run_NoWritableDisks(t *testing.T) {
	t.Parallel()
	outputDir := t.TempDir()

	// All disks are readonly
	srcDir := t.TempDir()
	srcRo := filepath.Join(srcDir, "seed.img")
	if err := os.WriteFile(srcRo, []byte("seed-content"), 0o600); err != nil {
		t.Fatalf("failed to create seed disk: %s", err)
	}

	cfg := &cloudhypervisor.Config{
		DiskImages: []cloudhypervisor.DiskImage{
			{Path: srcRo, Readonly: true, ImageType: "raw"},
		},
	}

	state := new(multistep.BasicStateBag)
	state.Put("config", cfg)
	state.Put("ui", &mockUI{})

	step := &cloudhypervisor.StepCollectArtifact{
		Config:    cfg,
		OutputDir: outputDir,
	}
	action := step.Run(testCtx, state)

	if action != multistep.ActionContinue {
		t.Fatalf("expected ActionContinue with no writable disks, got %v", action)
	}

	// No artifact should be created when there is nothing to collect
	if _, ok := state.GetOk("artifact"); ok {
		t.Error("unexpected artifact in state bag when no writable disks")
	}
}

func TestStepCollectArtifact_Run_EmptyDisks(t *testing.T) {
	t.Parallel()
	outputDir := t.TempDir()

	cfg := &cloudhypervisor.Config{
		DiskImages: []cloudhypervisor.DiskImage{},
	}

	state := new(multistep.BasicStateBag)
	state.Put("config", cfg)
	state.Put("ui", &mockUI{})

	step := &cloudhypervisor.StepCollectArtifact{
		Config:    cfg,
		OutputDir: outputDir,
	}
	action := step.Run(testCtx, state)

	if action != multistep.ActionContinue {
		t.Fatalf("expected ActionContinue with empty disk list, got %v", action)
	}
	if _, ok := state.GetOk("artifact"); ok {
		t.Error("unexpected artifact in state bag with empty disk list")
	}
}

// ---------------------------------------------------------------------------
// Artifact — packersdk.Artifact implementation
// ---------------------------------------------------------------------------
// REQ-015: BuilderID, Files, ID, State, Destroy must work according to spec.

func TestArtifact_BuilderID(t *testing.T) {
	t.Parallel()
	a := &cloudhypervisor.Artifact{}
	if a.BuilderId() != cloudhypervisor.BuilderID {
		t.Errorf("BuilderId() = %q, want %q", a.BuilderId(), cloudhypervisor.BuilderID)
	}
}

func TestArtifact_Files(t *testing.T) {
	t.Parallel()
	files := []string{"/tmp/out/disk0.img", "/tmp/out/disk1.img"}
	a := &cloudhypervisor.Artifact{Dir: "/tmp/out", FilesList: files}

	got := a.Files()
	if len(got) != len(files) {
		t.Fatalf("Files() returned %d entries, want %d: %v", len(got), len(files), got)
	}
	for i := range files {
		if got[i] != files[i] {
			t.Errorf("Files()[%d] = %q, want %q", i, got[i], files[i])
		}
	}
}

func TestArtifact_ID(t *testing.T) {
	t.Parallel()
	a := &cloudhypervisor.Artifact{Dir: "/some/build/dir"}
	id := a.Id()
	if id == "" {
		t.Fatal("Id() returned empty string")
	}
	if !strings.Contains(id, "cloud-hypervisor") && !strings.Contains(id, "artifact") {
		// At minimum, the id should describe what it is. Accept any non-empty
		// string; the precise format is a design choice.
		t.Logf("Id() = %q (unexpected format, but non-empty)", id)
	}
}

func TestArtifact_Destroy(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	f1 := filepath.Join(dir, "disk0.img")
	f2 := filepath.Join(dir, "disk1.img")
	for _, p := range []string{f1, f2} {
		if err := os.WriteFile(p, []byte("data"), 0o600); err != nil {
			t.Fatalf("failed to create artifact file: %s", err)
		}
	}

	a := &cloudhypervisor.Artifact{
		Dir:       dir,
		FilesList: []string{f1, f2},
	}

	if err := a.Destroy(); err != nil {
		t.Fatalf("Destroy() returned error: %v", err)
	}

	// The output directory should be removed
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Error("expected output directory to be removed after Destroy")
	}
}

func TestArtifact_State(t *testing.T) {
	t.Parallel()
	a := &cloudhypervisor.Artifact{
		Dir:       "/tmp/artifact-dir",
		FilesList: []string{"/tmp/artifact-dir/disk.img"},
	}

	// "generated_data" must return a map with at least artifact_id
	data := a.State("generated_data")
	m, ok := data.(map[string]any)
	if !ok {
		t.Fatalf("State('generated_data') returned %T, expected map[string]interface{}", data)
	}
	if _, exists := m["artifact_id"]; !exists {
		t.Error("generated_data should contain 'artifact_id'")
	}

	// Unknown keys return nil
	if v := a.State("nonexistent"); v != nil {
		t.Errorf("State('nonexistent') = %v, want nil", v)
	}
}

// ---------------------------------------------------------------------------
// CommHost — determines the SSH host address from config
// ---------------------------------------------------------------------------
// REQ-012: CommHost returns the guest IP from the first network interface
//          that has one, or empty string when no IP is configured.

func TestCommHost_WithIP(t *testing.T) {
	t.Parallel()
	cfg := &cloudhypervisor.Config{
		NetworkInterfaces: []cloudhypervisor.NetworkInterface{
			{Tap: "ch-tap-0", IP: "10.0.2.15"},
		},
	}

	hostFunc := cloudhypervisor.CommHost(cfg)
	host, err := hostFunc(nil)
	if err != nil {
		t.Fatalf("CommHost returned error: %v", err)
	}
	if host != "10.0.2.15" {
		t.Errorf("CommHost = %q, want %q", host, "10.0.2.15")
	}
}

func TestCommHost_WithIP_SecondInterface(t *testing.T) {
	t.Parallel()
	// IP on the second interface should still be returned
	cfg := &cloudhypervisor.Config{
		NetworkInterfaces: []cloudhypervisor.NetworkInterface{
			{Tap: "ch-tap-0", Mac: "de:ad:be:ef:00:01"},
			{Tap: "ch-tap-1", IP: "192.168.100.2"},
		},
	}

	hostFunc := cloudhypervisor.CommHost(cfg)
	host, err := hostFunc(nil)
	if err != nil {
		t.Fatalf("CommHost returned error: %v", err)
	}
	if host != "192.168.100.2" {
		t.Errorf("CommHost = %q, want %q", host, "192.168.100.2")
	}
}

func TestCommHost_NoIP(t *testing.T) {
	t.Parallel()
	cfg := &cloudhypervisor.Config{
		NetworkInterfaces: []cloudhypervisor.NetworkInterface{
			{Tap: "ch-tap-0"},
		},
	}

	hostFunc := cloudhypervisor.CommHost(cfg)
	host, err := hostFunc(nil)
	if err != nil {
		t.Fatalf("CommHost returned error: %v", err)
	}
	if host != "" {
		t.Errorf("CommHost = %q, want empty string", host)
	}
}

func TestCommHost_EmptyInterfaces(t *testing.T) {
	t.Parallel()
	cfg := &cloudhypervisor.Config{
		NetworkInterfaces: []cloudhypervisor.NetworkInterface{},
	}

	hostFunc := cloudhypervisor.CommHost(cfg)
	host, err := hostFunc(nil)
	if err != nil {
		t.Fatalf("CommHost returned error: %v", err)
	}
	if host != "" {
		t.Errorf("CommHost = %q, want empty string", host)
	}
}
