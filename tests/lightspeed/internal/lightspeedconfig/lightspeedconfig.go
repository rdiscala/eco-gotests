package lightspeedconfig

import (
	"log"
	"os"
	"path/filepath"
	"runtime"

	"github.com/kelseyhightower/envconfig"
	"github.com/rh-ecosystem-edge/eco-gotests/tests/internal/config"
	"gopkg.in/yaml.v2"
)

const (
	// PathToDefaultLightspeedParamsFile path to config file with default lightspeed parameters.
	PathToDefaultLightspeedParamsFile = "./default.yaml"
)

// LightspeedConfig type keeps lightspeed configuration.
type LightspeedConfig struct {
	*config.GeneralConfig
}

// NewLightspeedConfig returns instance of LightspeedConfig config type.
func NewLightspeedConfig() *LightspeedConfig {
	log.Print("Creating new LightspeedConfig struct")

	var lightspeedConf LightspeedConfig

	lightspeedConf.GeneralConfig = config.NewConfig()

	_, filename, _, _ := runtime.Caller(0)
	baseDir := filepath.Dir(filename)
	confFile := filepath.Join(baseDir, PathToDefaultLightspeedParamsFile)

	err := readFile(&lightspeedConf, confFile)
	if err != nil {
		log.Printf("Error to read config file %s", confFile)

		return nil
	}

	err = readEnv(&lightspeedConf)
	if err != nil {
		log.Print("Error to read environment variables")

		return nil
	}

	return &lightspeedConf
}

func readFile(lightspeedConfig *LightspeedConfig, cfgFile string) error {
	openedCfgFile, err := os.Open(cfgFile)
	if err != nil {
		return err
	}

	defer func() {
		_ = openedCfgFile.Close()
	}()

	decoder := yaml.NewDecoder(openedCfgFile)

	err = decoder.Decode(&lightspeedConfig)
	if err != nil {
		return err
	}

	return nil
}

func readEnv(lightspeedConfig *LightspeedConfig) error {
	err := envconfig.Process("", lightspeedConfig)
	if err != nil {
		return err
	}

	return nil
}
