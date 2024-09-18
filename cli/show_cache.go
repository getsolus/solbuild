package cli

import (
	"fmt"
	"log/slog"

	"github.com/DataDrake/cli-ng/v2/cmd"

	"github.com/getsolus/solbuild/builder"
	"github.com/getsolus/solbuild/builder/source"
	"github.com/getsolus/solbuild/cli/log"
)

func init() {
	cmd.Register(&ShowCache)
}

// ShowCache shows the disk usage of the solbuild cache.
var ShowCache = cmd.Sub{
	Name:  "show-cache",
	Alias: "sc",
	Short: "Show the size of assets stored on disk by solbuild",
	Run:   ShowCacheRun,
}

func ShowCacheRun(r *cmd.Root, s *cmd.Sub) {
	rFlags := r.Flags.(*GlobalFlags) //nolint:forcetypeassert // guaranteed by callee.

	if rFlags.Debug {
		log.Level.Set(slog.LevelDebug)
	}

	if rFlags.NoColor {
		log.SetUncoloredLogger()
	}

	manager, err := builder.NewManager()
	if err != nil {
		log.Panic("Failed to create new Manager: %e\n", err)
	}

	showCacheSizes(manager)
}

func showCacheSizes(manager *builder.Manager) {
	sizeDirs := []string{
		manager.Config.OverlayRootDir,
		builder.CacheDirectory,
		builder.ObsoleteCcacheDirectory,
		builder.ObsoleteSccacheDirectory,
		builder.ObsoleteLegacyCcacheDirectory,
		builder.ObsoleteLegacySccacheDirectory,
		builder.PackageCacheDirectory,
		source.SourceDir,
	}

	var totalSize int64

	for _, p := range sizeDirs {
		size, err := getDirSize(p)
		totalSize += size

		if err != nil {
			slog.Warn("Couldn't get directory size", "reason", err)
		}

		slog.Info(fmt.Sprintf("Size of '%s' is '%s'", p, humanReadableFormat(float64(size))))
	}

	slog.Info(fmt.Sprintf("Total size: '%s'", humanReadableFormat(float64(totalSize))))
}
