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
	"fmt"
	"github.com/DataDrake/cli-ng/v2/cmd"
	log "github.com/DataDrake/waterlog"
	"github.com/DataDrake/waterlog/format"
	"github.com/DataDrake/waterlog/level"
	"github.com/getsolus/solbuild/builder"
	"github.com/getsolus/solbuild/builder/source"
	"math"
	"os"
	"path/filepath"
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
	Sizes  bool `short:"s" long:"sizes"  desc:"Show disk usage of the caches"`
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

	// If sizes is requested just print disk usage of caches and return
	if sFlags.Sizes {
		sizeDirs := []string{
			manager.Config.OverlayRootDir,
			builder.CcacheDirectory,
			builder.LegacyCcacheDirectory,
			builder.SccacheDirectory,
			builder.LegacySccacheDirectory,
			builder.PackageCacheDirectory,
			source.SourceDir,
		}
		var totalSize int64
		for _, p := range sizeDirs {
			size, err := getDirSize(p)
			totalSize += size
			if err != nil {
				log.Warnf("Couldn't get directory size, reason: %s\n", err)
			}
			log.Infof("Size of '%s' is '%s'\n", p, humanReadableFormat(float64(size)))
		}
		log.Infof("Total size: '%s'\n", humanReadableFormat(float64(totalSize)))
		return
	}

	// By default include /var/cache/solbuild
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
	var totalSize int64
	for _, p := range nukeDirs {
		if !builder.PathExists(p) {
			continue
		}
		size, err := getDirSize(p)
		totalSize += size
		if err != nil {
			log.Warnf("Couldn't get directory size, reason: %s\n", err)
		}
		log.Infof("Removing cache directory '%s', of size '%s\n", p, humanReadableFormat(float64(size)))
		if err := os.RemoveAll(p); err != nil {
			log.Fatalf("Could not remove cache directory, reason: %s\n", err)
		}
	}
	if totalSize > 0 {
		log.Infof("Total restored size: '%s'\n", humanReadableFormat(float64(totalSize)))
	}
}

// getDirSize returns the disk usage of a directory
func getDirSize(path string) (int64, error) {
	var totalSize int64

	// Return nothing if dir doesn't exist
	_, err := os.Stat(path)
	if os.IsNotExist(err) {
		log.Debugf("Directory doesn't exist: %s\n", path)
		return 0, nil
	}

	// Walk the dir, get size, add to totalSize
	err = filepath.Walk(path, func(_ string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			totalSize += info.Size()
		}
		return err
	})
	return totalSize, err
}

// humanReadableFormat pretty prints a float64 input into a human friendly string in IEC format
func humanReadableFormat(i float64) string {
	if i <= 0 {
		return fmt.Sprintf("0.0 B")
	}
	units := []string{"B", "KiB", "MiB", "GiB", "TiB", "PiB", "EiB"}
	chosenUnit := math.Min(math.Floor(math.Log(i)/math.Log(1024)), float64(len(units)-1))
	return fmt.Sprintf("%.1f %s", i/math.Pow(1024, chosenUnit), units[int64(chosenUnit)])
}
