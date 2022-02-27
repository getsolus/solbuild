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

package cli

import (
	"github.com/DataDrake/cli-ng/v2/cmd"
	log "github.com/DataDrake/waterlog"
	"github.com/DataDrake/waterlog/format"
	"github.com/DataDrake/waterlog/level"
	"github.com/getsolus/solbuild/builder"
	"github.com/getsolus/solbuild/builder/source"
	"os"
)

func init() {
	cmd.Register(&DeleteCache)
}

// DeleteCache cleans up the solbuild caches to free up disk space
var DeleteCache = cmd.Sub{
	Name:  "delete-cache",
	Alias: "dc",
	Short: "Delete assets stored on disk by solbuild",
	Flags: &DeleteCacheFlags{},
	Run:   DeleteCacheRun,
}

// DeleteCacheFlags are the flags for the "delete-cache" sub-command
type DeleteCacheFlags struct {
	All    bool `short:"a" long:"all"    desc:"Additionally delete (s)ccache, packages and sources"`
	Images bool `short:"i" long:"images" desc:"Additionally delete solbuild images"`
}

// DeleteCache carries out the "delete-cache" sub-command
func DeleteCacheRun(r *cmd.Root, s *cmd.Sub) {
	rFlags := r.Flags.(*GlobalFlags)
	sFlags := s.Flags.(*DeleteCacheFlags)
	if rFlags.Debug {
		log.SetLevel(level.Debug)
	}
	if rFlags.NoColor {
		log.SetFormat(format.Un)
	}
	if os.Geteuid() != 0 {
		log.Fatalln("You must be root to delete caches")
	}
	manager, err := builder.NewManager()
	if err != nil {
		log.Fatalf("Failed to create new Manager: %e\n", err)
	}
	// By default include /var/lib/solbuild
	nukeDirs := []string{
		manager.Config.OverlayRootDir,
	}
	if sFlags.All {
		nukeDirs = append(nukeDirs, []string{
			builder.CcacheDirectory,
			builder.LegacyCcacheDirectory,
			builder.SccacheDirectory,
			builder.LegacySccacheDirectory,
			builder.PackageCacheDirectory,
			source.SourceDir,
		}...)
	}
	if sFlags.Images {
		nukeDirs = append(nukeDirs, []string{builder.ImagesDir}...)
	}
	for _, p := range nukeDirs {
		if !builder.PathExists(p) {
			continue
		}
		log.Infof("Removing cache directory '%s'\n", p)
		if err := os.RemoveAll(p); err != nil {
			log.Fatalf("Could not remove cache directory, reason: %s\n", err)
		}
	}
}
