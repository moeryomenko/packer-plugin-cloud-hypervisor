package cloudhypervisor

import (
	"net"
	"strings"

	"github.com/hashicorp/packer-plugin-sdk/multistep"
)

// stripCIDR returns just the IP address without the CIDR netmask suffix.
// For example, "10.0.2.15/24" -> "10.0.2.15", "192.168.1.1" -> "192.168.1.1".
func stripCIDR(ip string) string {
	if host, _, err := net.ParseCIDR(ip); err == nil {
		return host.String()
	}
	return ip
}

// CommHost returns a function that determines the SSH host address from the
// first network interface with an IP address. When no IP address is
// configured, it returns an empty string.
// The IP string may include a CIDR suffix (e.g. "10.0.2.15/24"); the suffix
// is stripped before returning so that Packer's communicator can connect.
func CommHost(cfg *Config) func(multistep.StateBag) (string, error) {
	return func(_ multistep.StateBag) (string, error) {
		for _, iface := range cfg.NetworkInterfaces {
			raw := strings.TrimSpace(iface.IP)
			if raw != "" {
				return stripCIDR(raw), nil
			}
		}
		return "", nil
	}
}
