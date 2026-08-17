package lightspeedinittools

import (
	"github.com/rh-ecosystem-edge/eco-goinfra/pkg/clients"
	"github.com/rh-ecosystem-edge/eco-gotests/tests/internal/inittools"
	"github.com/rh-ecosystem-edge/eco-gotests/tests/lightspeed/internal/lightspeedconfig"
)

var (
	// APIClient provides API access to cluster.
	APIClient *clients.Settings
	// LightspeedConfig provides access to general configuration parameters.
	LightspeedConfig *lightspeedconfig.LightspeedConfig
)

// init loads all variables automatically when this package is imported. Once package is imported a user has full
// access to all vars within init function. It is recommended to import this package using dot import.
//
//nolint:gochecknoinits // Package initialization pattern used throughout eco-gotests
func init() {
	LightspeedConfig = lightspeedconfig.NewLightspeedConfig()
	APIClient = inittools.APIClient
}
