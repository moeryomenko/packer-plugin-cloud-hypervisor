# Cloud-Hypervisor Packer Plugin

A [Packer](https://www.packer.io) builder plugin that creates VM images using
[Cloud-Hypervisor](https://github.com/cloud-hypervisor/cloud-hypervisor) (CH).
It launches CH as a subprocess, creates and boots a VM via CH's HTTP-over-Unix-socket
API, provisions it over SSH using standard Packer workflows, then produces the
modified disk image(s) as the artifact.

## Features

- **Direct kernel boot** — boot from a vmlinuz kernel + initramfs with a
  customizable command line.
- **Firmware/boot** — UEFI firmware boot (EDK2) with a bootable disk image.
- **Full Packer integration** — SSH communicator, shell/file/ansible provisioners,
  HCP Packer registry support.
- **Multi-disk support** — attach multiple disk images with per-disk read-only
  settings.
- **TAP networking** — bridge VM networking to the host via TAP devices.
- **Clean lifecycle** — automatic CH subprocess management, graceful VM shutdown,
  and cleanup on error.

## Requirements

- **Linux host with KVM support** — `/dev/kvm` must exist and be accessible.
- **Cloud-Hypervisor binary** (minimum v38.0) — on `$PATH` or pointed to by
  `ch_binary_path`. See the [releases page](https://github.com/cloud-hypervisor/cloud-hypervisor/releases).
- **Packer >= 1.9.0** — [packer.io/downloads](https://www.packer.io/downloads).
- **Go toolchain** — required to build the plugin from source.

## Installation

Build the plugin binary and install it for Packer:

```bash
git clone https://github.com/moeryomenko/packer-plugin-cloud-hypervisor.git
cd packer-plugin-cloud-hypervisor

go build -o packer-plugin-cloud-hypervisor .
mkdir -p ~/.packer.d/plugins/
cp packer-plugin-cloud-hypervisor ~/.packer.d/plugins/
```

## Quick Start

See the [examples](examples/) directory for complete working templates covering
both kernel boot (Alpine Linux) and firmware boot (Ubuntu).

```bash
# Build and install the plugin
go build -o packer-plugin-cloud-hypervisor .
cp packer-plugin-cloud-hypervisor ~/.packer.d/plugins/

# Run the setup script (creates assets and TAP device)
cd examples
sudo ./setup.sh

# Run the kernel-boot example
cd kernel-boot
packer init .
packer build .
```

## Configuration Reference

### Required Fields

At least one of `kernel` or `firmware` must be specified. They are mutually
exclusive.

| Field | Type | Description |
|-------|------|-------------|
| `kernel` | string | Path to a kernel image (vmlinuz/bzImage) for direct boot |
| `firmware` | string | Path to a firmware blob (EDK2 UEFI) for firmware boot |
| `vcpus` | int | Number of vCPUs (>= 1) |
| `memory` | int | Memory size in MiB (>= 128) |

### Optional Fields

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `ch_binary_path` | string | `"cloud-hypervisor"` | Path to the CH binary |
| `ch_socket_path` | string | temp file | Path for the CH API Unix socket |
| `cpu_boot` | int | `vcpus` | Boot vCPUs (must be <= vcpus) |
| `initramfs` | string | — | Path to initramfs (only with `kernel`) |
| `cmdline` | string | — | Kernel command line (only with `kernel`) |
| `serial` | string | `"null"` | Serial mode: `off`, `pty`, `tty`, `file`, `socket`, `null` |
| `serial_file` | string | — | Path for serial output when `serial = "file"` |
| `console` | string | `"null"` | Console mode: same options as `serial` |
| `console_file` | string | — | Path for console output when `console = "file"` |
| `rng_source` | string | `"/dev/urandom"` | RNG source device |
| `communicator` | string | `"ssh"` | Communicator type (`ssh`, `winrm`, `none`) |

### Disk Images

```hcl
disk_images {
  path      = "/path/to/disk.raw"
  readonly  = false    # optional, default: false
  image_type = "raw"   # optional: raw, qcow2, fixedvhd, vhdx
  id        = "vda"    # optional device identifier
}
```

At least one non-readonly disk is required for artifact capture. If all disks
are read-only, the builder emits a warning and produces no artifact.

### Network Interfaces

```hcl
network_interfaces {
  tap = "ch-tap-0"    # TAP device name
  mac = "52:54:00:..." # optional MAC address
  ip  = "10.0.2.15/24" # optional guest IP (for SSH host detection)
  id  = "net1"         # optional device identifier
}
```

SSH-based builds require at least one network interface.

## Build Lifecycle

```
1. Launch Cloud-Hypervisor as a subprocess
2. Create VM via HTTP API (PUT /api/v1/vm.create)
3. Boot VM via HTTP API (PUT /api/v1/vm.boot)
4. Wait for SSH connection
5. Run Packer provisioners
6. Gracefully shut down VM
7. Copy writable disk images to output directory
8. Clean up CH subprocess and socket
```

## Development

### Prerequisites

- Go 1.26+
- Cloud-Hypervisor binary for integration tests

### Commands

```bash
make build      # Build the plugin binary
make test       # Run all tests
make lint       # Run golangci-lint
make vet        # Run go vet
make generate   # Regenerate config.hcl2spec.go after config changes
make fmt        # Format Go source files
make check      # Run lint, vet, and test (CI gate)
```

### Configuration Changes

After editing fields in `builder/cloud-hypervisor/config.go`, regenerate the
HCL2 spec file:

```bash
make generate
```

The generated `config.hcl2spec.go` must never be hand-edited.

## License

Licensed under either of:

- Apache License, Version 2.0 ([LICENSE-APACHE](LICENSE-APACHE))
- MIT license ([LICENSE-MIT](LICENSE-MIT))

at your option.
