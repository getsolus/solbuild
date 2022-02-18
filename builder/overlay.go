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
	"github.com/getsolus/libosdev/commands"
	"github.com/getsolus/libosdev/disk"
	log "github.com/DataDrake/waterlog"
	"os"
	"path/filepath"
)

// An Overlay is formed from a backing image & Package combination.
// Using this Overlay we can bring up new temporary build roots using the
// overlayfs kernel module.
type Overlay struct {
	Back    *BackingImage // This will be mounted at $dir/image
	Package *Package      // The package we intend to interact with

	BaseDir    string // BaseDir is the base directory containing the root
	WorkDir    string // WorkDir is the overlayfs workdir lock
	UpperDir   string // UpperDir is where real inode changes happen (tmp)
	ImgDir     string // Where the profile is mounted (ro)
	MountPoint string // The actual mount point for the union'd directories
	LockPath   string // Path to the lockfile for this overlay

	EnableTmpfs bool   // Whether to use tmpfs for the upperdir or not
	TmpfsSize   string // Size of the tmpfs to pass to mount, string form

	ExtraMounts []string // Any extra mounts to take care of when cleaning up

	mountedImg     bool // Whether we mounted the image or not
	mountedOverlay bool // Whether we mounted the overlay or not
	mountedVFS     bool // Whether we mounted vfs or not
	mountedTmpfs   bool // Whether we mounted tmpfs or not
}

// NewOverlay creates a new Overlay for us in builds, etc.
//
// Unlike evobuild, we use fixed names within the more dynamic profile name,
// as opposed to a single dir with "unstable-x86_64" inside it, etc.
func NewOverlay(config *Config, profile *Profile, back *BackingImage, pkg *Package) *Overlay {
	// Ideally we could make this better..
	dirname := pkg.Name
	// i.e. /var/cache/solbuild/unstable-x86_64/nano
	basedir := filepath.Join(config.OverlayRootDir, profile.Name, dirname)
	return &Overlay{
		Back:           back,
		Package:        pkg,
		BaseDir:        basedir,
		WorkDir:        filepath.Join(basedir, "work"),
		UpperDir:       filepath.Join(basedir, "tmp"),
		ImgDir:         filepath.Join(basedir, "img"),
		MountPoint:     filepath.Join(basedir, "union"),
		LockPath:       fmt.Sprintf("%s.lock", basedir),
		mountedImg:     false,
		mountedOverlay: false,
		mountedVFS:     false,
		EnableTmpfs:    false,
		TmpfsSize:      "",
		mountedTmpfs:   false,
	}
}

// EnsureDirs is a helper to make sure we have all directories in place
func (o *Overlay) EnsureDirs() error {
	paths := []string{
		o.BaseDir,
		o.WorkDir,
		o.UpperDir,
		o.ImgDir,
		o.MountPoint,
	}

	for _, p := range paths {
		if PathExists(p) {
			continue
		}
		log.Debugf("Creating overlay storage directory: %s\n", p)
		if err := os.MkdirAll(p, 00755); err != nil {
			return fmt.Errorf("Failed to create overlay storage directory: dir='%s', reason: %s\n", p, err)
		}
	}
	return nil
}

// CleanExisting will purge an existing overlayfs configuration if it
// exists.
func (o *Overlay) CleanExisting() error {
	if !PathExists(o.BaseDir) {
		return nil
	}
	log.Debugf("Removing stale workspace: %s\n", o.BaseDir)
	if err := os.RemoveAll(o.BaseDir); err != nil {
		return fmt.Errorf("Failed to remove stale workspace: dir='%s', reason: %s\n", o.BaseDir, err)
	}
	return nil
}

// Mount will set up the overlayfs structure with the lower/upper respected
// properly.
func (o *Overlay) Mount() error {
	log.Debugln("Mounting overlayfs")

	mountMan := disk.GetMountManager()

	// Mount tmpfs as the root of all other mounts if requested
	if o.EnableTmpfs {
		if err := os.MkdirAll(o.BaseDir, 00755); err != nil {
			log.Errorf("Failed to create tmpfs directory: dir='%s', reason: %s\n", o.BaseDir, err)
			return nil
		}
		log.Debugf("Mounting root tmpfs: point='%s' size='%s'\n", o.BaseDir, o.TmpfsSize)

		var tmpfsOptions []string
		if o.TmpfsSize != "" {
			tmpfsOptions = append(tmpfsOptions, fmt.Sprintf("size=%s", o.TmpfsSize))
		}
		tmpfsOptions = append(tmpfsOptions, []string{
			"rw",
			"relatime",
		}...)
		if err := mountMan.Mount("tmpfs-root", o.BaseDir, "tmpfs", tmpfsOptions...); err != nil {
			return fmt.Errorf("Failed to mount root tmpfs: point='%s' size='%s', reason: %s\n", o.BaseDir, o.TmpfsSize, err)
		}
	}

	// Set up environment
	if err := o.EnsureDirs(); err != nil {
		return err
	}

	// First up, mount the backing image
	log.Debugf("Mounting backing image: point='%s'\n", o.Back.ImagePath)
	if err := mountMan.Mount(o.Back.ImagePath, o.ImgDir, "auto", "ro", "loop"); err != nil {
		return fmt.Errorf("Failed to mount backing image: point='%s', reason: %s\n", o.Back.ImagePath, err)
	}
	o.mountedImg = true

	// Now mount the overlayfs
    log.Debugf("Mounting overlayfs: upper='%s' lower='%s' workdir='%s' target='%s'\n", o.UpperDir, o.ImgDir, o.WorkDir, o.MountPoint)

	// Mounting overlayfs..
	err := mountMan.Mount("overlay", o.MountPoint, "overlay",
		fmt.Sprintf("lowerdir=%s", o.ImgDir),
		fmt.Sprintf("upperdir=%s", o.UpperDir),
		fmt.Sprintf("workdir=%s", o.WorkDir))

	// Check non-fatal..
	if err != nil {
		log.Fatalf("Failed to mount overlayfs: point='%s', reason: %s\n", o.MountPoint, err)
	}
	o.mountedOverlay = true

	// Must be done here before we do any more overlayfs work
	return EnsureEopkgLayout(o.MountPoint)
}

// Unmount will tear down the overlay mount again
func (o *Overlay) Unmount() error {
	mountMan := disk.GetMountManager()

	for _, m := range o.ExtraMounts {
		mountMan.Unmount(m)
	}
	o.ExtraMounts = nil

	vfsPoints := []string{
		filepath.Join(o.MountPoint, "dev/pts"),
		filepath.Join(o.MountPoint, "dev/shm"),
		filepath.Join(o.MountPoint, "dev"),
		filepath.Join(o.MountPoint, "proc"),
		filepath.Join(o.MountPoint, "sys"),
	}
	if o.mountedVFS {
		for _, p := range vfsPoints {
			mountMan.Unmount(p)
		}
		o.mountedVFS = false
	}

	if o.mountedImg {
		if err := mountMan.Unmount(o.ImgDir); err != nil {
			return err
		}
		o.mountedImg = false
	}
	if o.mountedOverlay {
		if err := mountMan.Unmount(o.MountPoint); err != nil {
			return err
		}
		o.mountedOverlay = false
	}
	if o.mountedTmpfs {
		if err := mountMan.Unmount(o.UpperDir); err != nil {
			return err
		}
		o.mountedTmpfs = false
	}
	return nil
}

// MountVFS will bring up virtual filesystems within the chroot
func (o *Overlay) MountVFS() error {
	mountMan := disk.GetMountManager()

	vfsPoints := []string{
		filepath.Join(o.MountPoint, "dev"),
		filepath.Join(o.MountPoint, "dev/pts"),
		filepath.Join(o.MountPoint, "proc"),
		filepath.Join(o.MountPoint, "sys"),
		filepath.Join(o.MountPoint, "dev/shm"),
	}

	for _, p := range vfsPoints {
		if PathExists(p) {
			continue
		}

		log.Debugf("Creating VFS directory: dir='%s'\n", p)

		if err := os.MkdirAll(p, 00755); err != nil {
			return fmt.Errorf("Failed to create VFS directory. dir='%s', reason: %s\n", p, err)
		}
	}

	// Bring up dev
	log.Debugln("Mounting vfs /dev")
	if err := mountMan.Mount("devtmpfs", vfsPoints[0], "devtmpfs", "nosuid", "mode=755"); err != nil {
		return fmt.Errorf("Failed to mount /dev, reason: %s\n", err)
	}
	o.mountedVFS = true

	// Bring up dev/pts
	log.Debugln("Mounting vfs /dev/pts")
	if err := mountMan.Mount("devpts", vfsPoints[1], "devpts", "gid=5", "mode=620", "nosuid", "noexec"); err != nil {
		return fmt.Errorf("Failed to mount /dev/pts, reason: %s\n", err)
	}

	// Bring up proc
	log.Debugln("Mounting vfs /proc")
	if err := mountMan.Mount("proc", vfsPoints[2], "proc", "nosuid", "noexec"); err != nil {
		return fmt.Errorf("Failed to mount /proc, reason: %s\n", err)
	}

	// Bring up sys
	log.Debugln("Mounting vfs /sys")
	if err := mountMan.Mount("sysfs", vfsPoints[3], "sysfs"); err != nil {
		return fmt.Errorf("Failed to mount /sys, reason: %s\n", err)
	}

	// Bring up shm
	log.Debugln("Mounting vfs /dev/shm")
	if err := mountMan.Mount("tmpfs-shm", vfsPoints[4], "tmpfs"); err != nil {
		return fmt.Errorf("Failed to mount /dev/shm, reason: %s\n", err)
	}
	return nil
}

// ConfigureNetworking will add a loopback interface to the container so
// that localhost networking will still work
func (o *Overlay) ConfigureNetworking() error {
	ipCommand := "/sbin/ip link set lo up"
	log.Debugln("Configuring container networking")
	if err := commands.ChrootExec(o.MountPoint, ipCommand); err != nil {
		return fmt.Errorf("Failed to configure networking, reason: %s\n", err)
	}
	return nil
}
