package main

import (
	"fmt"
	"os"

	"github.com/hashicorp/packer-plugin-sdk/plugin"

	cloudhypervisor "github.com/moeryomenko/packer-plugin-cloud-hypervisor/builder/cloud-hypervisor"
	"github.com/moeryomenko/packer-plugin-cloud-hypervisor/version"
)

func main() {
	pps := plugin.NewSet()
	pps.RegisterBuilder(plugin.DEFAULT_NAME, new(cloudhypervisor.Builder))
	pps.SetVersion(version.PluginVersion)
	err := pps.Run()
	if err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(1)
	}
}
