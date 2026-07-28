package cloudhypervisor

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/hashicorp/packer-plugin-sdk/multistep"
	packersdk "github.com/hashicorp/packer-plugin-sdk/packer"
)

// StepCollectArtifact copies writable disk images to the output directory and
// creates an Artifact from them.
type StepCollectArtifact struct {
	Config    *Config
	OutputDir string
}

const dirPerm = 0o755

// Run copies non-readonly disk images to the output directory and places the
// resulting Artifact in the state bag under the "artifact" key.
func (s *StepCollectArtifact) Run(_ context.Context, state multistep.StateBag) multistep.StepAction {
	ui, ok := state.Get("ui").(packersdk.Ui)
	if !ok {
		err := errors.New("failed to get ui from state bag")
		state.Put("error", err)
		return multistep.ActionHalt
	}

	// Collect writable disk files
	var diskFiles []string
	for _, disk := range s.Config.DiskImages {
		if !disk.Readonly {
			diskFiles = append(diskFiles, disk.Path)
		}
	}

	if len(diskFiles) == 0 {
		ui.Say("No writable disks to collect as artifact")
		return multistep.ActionContinue
	}

	// Create output directory
	if err := os.MkdirAll(s.OutputDir, dirPerm); err != nil {
		err := fmt.Errorf("error creating output directory: %w", err)
		state.Put("error", err)
		ui.Error(err.Error())
		return multistep.ActionHalt
	}

	// Copy disk files
	var copiedFiles []string
	for _, src := range diskFiles {
		dest := filepath.Join(s.OutputDir, filepath.Base(src))
		if err := copyFile(src, dest); err != nil {
			err := fmt.Errorf("error copying disk %s to %s: %w", src, dest, err)
			state.Put("error", err)
			ui.Error(err.Error())
			return multistep.ActionHalt
		}
		copiedFiles = append(copiedFiles, dest)
	}

	// Create artifact
	artifact := &Artifact{
		Dir:       s.OutputDir,
		FilesList: copiedFiles,
	}
	state.Put("artifact", artifact)
	ui.Say(fmt.Sprintf("Created artifact with %d disk image(s)", len(copiedFiles)))

	return multistep.ActionContinue
}

// Cleanup is a no-op for this step.
func (s *StepCollectArtifact) Cleanup(_ multistep.StateBag) {}

// copyFile copies a file from src to dst. The destination is truncated if it
// already exists.
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("error opening source file %s: %w", src, err)
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return fmt.Errorf("error creating destination file %s: %w", dst, err)
	}

	_, err = io.Copy(out, in)
	if err != nil {
		out.Close()
		return fmt.Errorf("error copying %s to %s: %w", src, dst, err)
	}

	if err := out.Close(); err != nil {
		return fmt.Errorf("error closing destination file %s: %w", dst, err)
	}
	return nil
}
