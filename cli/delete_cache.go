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
	"io/fs"
	"log/slog"
	"math"
	"os"

	"github.com/DataDrake/cli-ng/v2/cmd"
	"github.com/charlievieth/fastwalk"

	"github.com/getsolus/solbuild/builder"
	"github.com/getsolus/solbuild/builder/source"
	"github.com/getsolus/solbuild/cli/log"
)

func init() {
	cmd.Register(&DeleteCache)
}

// DeleteCache cleans up the solbuild caches to free up disk space.
var DeleteCache = cmd.Sub{
	Name:  "delete-cache",
	Alias: "dc",
	Short: "Delete assets stored on disk by solbuild",
	Flags: &DeleteCacheFlags{},
	Run:   DeleteCacheRun,
}

// DeleteCacheFlags are the flags for the "delete-cache" sub-command.
type DeleteCacheFlags struct {
	All    bool `short:"a" long:"all"    desc:"Additionally delete (s)ccache, packages and sources"`
	Images bool `short:"i" long:"images" desc:"Additionally delete solbuild images"`
	Sizes  bool `short:"s" long:"sizes"  desc:"Deprecated: use 'show-cache' instead"`
}

// DeleteCacheRun carries out the "delete-cache" sub-command.
func DeleteCacheRun(r *cmd.Root, s *cmd.Sub) {
	rFlags := r.Flags.(*GlobalFlags)      //nolint:forcetypeassert // guaranteed by callee.
	sFlags := s.Flags.(*DeleteCacheFlags) //nolint:forcetypeassert // guaranteed by callee.

	if rFlags.Debug {
		log.Level.Set(slog.LevelDebug)
	}

	if rFlags.NoColor {
		log.SetUncoloredLogger()
	}

	if os.Geteuid() != 0 {
		log.Panic("You must be root to delete caches")
	}

	manager, err := builder.NewManager()
	if err != nil {
		log.Panic("Failed to create new Manager: %e\n", err)
	}

	// If sizes is requested just print disk usage of caches and return
	if sFlags.Sizes {
		showCacheSizes(manager)

		return
	}

	// By default include /var/cache/solbuild
	nukeDirs := []string{
		manager.Config.OverlayRootDir,
	}
	if sFlags.All {
		nukeDirs = append(nukeDirs, []string{
			builder.CacheDirectory,
			builder.ObsoleteCcacheDirectory,
			builder.ObsoleteSccacheDirectory,
			builder.ObsoleteLegacyCcacheDirectory,
			builder.ObsoleteLegacySccacheDirectory,
			builder.PackageCacheDirectory,
			source.SourceDir,
		}...)
	}

	if sFlags.Images {
		nukeDirs = append(nukeDirs, []string{builder.ImagesDir}...)
	}

	var totalSize int64

	for _, p := range nukeDirs {
		size, err := deleteDir(p)
		if err != nil {
			slog.Warn(fmt.Sprintf("Failed to remove cache directory '%s', reason: '%s'\n", p, err))
		}

		slog.Info(fmt.Sprintf("Removed cache directory '%s', of size '%s", p, humanReadableFormat(float64(size))))

		totalSize += size
	}

	if totalSize > 0 {
		slog.Info(fmt.Sprintf("Total restored size: '%s'\n", humanReadableFormat(float64(totalSize))))
	}
}

func deleteDir(path string) (int64, error) {
	var totalSize int64

	// Return nothing if dir doesn't exist
	_, err := os.Stat(path)
	if os.IsNotExist(err) {
		slog.Debug("Directory doesn't exist", "path", path)
		return 0, nil
	}

	walkConf := fastwalk.Config{
		Follow: false,
	}

	/* Parallelized file walk */
	err = fastwalk.Walk(&walkConf, path, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if !d.IsDir() {
			file, err2 := d.Info()
			if err2 != nil {
				return err2
			}

			totalSize += file.Size()

			/* Remove if file */
			if d.Type().IsRegular() {
				if err = os.Remove(path); err != nil {
					return err
				}
			}
		}

		return err
	})

	/* Remove the remaining directories */
	/* Remove() instead of RemoveAll() would be slightly faster here but the
	 * dirs would need to be sorted depth-first from the file walk */
	if removeErr := os.RemoveAll(path); removeErr != nil {
		slog.Warn("Could not remove directory", "reason", removeErr)
	}

	return totalSize, err
}

// getDirSize returns the disk usage of a directory.
func getDirSize(path string) (int64, error) {
	var totalSize int64

	// Return nothing if dir doesn't exist
	_, err := os.Stat(path)
	if os.IsNotExist(err) {
		slog.Debug("Directory doesn't exist", "path", path)
		return 0, nil
	}

	walkConf := fastwalk.Config{
		Follow: false,
	}

	// Walk the dir, get size, add to totalSize
	err = fastwalk.Walk(&walkConf, path, func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if !d.IsDir() {
			file, err2 := d.Info()
			if err2 != nil {
				return err2
			}

			totalSize += file.Size()
		}

		return err
	})

	return totalSize, err
}

// humanReadableFormat pretty prints a float64 input into a human friendly string in IEC format.
func humanReadableFormat(i float64) string {
	if i <= 0 {
		return "0.0 B"
	}

	units := []string{"B", "KiB", "MiB", "GiB", "TiB", "PiB", "EiB"}
	chosenUnit := math.Min(math.Floor(math.Log(i)/math.Log(1024)), float64(len(units)-1))

	return fmt.Sprintf("%.1f %s", i/math.Pow(1024, chosenUnit), units[int64(chosenUnit)])
}
