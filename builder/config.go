//
// Copyright © 2016-2021 Solus Project <copyright@getsol.us>
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
//

package builder

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

// Config defines the global defaults for solbuild.
type Config struct {
	DefaultProfile string `toml:"default_profile"`  // Name of the default profile to use
	EnableHistory  bool   `toml:"enable_history"`   // Whether to enable history generation or not
	EnableTmpfs    bool   `toml:"enable_tmpfs"`     // Whether to enable tmpfs builds or
	OverlayRootDir string `toml:"overlay_root_dir"` // Custom Overlay Root Dir
	TmpfsSize      string `toml:"tmpfs_size"`       // Bounding size on the tmpfs
}

var (
	// ConfigPaths is a set of locations for valid solbuild configuration files.
	ConfigPaths = []string{
		"/etc/solbuild",
		"/usr/share/solbuild",
	}

	// ConfigSuffix is the suffix a file must have to be glob loaded by solbuild.
	ConfigSuffix = ".conf"
)

// NewConfig will read all the system config files and then the vendor config files
// until it gets somewhere.
func NewConfig() (*Config, error) {
	// Set up some sane defaults just in case someone mangles the configs
	config := &Config{
		DefaultProfile: "main-x86_64",
		EnableHistory:  false,
		EnableTmpfs:    false,
		OverlayRootDir: "/var/cache/solbuild",
		TmpfsSize:      "",
	}

	// Reverse because /etc takes precedence in stateless
	for i := len(ConfigPaths) - 1; i >= 0; i-- {
		globPat := filepath.Join(ConfigPaths[i], fmt.Sprintf("*%s", ConfigSuffix))

		configs, _ := filepath.Glob(globPat)

		// Load all globbed configs, using the same Config instance, to keep
		// setting the new flags/etc/
		for _, p := range configs {
			// Read the config file
			fi, err := os.Open(p)
			if err != nil {
				return nil, err
			}

			var b []byte

			// We don't defer the close because of the amount of files we could
			// potentially glob & open, we don't want to take the piss with open
			// file descriptors.
			if b, err = io.ReadAll(fi); err != nil {
				fi.Close()
				return nil, err
			}

			fi.Close()

			if _, err = toml.Decode(string(b), config); err != nil {
				return nil, err
			}
		}
	}

	return config, nil
}
