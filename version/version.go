package version

import sdkVersion "github.com/hashicorp/packer-plugin-sdk/version"

// PluginVersion is the semantic version of the plugin, used for plugin set
// registration and CLI describe output.
//
//nolint:gochecknoglobals
var PluginVersion = sdkVersion.InitializePluginVersion("0.0.1", "")
