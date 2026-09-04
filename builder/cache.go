package builder

import (
	"path"
)

var (
	Ccache = Cache{
		Name:     "ccache",
		CacheDir: path.Join(BuildUserHome, ".ccache"),
	}

	Sccache = Cache{
		Name:     "sccache",
		CacheDir: path.Join(BuildUserHome, ".cache", "sccache"),
	}

	Bazel = Cache{
		Name:     "bazel",
		CacheDir: path.Join(BuildUserHome, ".cache", "bazel"),
	}

	GoBuild = Cache{
		Name:     "go-build",
		CacheDir: path.Join(BuildUserHome, ".cache", "go-build"),
	}
	LtoCache = Cache{
		Name:     "ltocache",
		CacheDir: path.Join(BuildUserHome, ".cache", "ltocache"),
	}

	Caches = []Cache{Bazel, Ccache, GoBuild, LtoCache, Sccache}
)

type Cache struct {
	Name     string
	CacheDir string // CacheDir is the chroot-internal cache directory.
}
