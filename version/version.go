package version

import sdkVersion "github.com/hashicorp/packer-plugin-sdk/version"

var (
	// Version is the main version number that is being run at the moment.
	// It is overwritten at build time via ldflags to match the released tag.
	//
	//nolint:gochecknoglobals
	Version = "0.0.1"

	// VersionPrerelease is a pre-release marker for the version. If this is ""
	// (empty string) then it means that it is a final release. Otherwise, this
	// is a pre-release such as "dev" (in development), "beta", "rc1", etc.
	//
	//nolint:gochecknoglobals
	VersionPrerelease = ""

	// PluginVersion is the semantic version of the plugin, used for plugin set
	// registration and CLI describe output.
	//
	//nolint:gochecknoglobals
	PluginVersion = sdkVersion.InitializePluginVersion(Version, VersionPrerelease)
)
