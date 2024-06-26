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

// Package builder provides all the solbuild specific functionality
package builder

import (
	"fmt"
	"os"
	"path/filepath"
)

// DisableColors controls whether or not to use colours in the display.
// Spelled this way so people don't get confused :P.
var DisableColors bool

// Controls whether or not we generate an ABI report.
var DisableABIReport bool

const (
	// ImagesDir is where we keep the rootfs images for build profiles.
	ImagesDir = "/var/lib/solbuild/images"

	// ImageSuffix is the common suffix for all solbuild images.
	ImageSuffix = ".img"

	// ImageCompressedSuffix is the common suffix for a fetched evobuild image.
	ImageCompressedSuffix = ".img.xz"

	// ImageBaseURI is the storage area for base images.
	ImageBaseURI = "https://solbuild.getsol.us"

	// ImageRootsDir is where updates are performed on base images.
	ImageRootsDir = "/var/lib/solbuild/roots"
)

const (
	// LayersDir is where container layers are cached, identified by their
	// sha256 hashes, e.g. `/var/cache/solbuild/layers/3c0de53d6017469...`
	LayersDir      = "/var/cache/solbuild/layers"
	// sha256sum <file> | awk '{ print $1 }' | xxd -r -p | base58
	LayersFakeHash = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
)

const (
	// PackageCacheDirectory is where we share packages between all builders.
	PackageCacheDirectory = "/var/lib/solbuild/packages"

	// CacheDirectory is where packages' build cache are stored.
	CacheDirectory = "/var/lib/solbuild/cache"

	// Obsolete cache directories. These are only still specified so that the
	// `delete-cache -a` subcommand will remove them. In the future they will
	// be removed.
	ObsoleteCcacheDirectory        = "/var/lib/solbuild/ccache/ypkg"
	ObsoleteLegacyCcacheDirectory  = "/var/lib/solbuild/ccache/legacy"
	ObsoleteSccacheDirectory       = "/var/lib/solbuild/sccache/ypkg"
	ObsoleteLegacySccacheDirectory = "/var/lib/solbuild/sccache/legacy"
)

const (
	// BuildUser is the user that builds will run as inside the chroot.
	BuildUser = "build"

	// BuildUserID is the build user's numerical ID.
	BuildUserID = 1000

	// BuildUserGID is the group to use.
	BuildUserGID = 1000

	// BuildUserHome is the build user's home directory.
	BuildUserHome = "/home/build"

	// BuildUserGecos is the build user's description.
	BuildUserGecos = "solbuild user"

	// BuildUserShell is the system shell for the build user.
	BuildUserShell = "/bin/bash"
)

// ValidImages is a set of known, Solus-published, base profiles.
var ValidImages = []string{
	"main-x86_64",
	"unstable-x86_64",
}

// PathExists is a helper function to determine the existence of a file path.
func PathExists(path string) bool {
	if st, err := os.Stat(path); err == nil && st != nil {
		return true
	}

	return false
}

// IsValidImage will check if the specified profile is a valid one.
func IsValidImage(profile string) bool {
	for _, p := range ValidImages {
		if p == profile {
			return true
		}
	}

	return false
}

// EmitImageError emits the stock response to requesting an invalid image.
func EmitImageError(image string) {
	fmt.Fprintf(os.Stderr, "Error: '%v' is not a known image\n", image)
	fmt.Fprintf(os.Stderr, "Valid images include:\n\n")

	for _, p := range ValidImages {
		fmt.Fprintf(os.Stderr, " * %v\n", p)
	}
}

// EmitProfileError emits a stock response for an invalid profile.
func EmitProfileError(p string) {
	fmt.Fprintf(os.Stderr, "Error: '%v' is not a known profile\n", p)
	fmt.Fprintf(os.Stderr, "Valid profiles include:\n\n")

	profiles, err := GetAllProfiles()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading profiles: %v\n", err)
		return
	}

	if len(profiles) < 1 {
		fmt.Fprintf(os.Stderr, "Fatal: No profiles installed. Reinstall solbuild\n")
		return
	}

	for key := range profiles {
		fmt.Fprintf(os.Stderr, " * %v\n", key)
	}
}

// A BackingImage is the core of any given profile.
type BackingImage struct {
	Name        string // Name of the profile
	ImagePath   string // Absolute path to the .img file
	ImagePathXZ string // Absolute path to the .img.xz file
	ImageURI    string // URI of the image origin
	RootDir     string // Where to mount the backing image for updates
	LockPath    string // Our lock path for update operations
}

// IsInstalled will determine whether the given backing image has been installed
// to the global image directory or not.
func (b *BackingImage) IsInstalled() bool {
	return PathExists(b.ImagePath)
}

// IsFetched will determine whether or not the XZ image itself has been fetched.
func (b *BackingImage) IsFetched() bool {
	return PathExists(b.ImagePathXZ)
}

// NewBackingImage will return a correctly configured backing image for
// usage.
func NewBackingImage(name string) *BackingImage {
	return &BackingImage{
		Name:        name,
		ImagePath:   filepath.Join(ImagesDir, name+ImageSuffix),
		ImagePathXZ: filepath.Join(ImagesDir, name+ImageCompressedSuffix),
		ImageURI:    fmt.Sprintf("%s/%s%s", ImageBaseURI, name, ImageCompressedSuffix),
		LockPath:    filepath.Join(ImagesDir, name+".lock"),
		RootDir:     filepath.Join(ImageRootsDir, name),
	}
}
