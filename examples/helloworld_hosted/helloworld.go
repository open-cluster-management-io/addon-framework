package helloworld_hosted //nolint:revive

import (
	"embed"
)

const (
	AddonName = "helloworldhosted"
)

//go:embed manifests
//go:embed manifests/templates
var FS embed.FS
