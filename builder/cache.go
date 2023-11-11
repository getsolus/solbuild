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

	Caches = []Cache{Bazel, Ccache, GoBuild, Sccache}
)

type Cache struct {
	Name     string
	CacheDir string // CacheDir is the chroot-internal cache directory.
}
