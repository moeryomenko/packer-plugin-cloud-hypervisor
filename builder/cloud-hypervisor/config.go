//go:generate packer-sdc mapstructure-to-hcl2 -type Config,DiskImage,NetworkInterface

package cloudhypervisor

import (
	"errors"
	"fmt"
	"os"
	"reflect"
	"strings"

	"github.com/hashicorp/packer-plugin-sdk/common"
	"github.com/hashicorp/packer-plugin-sdk/communicator"
	packersdk "github.com/hashicorp/packer-plugin-sdk/packer"
	"github.com/hashicorp/packer-plugin-sdk/template/config"
	"github.com/hashicorp/packer-plugin-sdk/template/interpolate"
)

const BuilderID = "eryoma.cloud-hypervisor"

const (
	minMemoryMiB        = 128
	defaultChBinaryPath = "cloud-hypervisor"
	defaultSerialMode   = "Null"
	commNone            = "none"
)

type Config struct {
	common.PackerConfig `mapstructure:",squash"`
	CommConfig          communicator.Config `mapstructure:",squash"`

	ChBinaryPath string `mapstructure:"ch_binary_path"`
	ChSocketPath string `mapstructure:"ch_socket_path"`

	Vcpus   int  `mapstructure:"vcpus"`
	Memory  int  `mapstructure:"memory"`
	CPUBoot *int `mapstructure:"cpu_boot"`

	Kernel    string `mapstructure:"kernel"`
	Initramfs string `mapstructure:"initramfs"`
	Cmdline   string `mapstructure:"cmdline"`
	Firmware  string `mapstructure:"firmware"`

	DiskImages        []DiskImage        `mapstructure:"disk_images"`
	NetworkInterfaces []NetworkInterface `mapstructure:"network_interfaces"`

	Serial      string `mapstructure:"serial"`
	SerialFile  string `mapstructure:"serial_file"`
	Console     string `mapstructure:"console"`
	ConsoleFile string `mapstructure:"console_file"`
	RngSource   string `mapstructure:"rng_source"`
}

type DiskImage struct {
	Path      string `mapstructure:"path"`
	Readonly  bool   `mapstructure:"readonly"`
	ImageType string `mapstructure:"image_type"`
	ID        string `mapstructure:"id"`
}

type NetworkInterface struct {
	Tap  string `mapstructure:"tap"`
	Mac  string `mapstructure:"mac"`
	IP   string `mapstructure:"ip"`
	Mask string `mapstructure:"mask"`
	ID   string `mapstructure:"id"`
}

// knownKeys returns a set of all mapstructure tag keys that the Config struct
// (and its embedded types) can accept. This is used to filter out unknown keys
// from raw configuration before passing to config.Decode, because the
// packer-plugin-sdk's Decode function rejects unknown keys.
func knownKeys() map[string]bool {
	keys := map[string]bool{}
	collectKeys(reflect.TypeFor[Config](), keys)
	return keys
}

func collectKeys(t reflect.Type, keys map[string]bool) {
	if t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct {
		return
	}
	for f := range t.Fields() {
		tag := f.Tag.Get("mapstructure")
		if tag == "" || tag == "-" {
			continue
		}
		parts := strings.Split(tag, ",")
		name := parts[0]
		isSquash := len(parts) > 1 && parts[1] == "squash"
		if name == "" && isSquash {
			// Recurse into embedded structs
			collectKeys(f.Type, keys)
			continue
		}
		if name != "" {
			keys[name] = true
		}
		// Also recurse into named fields with squash to collect their keys
		if name != "" && isSquash {
			collectKeys(f.Type, keys)
		}
	}
}

// filterRaws removes keys from raw config maps that are not known to the
// Config struct. This prevents config.Decode from returning "unknown
// configuration key" errors for fields like "some_unknown_field" that may
// appear in test fixtures or template leftovers.
func filterRaws(raws []any, keys map[string]bool) []any {
	filtered := make([]any, len(raws))
	for i, raw := range raws {
		m, ok := raw.(map[string]any)
		if !ok {
			filtered[i] = raw
			continue
		}
		nm := make(map[string]any, len(m))
		for k, v := range m {
			// Always allow packer_* and type keys (they are handled by
			// PackerCore or explicitly allowed by the SDK).
			if strings.HasPrefix(k, "packer_") || k == "type" {
				nm[k] = v
				continue
			}
			if keys[k] {
				nm[k] = v
			}
		}
		filtered[i] = nm
	}
	return filtered
}

func (c *Config) Prepare(raws ...any) ([]string, error) {
	// Build known keys once and cache.
	known := knownKeys()

	// Filter raws to remove keys that the Config struct does not know about,
	// preventing "unknown configuration key" errors from config.Decode.
	raws = filterRaws(raws, known)

	// Set defaults before decode so user-provided values can override them.
	c.ChBinaryPath = defaultChBinaryPath
	c.Serial = defaultSerialMode
	c.Console = defaultSerialMode

	err := config.Decode(c, &config.DecodeOpts{
		PluginType:        BuilderID,
		Interpolate:       true,
		InterpolateFilter: &interpolate.RenderFilter{},
	}, raws...)
	if err != nil {
		return nil, fmt.Errorf("config decode: %w", err)
	}

	// Initialize communicator defaults (SSHPort=22, etc.) and validate the
	// communicator config. This MUST run after config.Decode so the
	// user-supplied values (ssh_username, ssh_private_key_file, ...) are
	// populated first; validating the zero-value config before decode made
	// every build fail with "An ssh_username must be specified".
	if errs := c.CommConfig.Prepare(&interpolate.Context{}); len(errs) > 0 {
		var msgs []string
		for _, e := range errs {
			msgs = append(msgs, e.Error())
		}
		return nil, fmt.Errorf("communicator config: %s", strings.Join(msgs, "; "))
	}

	var warnings []string
	var errs *packersdk.MultiError

	// Default ChSocketPath to a temp file path if not provided.
	if c.ChSocketPath == "" {
		f, err := os.CreateTemp("", "ch-socket-*")
		if err != nil {
			return nil, fmt.Errorf("failed to create default ch_socket_path: %w", err)
		}
		c.ChSocketPath = f.Name()
		f.Close()
		os.Remove(c.ChSocketPath)
	}

	// Validate ch_binary_path if it differs from the default.
	if c.ChBinaryPath != defaultChBinaryPath {
		info, err := os.Stat(c.ChBinaryPath)
		if err != nil {
			errs = packersdk.MultiErrorAppend(errs,
				fmt.Errorf("ch_binary_path: %w", err))
		} else if info.IsDir() {
			errs = packersdk.MultiErrorAppend(errs,
				errors.New("ch_binary_path must be a regular file, not a directory"))
		}
	}

	// Validate payload: kernel and firmware are mutually exclusive; at least
	// one must be set.
	hasKernel := c.Kernel != ""
	hasFirmware := c.Firmware != ""

	if !hasKernel && !hasFirmware {
		errs = packersdk.MultiErrorAppend(errs,
			errors.New("either kernel or firmware must be specified"))
	}
	if hasKernel && hasFirmware {
		errs = packersdk.MultiErrorAppend(errs,
			errors.New("kernel and firmware are mutually exclusive"))
	}

	// Validate kernel file existence.
	if hasKernel {
		info, err := os.Stat(c.Kernel)
		if err != nil {
			errs = packersdk.MultiErrorAppend(errs,
				fmt.Errorf("kernel: %w", err))
		} else if info.IsDir() {
			errs = packersdk.MultiErrorAppend(errs,
				errors.New("kernel must be a regular file, not a directory"))
		}
	}

	// Validate initramfs file existence if set.
	if c.Initramfs != "" {
		info, err := os.Stat(c.Initramfs)
		if err != nil {
			errs = packersdk.MultiErrorAppend(errs,
				fmt.Errorf("initramfs: %w", err))
		} else if info.IsDir() {
			errs = packersdk.MultiErrorAppend(errs,
				errors.New("initramfs must be a regular file, not a directory"))
		}
	}

	// Validate firmware file existence.
	if hasFirmware {
		info, err := os.Stat(c.Firmware)
		if err != nil {
			errs = packersdk.MultiErrorAppend(errs,
				fmt.Errorf("firmware: %w", err))
		} else if info.IsDir() {
			errs = packersdk.MultiErrorAppend(errs,
				errors.New("firmware must be a regular file, not a directory"))
		}
	}

	// Validate vcpus >= 1.
	if c.Vcpus < 1 {
		errs = packersdk.MultiErrorAppend(errs,
			fmt.Errorf("vcpus must be >= 1, got %d", c.Vcpus))
	}

	// Validate memory >= 128 MiB.
	if c.Memory < minMemoryMiB {
		errs = packersdk.MultiErrorAppend(errs,
			fmt.Errorf("memory must be >= 128 MiB, got %d", c.Memory))
	}

	// Validate cpu_boot if set: must be >= 1 and <= vcpus.
	if c.CPUBoot != nil {
		if *c.CPUBoot < 1 {
			errs = packersdk.MultiErrorAppend(errs,
				fmt.Errorf("cpu_boot must be >= 1, got %d", *c.CPUBoot))
		}
		if *c.CPUBoot > c.Vcpus {
			errs = packersdk.MultiErrorAppend(errs,
				fmt.Errorf("cpu_boot (%d) must be <= vcpus (%d)", *c.CPUBoot, c.Vcpus))
		}
	}

	// Validate disk images: each path must exist and be a regular file.
	allReadonly := len(c.DiskImages) > 0
	for _, disk := range c.DiskImages {
		info, err := os.Stat(disk.Path)
		if err != nil {
			errs = packersdk.MultiErrorAppend(errs,
				fmt.Errorf("disk image %q: %w", disk.Path, err))
		} else if info.IsDir() {
			errs = packersdk.MultiErrorAppend(errs,
				fmt.Errorf("disk image %q must be a regular file, not a directory", disk.Path))
		}
		if !disk.Readonly {
			allReadonly = false
		}
	}
	if allReadonly && len(c.DiskImages) > 0 {
		warnings = append(warnings,
			"all disk images are read-only; no disk will be writable for artifact capture")
	}

	// Validate network for explicitly set communicators.
	// If the user explicitly requested SSH or WinRM, they must provide at
	// least one network interface for connectivity. When the communicator
	// type is not explicitly set (defaults to SSH) and no network interfaces
	// are configured, we assume a headless build that does not need
	// provisioning connectivity.
	explicitComm := ""
	for _, raw := range raws {
		if m, ok := raw.(map[string]any); ok {
			if v, ok := m["communicator"]; ok {
				if s, ok := v.(string); ok {
					explicitComm = s
				}
				break
			}
		}
	}
	if explicitComm != "" && explicitComm != commNone && len(c.NetworkInterfaces) == 0 {
		errs = packersdk.MultiErrorAppend(errs,
			fmt.Errorf("at least one network_interface must be specified when communicator is %q", explicitComm))
	}

	if errs != nil && len(errs.Errors) > 0 {
		return warnings, errs
	}

	return warnings, nil
}
