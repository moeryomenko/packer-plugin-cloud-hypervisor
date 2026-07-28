package cloudhypervisor

import (
	"context"
	"errors"
	"strings"

	"github.com/hashicorp/hcl/v2/hcldec"
	"github.com/hashicorp/packer-plugin-sdk/communicator"
	"github.com/hashicorp/packer-plugin-sdk/multistep"
	"github.com/hashicorp/packer-plugin-sdk/multistep/commonsteps"
	packersdk "github.com/hashicorp/packer-plugin-sdk/packer"

	"github.com/moeryomenko/packer-plugin-cloud-hypervisor/builder/cloud-hypervisor/chclient"
)

const miB = 1024 * 1024

// Builder implements the packersdk.Builder interface for building VM images
// with Cloud-Hypervisor.
type Builder struct {
	config Config
	runner multistep.Runner
}

// ConfigSpec returns the HCL2 spec for the builder configuration.
func (b *Builder) ConfigSpec() hcldec.ObjectSpec {
	return b.config.FlatMapstructure().HCL2Spec()
}

// Prepare validates the builder configuration and returns any warnings or
// errors.
func (b *Builder) Prepare(raws ...interface{}) (generatedVars, warnings []string, err error) { //nolint:nonamedreturns
	w, e := b.config.Prepare(raws...)
	if e != nil {
		return nil, w, e
	}
	return nil, w, nil
}

// Run orchestrates the full VM build lifecycle:
//  1. Launch Cloud-Hypervisor
//  2. Create the VM
//  3. Boot the VM
//  4. Connect via communicator (SSH/WinRM)
//  5. Run provisioners
//  6. Shutdown the VM
//  7. Collect the artifact (disk images)
func (b *Builder) Run(ctx context.Context, ui packersdk.Ui, hook packersdk.Hook) (packersdk.Artifact, error) {
	// Build VMConfig from our config
	vmConfig := b.buildVMConfig()

	steps := []multistep.Step{}

	// 1. Launch Cloud-Hypervisor
	steps = append(steps, &StepLaunchCh{
		ChBinaryPath: b.config.ChBinaryPath,
		ChSocketPath: b.config.ChSocketPath,
	})

	// 2. Create the VM
	steps = append(steps, &StepCreateVM{
		VMConfig: vmConfig,
	})

	// 3. Boot the VM
	steps = append(steps, &StepBootVM{})

	// 4. Connect via communicator (SSH/WinRM)
	steps = append(steps, &communicator.StepConnect{
		Config:    &b.config.CommConfig,
		Host:      CommHost(&b.config),
		SSHConfig: b.config.CommConfig.SSHConfigFunc(),
	})

	// 5. Run provisioners
	steps = append(steps, new(commonsteps.StepProvision))

	// 6. Shutdown the VM
	steps = append(steps, &StepShutdownVM{})

	// 7. Collect artifact (disk images)
	outputDir := "output-" + b.config.PackerBuildName
	steps = append(steps, &StepCollectArtifact{
		Config:    &b.config,
		OutputDir: outputDir,
	})

	// Setup state bag
	state := new(multistep.BasicStateBag)
	state.Put("hook", hook)
	state.Put("ui", ui)
	state.Put("config", &b.config)
	state.Put("generated_data", make(map[string]interface{}))

	// Run!
	b.runner = commonsteps.NewRunner(steps, b.config.PackerConfig, ui)
	b.runner.Run(ctx, state)

	// Check for errors
	if rawErr, ok := state.GetOk("error"); ok {
		if err, ok := rawErr.(error); ok {
			return nil, err
		}
		return nil, errors.New("unknown error in state bag")
	}

	// Return artifact
	if rawArtifact, ok := state.GetOk("artifact"); ok {
		if art, ok := rawArtifact.(packersdk.Artifact); ok {
			return art, nil
		}
	}

	// No artifact (all disks were readonly)
	return nil, errors.New("no artifact produced: all disk images are read-only")
}

// normalizeImageType converts a user-supplied image type string to the
// PascalCase variant expected by the Cloud-Hypervisor Rust API (serde default).
// For example, "raw" -> "Raw", "qcow2" -> "Qcow2", "fixedvhd" -> "FixedVhd".
func normalizeImageType(t string) string {
	switch strings.ToLower(t) {
	case "raw":
		return "Raw"
	case "qcow2":
		return "Qcow2"
	case "fixedvhd":
		return "FixedVhd"
	case "vhdx":
		return "Vhdx"
	case "":
		return ""
	default:
		return pascalCase(t)
	}
}

// normalizeConsoleMode converts a user-supplied console/serial mode string to
// the PascalCase variant expected by Cloud-Hypervisor's serde deserialization.
func normalizeConsoleMode(m string) string {
	switch strings.ToLower(m) {
	case "off":
		return "Off"
	case "pty":
		return "Pty"
	case "tty":
		return "Tty"
	case "file":
		return "File"
	case "socket":
		return "Socket"
	case "null":
		return "Null"
	case "":
		return ""
	default:
		return pascalCase(m)
	}
}

// pascalCase capitalizes the first letter of a string.
func pascalCase(s string) string {
	if len(s) == 0 {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// buildVMConfig converts the Packer Config into a chclient.VMConfig for the CH
// API.
func (b *Builder) buildVMConfig() *chclient.VMConfig {
	cfg := &b.config

	vmConfig := &chclient.VMConfig{
		Cpus: chclient.CpusConfig{
			BootVcpus: cfg.Vcpus,
			MaxVcpus:  cfg.Vcpus,
		},
		Memory: chclient.MemoryConfig{
			Size: int64(cfg.Memory) * miB,
		},
		Serial:  chclient.SerialConfig{Mode: "Null"},
		Console: chclient.ConsoleConfig{Mode: "Null"},
		Rng:     chclient.RngConfig{Src: "/dev/urandom"},
	}

	// Handle CPU boot count
	if cfg.CPUBoot != nil && *cfg.CPUBoot > 0 {
		vmConfig.Cpus.BootVcpus = *cfg.CPUBoot
	}

	// Handle payload
	if cfg.Kernel != "" {
		payload := &chclient.PayloadConfig{
			Kernel: &cfg.Kernel,
		}
		if cfg.Initramfs != "" {
			payload.Initramfs = &cfg.Initramfs
		}
		if cfg.Cmdline != "" {
			payload.Cmdline = &cfg.Cmdline
		}
		vmConfig.Payload = payload
	} else if cfg.Firmware != "" {
		vmConfig.Payload = &chclient.PayloadConfig{
			Firmware: &cfg.Firmware,
		}
	}

	// Handle disks (normalize image type to PascalCase for CH API)
	for _, disk := range cfg.DiskImages {
		vmConfig.Disks = append(vmConfig.Disks, chclient.DiskConfig{
			Path:      disk.Path,
			Readonly:  disk.Readonly,
			ImageType: normalizeImageType(disk.ImageType),
			ID:        disk.ID,
		})
	}

	// Handle network interfaces
	for _, iface := range cfg.NetworkInterfaces {
		vmConfig.Net = append(vmConfig.Net, chclient.NetConfig{
			Tap:  iface.Tap,
			Mac:  iface.Mac,
			IP:   iface.IP,
			Mask: iface.Mask,
			ID:   iface.ID,
		})
	}

	// Apply serial/console config (normalize modes for CH API)
	if cfg.Serial != "" {
		vmConfig.Serial.Mode = normalizeConsoleMode(cfg.Serial)
	}
	if cfg.Console != "" {
		vmConfig.Console.Mode = normalizeConsoleMode(cfg.Console)
	}
	if cfg.RngSource != "" {
		vmConfig.Rng.Src = cfg.RngSource
	}

	return vmConfig
}
