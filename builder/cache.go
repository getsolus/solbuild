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

	Caches = []Cache{Ccache, Sccache}
)

type Cache struct {
	Name     string
	CacheDir string // CacheDir is the chroot-internal cache directory.
}
