package cloudhypervisor

import (
	"context"
	"errors"
	"fmt"

	"github.com/hashicorp/packer-plugin-sdk/multistep"
	packersdk "github.com/hashicorp/packer-plugin-sdk/packer"

	"github.com/moeryomenko/packer-plugin-cloud-hypervisor/builder/cloud-hypervisor/chclient"
)

// StepBootVm sends a boot request to the previously created Cloud-Hypervisor
// VM.
type StepBootVM struct{}

// Run sends the VM boot request and blocks until the API responds. On success
// it returns ActionContinue; on failure it records the error in the state bag
// and returns ActionHalt.
func (s *StepBootVM) Run(ctx context.Context, state multistep.StateBag) multistep.StepAction {
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

	ui.Say("Booting Cloud-Hypervisor VM...")

	if err := client.BootVM(ctx); err != nil {
		err := fmt.Errorf("error booting VM: %w", err)
		state.Put("error", err)
		ui.Error(err.Error())

		return multistep.ActionHalt
	}

	return multistep.ActionContinue
}

// Cleanup is a no-op for this step.
func (s *StepBootVM) Cleanup(_ multistep.StateBag) {}
