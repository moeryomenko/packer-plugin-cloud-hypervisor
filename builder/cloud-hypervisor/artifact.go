package cloudhypervisor

import (
	"fmt"
	"os"
	"path/filepath"

	registryimage "github.com/hashicorp/packer-plugin-sdk/packer/registry/image"
)

// Artifact represents the set of disk images produced by a Cloud-Hypervisor
// build. It implements packersdk.Artifact.
type Artifact struct {
	Dir       string
	FilesList []string
}

// BuilderId returns the builder identifier for this artifact.
//
//nolint:revive // method name mandated by packersdk.Artifact interface
func (a *Artifact) BuilderId() string {
	return BuilderID
}

// Files returns the list of artifact file paths.
func (a *Artifact) Files() []string {
	return a.FilesList
}

// Id returns the base name of the output directory as a simple identifier.
//
//nolint:revive // method name mandated by packersdk.Artifact interface
func (a *Artifact) Id() string {
	return filepath.Base(a.Dir)
}

// String returns a human-readable description of the artifact.
func (a *Artifact) String() string {
	return "Cloud-Hypervisor images in: " + a.Dir
}

// State returns additional artifact metadata. It supports the "generated_data"
// key and the registry image state URI.
func (a *Artifact) State(name string) any {
	switch name {
	case "generated_data":
		return map[string]any{
			"artifact_id": a.Id(),
		}
	case registryimage.ArtifactStateURI:
		return a.artifactState()
	default:
		return nil
	}
}

// Destroy removes the entire output directory and its contents.
func (a *Artifact) Destroy() error {
	if err := os.RemoveAll(a.Dir); err != nil {
		return fmt.Errorf("error destroying artifact: %w", err)
	}

	return nil
}

// artifactState returns the registry image state for the artifact. For now
// this returns nil (no registry image).
func (a *Artifact) artifactState() any {
	return nil
}
