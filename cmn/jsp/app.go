// Package jsp (JSON persistence) provides utilities to store and load arbitrary
// JSON-encoded structures with optional checksumming and compression.
/*
 * Copyright (c) 2018-2022, NVIDIA CORPORATION. All rights reserved.
 */
package jsp

import (
	"fmt"
	"path/filepath"

	"github.com/NVIDIA/aistore/cmn/cos"
	"github.com/NVIDIA/aistore/cmn/debug"
)

// NOTE:
// The default app config directory is: $HOME/.config/ais
// e.g.: $HOME/.config/ais/cli.json 			- CLI config
//       $HOME/.config/ais/auth.token 			- authentication token
//       $HOME/.config/ais/<bucket>.aisfs.mount.json 	- aisfs mount config
func DefaultAppConfigDir() (configDir string) {
	uhome, err := cos.HomeDir()
	if err != nil {
		debug.AssertNoErr(err)
		cos.Errorf("%v", err)
	}
	return filepath.Join(uhome, ".config/ais")
}

// LoadAppConfig loads app config.
func LoadAppConfig(configDir, configFileName string, v interface{}) (err error) {
	// Check if config file exists.
	configFilePath := filepath.Join(configDir, configFileName)
	if err = cos.Stat(configFilePath); err != nil {
		return err
	}

	// Load config from file.
	if _, err = Load(configFilePath, v, Options{Indent: true}); err != nil {
		return fmt.Errorf("failed to load config file %q: %v", configFilePath, err)
	}
	return
}

// SaveAppConfig writes app config.
func SaveAppConfig(configDir, configFileName string, v interface{}) (err error) {
	// Check if config dir exists; if not, create one with default config.
	configFilePath := filepath.Join(configDir, configFileName)
	return Save(configFilePath, v, Options{Indent: true}, nil /*sgl*/)
}
