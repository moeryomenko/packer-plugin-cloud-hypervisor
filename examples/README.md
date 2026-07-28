# Cloud-Hypervisor Packer Plugin — Examples

This directory contains example Packer templates that demonstrate the
cloud-hypervisor builder plugin. Two boot modes are covered:

- **Kernel boot (Alpine Linux)** — Direct kernel boot using a vmlinuz
  and initramfs payload with a raw rootfs disk image.
- **Firmware boot (Ubuntu)** — UEFI firmware boot using EDK2
  (CLOUDHV.fd) with a raw Ubuntu cloud image.

Each example includes complete Packer HCL2 template files
(packer.pkr.hcl, variables.pkr.hcl, source.pkr.hcl, build.pkr.hcl)
and a setup.sh script that prepares all prerequisite files and network
infrastructure.

## Prerequisites

All examples require the following on the host system:

- **Linux host with KVM support** — Check that `/dev/kvm` exists and
  is readable/writable by the user running Packer (or by the
  cloud-hypervisor binary).
- **Cloud-Hypervisor binary** (minimum v38.0) — Available on `$PATH`
  or pointed to by the `CH_BINARY_PATH` environment variable. Binaries
  are published on the
  [cloud-hypervisor releases page](https://github.com/cloud-hypervisor/cloud-hypervisor/releases).
- **Packer >= 1.9.0** — Installed on the host. Downloads from
  [packer.io/downloads](https://www.packer.io/downloads).
- **Go toolchain** — Required to build the plugin binary from source.
  See [go.dev/dl](https://go.dev/dl/).
- **qemu-img** — Required by setup.sh to convert the Ubuntu cloud image
  from qcow2 to raw format. Available from the `qemu-utils` or
  `qemu-img` package on most distributions.
- **TAP device** — A `ch-tap-0` TAP device is required for VM
  networking. Created by setup.sh (see below).
- **root or sudo access** — Required for TAP device creation via
  `ip tuntap`.

The setup.sh script checks all dependencies and reports missing items
before making any changes.

## Quick Start

The following commands build the plugin from source, install it for
Packer, prepare prerequisite files, and run the kernel-boot example.

```bash
# 1. Build the plugin
cd ..
go build -o packer-plugin-cloud-hypervisor .

# 2. Install the plugin for Packer
mkdir -p ~/.packer.d/plugins/
cp packer-plugin-cloud-hypervisor ~/.packer.d/plugins/

# 3. Run the setup script (creates assets and TAP device)
cd examples
sudo ./setup.sh

# 4. Run the kernel-boot example
cd kernel-boot
packer init .
packer build .
```

## Example Walkthroughs

### Kernel Boot (Alpine)

**What it demonstrates.** This example showcases direct kernel boot with
cloud-hypervisor. No firmware is involved — the VM starts by loading a
vmlinuz kernel and initramfs directly, with a kernel command line
specifying console, network, and root device parameters.

**Config highlights.**

| Setting | Value | Variable |
|---|---|---|
| `kernel` | `../assets/alpine-vmlinuz` (Alpine virt kernel) | `kernel_path` |
| `initramfs` | `../assets/alpine-initramfs` (Alpine initramfs) | `initramfs_path` |
| `cmdline` | `console=ttyS0,115200 earlyprintk=serial net.ifnames=0 alpine_dev=sda modules=ext4` | inlined in source |
| `disk_images` | One raw disk, read-write | `disk_path` |
| `network_interfaces` | TAP device `ch-tap-0` | `tap_device` |
| `vcpus` | 2 | `vcpus` |
| `memory` | 1024 MiB | `memory` |
| `cpu_boot` | 1 | `cpus_boot` |

**SSH connectivity.** Alpine boots with root password authentication
enabled. The template uses:

- Username: `root`
- Password: `root`
- Timeout: 10 minutes
- Handshake attempts: 200

**Build flow.**

1. Packer creates the VM via the cloud-hypervisor HTTP API
2. The VM boots the Alpine kernel with the provided initramfs
3. SSH connection is established (root/root)
4. The file provisioner uploads `provision.sh` to `/tmp/provision.sh`
5. A shell provisioner runs the script, which:
   - Writes build metadata to `/etc/builder-info`
   - Creates a flag file at `/etc/alpine-packed-by-packer`
   - Disables root password logins on subsequent boots
6. A verification provisioner runs `uname -a` and `cat /etc/builder-info`
7. The VM shuts down
8. Packer collects the modified raw disk image as an artifact

**Expected output.** The build produces an artifact directory named
`output-alpine-kernel-boot/` containing the modified raw disk image
with the provisioning changes applied.

### Firmware Boot (Ubuntu)

**What it demonstrates.** This example showcases UEFI firmware boot with
cloud-hypervisor. The VM boots using the EDK2 UEFI firmware binary
(CLOUDHV.fd) and loads a standard Ubuntu cloud image as its root disk.
Cloud-init runs on first boot, configuring the `ubuntu` user and
network.

**Config highlights.**

| Setting | Value | Variable |
|---|---|---|
| `firmware` | `../assets/CLOUDHV.fd` (EDK2 UEFI) | `firmware_path` |
| `disk_images` | One raw disk, read-write | `disk_path` |
| `network_interfaces` | TAP device `ch-tap-0` | `tap_device` |
| `vcpus` | 2 | `vcpus` |
| `memory` | 2048 MiB | `memory` |
| `serial` / `console` | `"null"` (SSH-only interaction) | inlined in source |

**SSH connectivity.** Ubuntu cloud images use SSH key authentication by
default. This example uses password auth for convenience. The template
uses:

- Username: `ubuntu`
- Password: `ubuntu`
- Timeout: 20 minutes (UEFI boot + cloud-init first boot can be slow)
- Handshake attempts: 300

For production use, configure SSH key authentication via cloud-init
user-data or using the `ssh_private_key_file` option.

**Build flow.**

1. Packer creates the VM via the cloud-hypervisor HTTP API
2. The VM boots via UEFI firmware (EDK2)
3. Cloud-init runs on first boot, setting up the `ubuntu` user
4. SSH connection is established (ubuntu/ubuntu)
5. A shell provisioner runs `apt-get update` and installs `htop`
6. A second shell provisioner writes build metadata to `/etc/builder-info`
7. A file provisioner writes a placeholder cloud-init file to
   `/etc/cloud-init-placeholder`
8. The VM shuts down
9. Packer collects the modified raw disk image as an artifact

**Expected output.** The build produces an artifact directory named
`output-ubuntu-firmware-boot/` containing the modified raw disk image
with updated package cache, installed packages, and the custom
metadata files.

## Architecture

The cloud-hypervisor Packer builder follows this flow:

```
Packer Builder
     |
     |-- (1) Launch cloud-hypervisor as a subprocess
     |          |
     |          v
     |     cloud-hypervisor (listening on Unix socket)
     |
     |-- (2) Create VM via HTTP API (PUT /api/v1/vm.create)
     |          |
     |          v
     |     VM created with VmConfig (CPU, memory, disks, network)
     |
     |-- (3) Boot VM via HTTP API (PUT /api/v1/vm.boot)
     |          |
     |          v
     |     VM running (kernel or firmware boot)
     |
     |-- (4) Wait for SSH connection
     |          |
     |          v
     |     SSH connection established to guest IP
     |
     |-- (5) Run Packer provisioners
     |          |
     |          v
     |     Guest OS modified (files, packages, config)
     |
     |-- (6) Shut down VM via HTTP API (PUT /api/v1/vm.shutdown)
     |          |
     |          v
     |     VM stopped
     |
     |-- (7) Collect modified disk as artifact
```

Key technical details:

- Communication with cloud-hypervisor uses **HTTP/1.1 over a Unix
  domain socket** at the default API endpoint (`/api/v1`).
- The builder manages the full lifecycle: start cloud-hypervisor,
  create the VM, boot it, wait for SSH, run provisioners, shut down,
  collect the artifact.
- Networking uses a **TAP device** bridged to the host, with the guest
  obtaining an IP address via DHCP.
- The artifact is the **modified raw disk image** — the same file that
  was attached to the VM, now containing all provisioning changes.

## Troubleshooting

### "Plugin not found"

If `packer build` fails with an error about the cloud-hypervisor plugin
not being found, the plugin binary is not installed correctly.

```bash
# Build the plugin from the project root
go build -o packer-plugin-cloud-hypervisor .

# Install it for Packer
mkdir -p ~/.packer.d/plugins/
cp packer-plugin-cloud-hypervisor ~/.packer.d/plugins/
```

### "KVM not available"

Cloud-hypervisor requires access to `/dev/kvm`. If the device is missing
or not accessible:

```bash
# Check if /dev/kvm exists
ls -l /dev/kvm

# On most distributions, add your user to the kvm group
sudo usermod -aG kvm $USER

# Log out and back in for the group change to take effect
```

If `/dev/kvm` does not exist at all, ensure hardware virtualization is
enabled in the BIOS and that the `kvm` kernel module is loaded:

```bash
sudo modprobe kvm kvm_intel  # or kvm_amd
```

### "TAP device not found"

The examples expect a TAP device named `ch-tap-0`. If it does not exist,
run the setup script with sudo:

```bash
sudo ./examples/setup.sh
```

To create it manually:

```bash
sudo ip tuntap add dev ch-tap-0 mode tap
```

### "SSH connection timeout"

If Packer fails to connect to the VM via SSH, the boot may be too slow
for the default timeout. Override the SSH timeout or handshake attempts
on the command line:

```bash
packer build \
  -var 'ssh_timeout=20m' \
  -var 'ssh_handshake_attempts=300' \
  .
```

For the kernel-boot example, the relevant variables are
`ssh_wait_timeout` (in the source block) and `ssh_handshake_attempts`
(in the source block). For the firmware-boot example, use `ssh_timeout`
and `ssh_handshake_attempts` as template variables.

### "cloud-hypervisor not found"

If cloud-hypervisor is not on `$PATH`, install it or set the
`CH_BINARY_PATH` environment variable:

```bash
# Set the path to the cloud-hypervisor binary
export CH_BINARY_PATH=/usr/local/bin/cloud-hypervisor

# Or override it in the Packer build
packer build -var 'ch_binary=/usr/local/bin/cloud-hypervisor' .
```

### "Disk image not found"

The examples expect asset files (Alpine kernel, initramfs, rootfs, EDK2
firmware, Ubuntu cloud image) in the `examples/assets/` directory. Run
the setup script to create them:

```bash
sudo ./examples/setup.sh
```

The script is idempotent — it skips files that already exist and
reports pass/fail/skip for each step.

## Directory Structure

```
examples/
  setup.sh                          # Prerequisite setup script
  README.md                         # This file

  kernel-boot/                      # Direct kernel boot example (Alpine)
    packer.pkr.hcl                  #   Packer version & plugin requirements
    variables.pkr.hcl               #   Template variable declarations
    source.pkr.hcl                  #   Source block (kernel + initramfs boot)
    build.pkr.hcl                   #   Build orchestration (provisioners)
    provision.sh                    #   Guest provisioning script

  firmware-boot/                    # UEFI firmware boot example (Ubuntu)
    packer.pkr.hcl                  #   Packer version & plugin requirements
    variables.pkr.hcl               #   Template variable declarations
    source.pkr.hcl                  #   Source block (firmware + disk boot)
    build.pkr.hcl                   #   Build orchestration (provisioners)
```

### File Descriptions

| File | Purpose |
|---|---|
| `setup.sh` | Downloads Alpine kernel/initramfs, EDK2 firmware, Ubuntu cloud image; creates Alpine rootfs; converts Ubuntu image to raw; validates cloud-hypervisor binary; creates TAP device. Idempotent and safe to re-run. |
| `kernel-boot/packer.pkr.hcl` | Declares Packer >= 1.9.0 and the cloud-hypervisor plugin (>= 0.0.1) from `github.com/eryoma/cloud-hypervisor`. |
| `kernel-boot/variables.pkr.hcl` | Defines variables for kernel path, initramfs path, disk path, TAP device, CH binary, SSH credentials, vCPUs, memory, and boot CPUs with defaults pointing to `../assets/`. |
| `kernel-boot/source.pkr.hcl` | Defines the `cloud-hypervisor` source block for Alpine kernel boot with kernel, initramfs, cmdline, raw disk, TAP networking, and SSH communicator. |
| `kernel-boot/build.pkr.hcl` | Defines the build block with file upload, shell provisioning (provision.sh), and verification steps. |
| `kernel-boot/provision.sh` | Guest script that writes `/etc/builder-info`, creates `/etc/alpine-packed-by-packer`, and disables root password SSH login. |
| `firmware-boot/packer.pkr.hcl` | Declares Packer >= 1.9.0 and the cloud-hypervisor plugin (>= 0.0.1) from `github.com/eryoma/cloud-hypervisor`. |
| `firmware-boot/variables.pkr.hcl` | Defines variables for firmware path, disk path, TAP device, CH binary, SSH credentials, vCPUs, memory, and SSH timeout with defaults pointing to `../assets/`. |
| `firmware-boot/source.pkr.hcl` | Defines the `cloud-hypervisor` source block for Ubuntu firmware boot with EDK2 firmware, raw disk, TAP networking, null serial/console, and SSH communicator. |
| `firmware-boot/build.pkr.hcl` | Defines the build block with apt update, package install (`htop`), build metadata creation, and file upload. |
