package cloudhypervisor

import (
	"context"
	"errors"
	"fmt"

	"github.com/hashicorp/packer-plugin-sdk/multistep"
	packersdk "github.com/hashicorp/packer-plugin-sdk/packer"

	"github.com/moeryomenko/packer-plugin-cloud-hypervisor/builder/cloud-hypervisor/chclient"
)

// StepCreateVM sends a VM creation request to Cloud-Hypervisor with the
// provided VMConfig.
type StepCreateVM struct {
	VMConfig *chclient.VMConfig
}

// Run sends the VM creation request and blocks until the API responds. On
// success it returns ActionContinue; on failure it records the error in the
// state bag and returns ActionHalt.
func (s *StepCreateVM) Run(ctx context.Context, state multistep.StateBag) multistep.StepAction {
	ui, ok := state.Get("ui").(packersdk.Ui)
	if !ok {
		err := errors.New("failed to get ui from state bag")
		state.Put("error", err)

		return multistep.ActionHalt
	}

	client, ok := state.Get("ch_client").(*chclient.Client)
	if !ok {
		err := errors.New("failed to get ch_client from state bag")
		state.Put("error", err)
		ui.Error(err.Error())

		return multistep.ActionHalt
	}

	ui.Say("Creating Cloud-Hypervisor VM...")

	if err := client.CreateVM(ctx, s.VMConfig); err != nil {
		err := fmt.Errorf("error creating VM: %w", err)
		state.Put("error", err)
		ui.Error(err.Error())

		return multistep.ActionHalt
	}

	return multistep.ActionContinue
}

// Cleanup is a no-op for this step.
func (s *StepCreateVM) Cleanup(_ multistep.StateBag) {}
