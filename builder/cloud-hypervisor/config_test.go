package cloudhypervisor_test

import (
	"os"
	"path/filepath"
	"testing"

	cloudhypervisor "github.com/moeryomenko/packer-plugin-cloud-hypervisor/builder/cloud-hypervisor"
)

// testConfig returns a minimal valid config map. Individual tests add fields
// to trigger specific validation paths. Note: this config is NOT valid on its
// own — it needs a kernel or firmware payload to pass Prepare(). ssh_username
// is supplied because the communicator Prepare (post-decode) requires it for
// the default ssh communicator.
func testConfig() map[string]any {
	return map[string]any{
		"vcpus":        2,
		"memory":       512,
		"ssh_username": "packer",
	}
}

// testConfigErr asserts that Prepare() produced no warnings and at least one
// error.
func testConfigErr(t *testing.T, warns []string, err error) {
	t.Helper()
	if len(warns) > 0 {
		t.Fatalf("unexpected warnings: %#v", warns)
	}
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

// testConfigOk asserts that Prepare() returned no error. It ignores warnings
// (callers wanting to check warnings should inspect them directly).
func testConfigOk(t *testing.T, _ []string, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
}

// writeTempFile creates a temporary regular file and returns its path. The
// file is removed at the end of the test via t.Cleanup.
func writeTempFile(t *testing.T, pattern string) string {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), pattern)
	if err != nil {
		t.Fatalf("failed to create temp file: %s", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("failed to close temp file: %s", err)
	}
	return f.Name()
}

// makeDiskImageDir creates a temp directory and returns its path. Helper for
// disk-image path tests.
func makeDiskImageDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp(t.TempDir(), "disk-images")
	if err != nil {
		t.Fatalf("failed to create temp dir: %s", err)
	}
	return dir
}

// ---------------------------------------------------------------------------
// VC-01: Prepare() with neither kernel nor firmware returns error
// ---------------------------------------------------------------------------

func TestConfigPrepare_noPayload(t *testing.T) {
	t.Parallel()
	raw := testConfig()
	var c cloudhypervisor.Config
	warns, errs := c.Prepare(raw)
	testConfigErr(t, warns, errs)
}

// ---------------------------------------------------------------------------
// VC-02: Prepare() with both kernel and firmware returns error
// ---------------------------------------------------------------------------

func TestConfigPrepare_bothPayload(t *testing.T) {
	t.Parallel()
	raw := testConfig()
	raw["kernel"] = writeTempFile(t, "kernel-*")
	raw["firmware"] = writeTempFile(t, "firmware-*")
	var c cloudhypervisor.Config
	warns, errs := c.Prepare(raw)
	testConfigErr(t, warns, errs)
}

// ---------------------------------------------------------------------------
// VC-03: Prepare() with vcpus == 0 returns error
// ---------------------------------------------------------------------------

func TestConfigPrepare_zeroVcpus(t *testing.T) {
	t.Parallel()
	raw := testConfig()
	raw["kernel"] = writeTempFile(t, "kernel-*")
	raw["vcpus"] = 0
	var c cloudhypervisor.Config
	warns, errs := c.Prepare(raw)
	testConfigErr(t, warns, errs)
}

// ---------------------------------------------------------------------------
// VC-04: Prepare() with memory < 128 returns error
// ---------------------------------------------------------------------------

func TestConfigPrepare_lowMemory(t *testing.T) {
	t.Parallel()
	raw := testConfig()
	raw["kernel"] = writeTempFile(t, "kernel-*")
	raw["memory"] = 64
	var c cloudhypervisor.Config
	warns, errs := c.Prepare(raw)
	testConfigErr(t, warns, errs)
}

// ---------------------------------------------------------------------------
// VC-05: Prepare() with non-existent disk path returns error
// ---------------------------------------------------------------------------

func TestConfigPrepare_badDiskPath(t *testing.T) {
	t.Parallel()
	raw := testConfig()
	raw["kernel"] = writeTempFile(t, "kernel-*")
	raw["disk_images"] = []map[string]any{
		{
			"path":     "/nonexistent/path/disk.img",
			"readonly": false,
		},
	}
	var c cloudhypervisor.Config
	warns, errs := c.Prepare(raw)
	testConfigErr(t, warns, errs)
}

// ---------------------------------------------------------------------------
// VC-06: Prepare() with non-existent ch_binary_path returns error
// ---------------------------------------------------------------------------

func TestConfigPrepare_badBinaryPath(t *testing.T) {
	t.Parallel()
	raw := testConfig()
	raw["kernel"] = writeTempFile(t, "kernel-*")
	raw["ch_binary_path"] = "/nonexistent/cloud-hypervisor"
	var c cloudhypervisor.Config
	warns, errs := c.Prepare(raw)
	testConfigErr(t, warns, errs)
}

// ---------------------------------------------------------------------------
// VC-07: kernel-only config passes; firmware-only config passes
// ---------------------------------------------------------------------------

func TestConfigPrepare_kernelOnly(t *testing.T) {
	t.Parallel()
	raw := testConfig()
	raw["kernel"] = writeTempFile(t, "kernel-*")
	var c cloudhypervisor.Config
	warns, errs := c.Prepare(raw)
	testConfigOk(t, warns, errs)
}

func TestConfigPrepare_firmwareOnly(t *testing.T) {
	t.Parallel()
	raw := testConfig()
	raw["firmware"] = writeTempFile(t, "firmware-*")
	var c cloudhypervisor.Config
	warns, errs := c.Prepare(raw)
	testConfigOk(t, warns, errs)
}

// ---------------------------------------------------------------------------
// VC-08: All disks readonly produces a warning (non-fatal)
// ---------------------------------------------------------------------------

func TestConfigPrepare_allReadonlyWarning(t *testing.T) {
	t.Parallel()
	diskDir := makeDiskImageDir(t)
	img1 := filepath.Join(diskDir, "disk1.img")
	img2 := filepath.Join(diskDir, "disk2.img")
	for _, p := range []string{img1, img2} {
		if err := os.WriteFile(p, []byte("disk-content"), 0o600); err != nil {
			t.Fatalf("failed to create disk image: %s", err)
		}
	}

	raw := testConfig()
	raw["kernel"] = writeTempFile(t, "kernel-*")
	raw["disk_images"] = []map[string]any{
		{"path": img1, "readonly": true},
		{"path": img2, "readonly": true},
	}
	var c cloudhypervisor.Config
	warns, errs := c.Prepare(raw)
	testConfigOk(t, warns, errs)

	// Expect a warning about no writable disks
	found := false
	for _, w := range warns {
		if w != "" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected a warning about all-readonly disks, got none")
	}
}

// ---------------------------------------------------------------------------
// VC-09: SSH communicator without network_interfaces → error;
//        "none" communicator without network → ok
// ---------------------------------------------------------------------------

func TestConfigPrepare_sshNoNetwork(t *testing.T) {
	t.Parallel()
	raw := testConfig()
	raw["kernel"] = writeTempFile(t, "kernel-*")
	raw["communicator"] = "ssh"
	raw["ssh_username"] = "root"
	// No network_interfaces set; SSH requires at least one network interface
	var c cloudhypervisor.Config
	warns, errs := c.Prepare(raw)
	testConfigErr(t, warns, errs)
}

func TestConfigPrepare_noneNoNetwork(t *testing.T) {
	t.Parallel()
	raw := testConfig()
	raw["kernel"] = writeTempFile(t, "kernel-*")
	raw["communicator"] = "none"
	var c cloudhypervisor.Config
	warns, errs := c.Prepare(raw)
	testConfigOk(t, warns, errs)
}

// ---------------------------------------------------------------------------
// VC-10: Default ch_binary_path, socket path, serial/console
// ---------------------------------------------------------------------------

func TestConfigPrepare_defaults(t *testing.T) {
	t.Parallel()
	raw := testConfig()
	raw["kernel"] = writeTempFile(t, "kernel-*")
	raw["communicator"] = "none"

	var c cloudhypervisor.Config
	warns, errs := c.Prepare(raw)
	testConfigOk(t, warns, errs)

	if c.ChBinaryPath != "cloud-hypervisor" {
		t.Errorf("expected default ch_binary_path to be %q, got %q",
			"cloud-hypervisor", c.ChBinaryPath)
	}
	if c.ChSocketPath == "" {
		t.Errorf("expected non-empty default ch_socket_path")
	}
	if c.Serial != "Null" {
		t.Errorf("expected default serial to be %q, got %q", "Null", c.Serial)
	}
	if c.Console != "Null" {
		t.Errorf("expected default console to be %q, got %q", "Null", c.Console)
	}
}

// ---------------------------------------------------------------------------
// Edge Cases
// ---------------------------------------------------------------------------

// Edge case: memory exactly 128 should PASS (boundary between valid/invalid).
func TestConfigPrepare_memoryBoundary(t *testing.T) {
	t.Parallel()
	raw := testConfig()
	raw["kernel"] = writeTempFile(t, "kernel-*")
	raw["memory"] = 128
	var c cloudhypervisor.Config
	warns, errs := c.Prepare(raw)
	testConfigOk(t, warns, errs)
}

// Edge case: vcpus exactly 1 should PASS (minimum valid).
func TestConfigPrepare_vcpusBoundary(t *testing.T) {
	t.Parallel()
	raw := testConfig()
	raw["kernel"] = writeTempFile(t, "kernel-*")
	raw["vcpus"] = 1
	var c cloudhypervisor.Config
	warns, errs := c.Prepare(raw)
	testConfigOk(t, warns, errs)
}

// Edge case: empty disk_images list should be valid (no disks at all).
func TestConfigPrepare_emptyDisks(t *testing.T) {
	t.Parallel()
	raw := testConfig()
	raw["kernel"] = writeTempFile(t, "kernel-*")
	raw["disk_images"] = []map[string]any{}
	var c cloudhypervisor.Config
	warns, errs := c.Prepare(raw)
	testConfigOk(t, warns, errs)
}

// Edge case: multiple disk entries with mixed readonly/writable should
// validate correctly (no warning about all-readonly, no error about disk
// paths).
func TestConfigPrepare_mixedDisks(t *testing.T) {
	t.Parallel()
	diskDir := makeDiskImageDir(t)
	ro := filepath.Join(diskDir, "cloud-init.img")
	rw := filepath.Join(diskDir, "rootfs.img")
	for _, p := range []string{ro, rw} {
		if err := os.WriteFile(p, []byte("disk-content"), 0o600); err != nil {
			t.Fatalf("failed to create disk image: %s", err)
		}
	}

	raw := testConfig()
	raw["kernel"] = writeTempFile(t, "kernel-*")
	raw["disk_images"] = []map[string]any{
		{"path": ro, "readonly": true},
		{"path": rw, "readonly": false},
	}
	var c cloudhypervisor.Config
	warns, errs := c.Prepare(raw)
	testConfigOk(t, warns, errs)

	// Should NOT have an all-readonly warning since rootfs.img is writable
	foundWarning := false
	for _, w := range warns {
		if w != "" {
			foundWarning = true
			break
		}
	}
	if foundWarning {
		t.Fatal("did not expect a warning with mixed readonly/writable disks")
	}
}

// Edge case: Prepare() called with extra unknown map keys should not error.
// Packer passes through template-level variables like packer_build_name.
func TestConfigPrepare_unknownKeys(t *testing.T) {
	t.Parallel()
	raw := testConfig()
	raw["kernel"] = writeTempFile(t, "kernel-*")
	raw["packer_build_name"] = "test-build"
	raw["packer_build_dir"] = "/tmp"
	raw["some_unknown_field"] = "should-be-ignored"

	var c cloudhypervisor.Config
	warns, errs := c.Prepare(raw)
	testConfigOk(t, warns, errs)
}

// Edge case: custom ch_binary_path pointing to an existing regular file
// should pass.
func TestConfigPrepare_customBinaryPath(t *testing.T) {
	t.Parallel()
	existingBin := writeTempFile(t, "cloud-hypervisor-*")

	raw := testConfig()
	raw["kernel"] = writeTempFile(t, "kernel-*")
	raw["ch_binary_path"] = existingBin
	var c cloudhypervisor.Config
	warns, errs := c.Prepare(raw)
	testConfigOk(t, warns, errs)
}

// Edge case: negative memory should fail (spec says >= 128).
func TestConfigPrepare_negativeMemory(t *testing.T) {
	t.Parallel()
	raw := testConfig()
	raw["kernel"] = writeTempFile(t, "kernel-*")
	raw["memory"] = -1
	var c cloudhypervisor.Config
	warns, errs := c.Prepare(raw)
	testConfigErr(t, warns, errs)
}

// Edge case: negative vcpus should fail (spec says >= 1).
func TestConfigPrepare_negativeVcpus(t *testing.T) {
	t.Parallel()
	raw := testConfig()
	raw["kernel"] = writeTempFile(t, "kernel-*")
	raw["vcpus"] = -1
	var c cloudhypervisor.Config
	warns, errs := c.Prepare(raw)
	testConfigErr(t, warns, errs)
}

// Edge case: disk_images with a valid path that is a directory (not a regular
// file) should fail.
func TestConfigPrepare_diskPathIsDirectory(t *testing.T) {
	t.Parallel()
	diskDir := makeDiskImageDir(t)

	raw := testConfig()
	raw["kernel"] = writeTempFile(t, "kernel-*")
	raw["disk_images"] = []map[string]any{
		{"path": diskDir, "readonly": false},
	}
	var c cloudhypervisor.Config
	warns, errs := c.Prepare(raw)
	testConfigErr(t, warns, errs)
}

// Edge case: WinRM communicator without network should also error (same as
// SSH — any network-based communicator needs at least one interface).
func TestConfigPrepare_winrmNoNetwork(t *testing.T) {
	t.Parallel()
	raw := testConfig()
	raw["kernel"] = writeTempFile(t, "kernel-*")
	raw["communicator"] = "winrm"
	raw["winrm_username"] = "Administrator"
	// No network_interfaces set
	var c cloudhypervisor.Config
	warns, errs := c.Prepare(raw)
	testConfigErr(t, warns, errs)
}

// ---------------------------------------------------------------------------
// Regression: communicator validation must run AFTER config.Decode (bug
// 979d702 validated the zero-value config before decode, failing every build
// with "An ssh_username must be specified" even when the template supplies it).
// ---------------------------------------------------------------------------

func TestConfigPrepare_sshWithUsername(t *testing.T) {
	t.Parallel()
	raw := testConfig()
	raw["kernel"] = writeTempFile(t, "kernel-*")
	raw["communicator"] = "ssh"
	raw["ssh_username"] = "root"
	raw["network_interfaces"] = []map[string]any{
		{"tap": "k8s-test", "mac": "de:ad:be:ef:00:01"},
	}
	var c cloudhypervisor.Config
	warns, errs := c.Prepare(raw)
	testConfigOk(t, warns, errs)
}

func TestConfigPrepare_sshMissingUsername(t *testing.T) {
	t.Parallel()
	raw := testConfig()
	raw["kernel"] = writeTempFile(t, "kernel-*")
	raw["communicator"] = "ssh"
	raw["network_interfaces"] = []map[string]any{
		{"tap": "k8s-test", "mac": "de:ad:be:ef:00:01"},
	}
	// Deliberately no ssh_username: the communicator Prepare (post-decode)
	// must report the missing username.
	delete(raw, "ssh_username")
	var c cloudhypervisor.Config
	warns, errs := c.Prepare(raw)
	testConfigErr(t, warns, errs)
}

func TestConfigPrepare_sshPrivateKeyMissing(t *testing.T) {
	t.Parallel()
	raw := testConfig()
	raw["kernel"] = writeTempFile(t, "kernel-*")
	raw["communicator"] = "ssh"
	raw["ssh_username"] = "root"
	raw["ssh_private_key_file"] = filepath.Join(t.TempDir(), "does-not-exist")
	raw["network_interfaces"] = []map[string]any{
		{"tap": "k8s-test", "mac": "de:ad:be:ef:00:01"},
	}
	var c cloudhypervisor.Config
	warns, errs := c.Prepare(raw)
	testConfigErr(t, warns, errs)
}
