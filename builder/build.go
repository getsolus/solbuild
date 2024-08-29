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
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/getsolus/libosdev/disk"
)

// CreateDirs creates any directories we may need later on.
func (p *Package) CreateDirs(o *Overlay) error {
	dirs := []string{
		p.GetWorkDir(o),
		p.GetSourceDir(o),
	}

	for _, p := range dirs {
		if err := os.MkdirAll(p, 0o0755); err != nil {
			return fmt.Errorf("Failed to create required directory %s. Reason: %w", p, err)
		}
	}

	// Create cache directories
	if p.Type == PackageTypeYpkg {
		for _, cache := range Caches {
			inRootCacheDir := filepath.Join(o.MountPoint, cache.CacheDir[1:])
			hostCacheDir := filepath.Join(CacheDirectory, cache.Name)

			// Cache directories in build root.
			if err := os.MkdirAll(inRootCacheDir, 0o0755); err != nil {
				return fmt.Errorf("Failed to create cache directory %s in build root, reason: %w", inRootCacheDir, err)
			}

			// Cache directory in host.
			// Ensure we have root owned cache directories.
			if err := os.MkdirAll(hostCacheDir, 0o0755); err != nil {
				return fmt.Errorf("Failed to create cache directory %s for %s, reason: %w", cache.CacheDir, cache.Name, err)
			}

			// Ensure the build user can write to the cache directories.
			if err := os.Chown(hostCacheDir, BuildUserID, BuildUserGID); err != nil {
				return fmt.Errorf("Failed to chown cache directory %s in build root, reason: %w", inRootCacheDir, err)
			}
		}
	}

	return nil
}

// FetchSources will attempt to fetch the sources from the network
// if necessary.
func (p *Package) FetchSources(o *Overlay) error {
	for _, source := range p.Sources {
		// Already fetched, skip it
		if source.IsFetched() {
			continue
		}

		if err := source.Fetch(); err != nil {
			return fmt.Errorf("Failed to fetch source %s, reason: %w\n", source.GetIdentifier(), err)
		}
	}

	return nil
}

// BindSources will make the sources available to the chroot by bind mounting
// them into place.
func (p *Package) BindSources(o *Overlay) error {
	mountMan := disk.GetMountManager()

	for _, source := range p.Sources {
		sourceDir := p.GetSourceDir(o)
		bindConfig := source.GetBindConfiguration(sourceDir)

		// Ensure sources tree exists
		if !PathExists(sourceDir) {
			if err := os.MkdirAll(sourceDir, 0o0755); err != nil {
				return fmt.Errorf("Failed to create source directory %s, reason: %w\n", sourceDir, err)
			}
		}

		// Find the target path in the chroot
		slog.Debug("Exposing source to container", "source", bindConfig.BindSource, "target", bindConfig.BindTarget)

		if st, err := os.Stat(bindConfig.BindSource); err == nil && st != nil {
			if st.IsDir() {
				if err := os.MkdirAll(bindConfig.BindTarget, 0o0755); err != nil {
					slog.Error("Failed to create bind mount", "target", bindConfig.BindTarget, "reason", err)
					return nil
				}
			} else {
				if err := TouchFile(bindConfig.BindTarget); err != nil {
					slog.Error("Failed to create bind mount target", "target", bindConfig.BindTarget, "reason", err)
					return nil
				}
			}
		}

		// Bind mount local source into chroot
		if err := mountMan.BindMount(bindConfig.BindSource, bindConfig.BindTarget, "ro"); err != nil {
			return fmt.Errorf("Failed to bind mount source %s, reason: %w\n", bindConfig.BindTarget, err)
		}

		// Account for these to help cleanups
		o.ExtraMounts = append(o.ExtraMounts, bindConfig.BindTarget)
	}

	return nil
}

// BindCache will make all cache defined in [caches] available to the build.
func (p *Package) BindCaches(o *Overlay) error {
	if p.Type == PackageTypeXML {
		return fmt.Errorf("Failed to bind caches, reason: not YPKG build")
	}

	mountMan := disk.GetMountManager()

	for _, c := range Caches {
		cacheSource := filepath.Join(CacheDirectory, c.Name)
		cacheDir := filepath.Join(o.MountPoint, c.CacheDir[1:])

		slog.Debug("Exposing cache to build", "cache", c.Name, "source", cacheSource, "target", cacheDir)

		// Bind mount local ccache into chroot
		if err := mountMan.BindMount(cacheSource, cacheDir); err != nil {
			return fmt.Errorf("Failed to bind mount %s %s, reason: %w\n", c.Name, cacheDir, err)
		}

		o.ExtraMounts = append(o.ExtraMounts, cacheDir)
	}

	return nil
}

// GetWorkDir will return the externally visible work directory for the
// given build type.
func (p *Package) GetWorkDir(o *Overlay) string {
	return filepath.Join(o.MountPoint, p.GetWorkDirInternal()[1:])
}

// GetWorkDirInternal returns the internal chroot path for the work directory.
func (p *Package) GetWorkDirInternal() string {
	if p.Type == PackageTypeXML {
		return "/WORK"
	}

	return filepath.Join(BuildUserHome, "work")
}

// GetSourceDir will return the externally visible work directory.
func (p *Package) GetSourceDir(o *Overlay) string {
	return filepath.Join(o.MountPoint, p.GetSourceDirInternal()[1:])
}

// GetSourceDirInternal will return the chroot-internal source directory
// for the given build type.
func (p *Package) GetSourceDirInternal() string {
	if p.Type == PackageTypeXML {
		return "/var/cache/eopkg/archives"
	}

	return filepath.Join(BuildUserHome, "YPKG", "sources")
}

// CopyAssets will copy all of the required assets into the builder root.
func (p *Package) CopyAssets(h *PackageHistory, o *Overlay) error {
	baseDir := filepath.Dir(p.Path)

	if abs, err := filepath.Abs(baseDir); err == nil {
		baseDir = abs
	} else {
		return err
	}

	copyPaths := []string{
		filepath.Base(p.Path),
		"files",
		"comar",
		"component.xml",
	}

	if p.Type == PackageTypeXML {
		copyPaths = append(copyPaths, "actions.py")
	}

	// This should be changed for ypkg.
	destdir := p.GetWorkDir(o)

	for _, pat := range copyPaths {
		fso := filepath.Join(baseDir, pat)

		newDest := destdir
		if p.Type == PackageTypeXML && pat == "component.xml" {
			newDest = filepath.Dir(destdir)
		}

		if err := CopyAll(fso, newDest); err != nil {
			return err
		}
	}

	if h == nil {
		return nil
	}
	// Write the history file out
	histPath := filepath.Join(destdir, "history.xml")

	return h.WriteXML(histPath)
}

// PrepYpkg will do the initial leg work of preparing us for a ypkg build.
func (p *Package) PrepYpkg(notif PidNotifier, usr *UserInfo, pman *EopkgManager, overlay *Overlay, h *PackageHistory) error {
	slog.Debug("Writing packager file")

	fp := filepath.Join(overlay.MountPoint, BuildUserHome, ".config", "solus", "packager")
	fpd := filepath.Dir(fp)

	if !PathExists(fpd) {
		if err := os.MkdirAll(fpd, 0o0755); err != nil {
			return fmt.Errorf("Failed to create packager directory %s, reason: %w\n", fpd, err)
		}
	}

	if err := usr.WritePackager(fp); err != nil {
		return fmt.Errorf("Failed to write packager file %s, reason: %w\n", fp, err)
	}

	wdir := p.GetWorkDirInternal()
	ymlFile := filepath.Join(wdir, filepath.Base(p.Path))

	cmd := fmt.Sprintf("ypkg-install-deps -f %s", ymlFile)
	if DisableColors {
		cmd += " -n"
	}

	// Install build dependencies
	slog.Debug("Installing build dependencies", "file", ymlFile)

	if err := ChrootExec(notif, overlay.MountPoint, cmd); err != nil {
		return fmt.Errorf("Failed to install build dependencies %s, reason: %w\n", ymlFile, err)
	}

	notif.SetActivePID(0)

	// Cleanup now
	slog.Debug("Stopping D-BUS")

	if err := pman.StopDBUS(); err != nil {
		return fmt.Errorf("Failed to stop d-bus, reason: %w\n", err)
	}

	// Chwn the directory before bringing up sources
	cmd = fmt.Sprintf("chown -R %s:%s %s", BuildUser, BuildUser, BuildUserHome)
	if err := ChrootExec(notif, overlay.MountPoint, cmd); err != nil {
		return fmt.Errorf("Failed to set home directory permissions, reason: %w\n", err)
	}

	notif.SetActivePID(0)

	return nil
}

// BuildYpkg will take care of the ypkg specific build process and is called only
// by Build().
func (p *Package) BuildYpkg(notif PidNotifier, usr *UserInfo, pman *EopkgManager, overlay *Overlay, h *PackageHistory) error {
	if err := p.PrepYpkg(notif, usr, pman, overlay, h); err != nil {
		return err
	}

	// Now kill networking
	if !p.CanNetwork {
		if err := DropNetworking(); err != nil {
			return err
		}

		// Ensure the overlay can network on localhost only
		if err := overlay.ConfigureNetworking(); err != nil {
			return err
		}
	} else {
		slog.Warn("Package has explicitly requested networking, sandboxing disabled")
	}

	// Bring up sources
	if err := p.BindSources(overlay); err != nil {
		return fmt.Errorf("Failed to set home directory permissions, reason: %w\n", err)
	}

	// Reaffirm the layout
	if err := EnsureEopkgLayout(overlay.MountPoint); err != nil {
		return err
	}

	// Ensure we have build caches available
	if err := p.BindCaches(overlay); err != nil {
		return err
	}

	// Now recopy the assets prior to build
	if err := pman.CopyAssets(); err != nil {
		return err
	}

	wdir := p.GetWorkDirInternal()
	ymlFile := filepath.Join(wdir, filepath.Base(p.Path))

	// Now build the package
	// cmd := fmt.Sprintf("/bin/su %s -- fakeroot ypkg-build -D %s %s", BuildUser, wdir, ymlFile)
	// use rootlesskit instead of fatfakeroot
	buildCmd := fmt.Sprintf("ypkg-build -D %s %s", wdir, ymlFile)
	if DisableColors {
		buildCmd += " -n"
	}
	// Pass unix timestamp of last git update
	if h != nil && len(h.Updates) > 0 {
		buildCmd += fmt.Sprintf(" -t %v", h.GetLastVersionTimestamp())
	}

	// need to properly quote the innner -c 'command' syntax
	//suCmd := fmt.Sprintf("strace /bin/su %s --command='%s'", BuildUser, buildCmd)

	if p.CanCCache {
		// Start an sccache server to work around #87
		StartSccache(overlay.MountPoint)
	}

	// ensure that the BuildUser has /etc/sub{g,u}id files present for use with rootlesskit user namespaces
	usermodCmd := fmt.Sprintf("touch /etc/sub{g,u}id && usermod --add-subuids 100000-165535 --add-subgids 100000-165535 %s", BuildUser)
	if err := ChrootExec(notif, overlay.MountPoint, usermodCmd); err != nil {
		slog.Error(fmt.Sprintf("Failed to ensure that user '%s' has /etc/sub{g,u}id files in chroot", BuildUser))
	}

	slog.Info("Now starting build", "package", p.Name)
	slog.Info("Build", "command", buildCmd)

	if err := RootlesskitExec(notif, overlay.MountPoint, buildCmd); err != nil {
		return fmt.Errorf("Failed to start build of package, reason: %w\n", err)
	}

	// Generate ABI Report
	if !DisableABIReport {
		slog.Debug("Attempting to generate ABI report")

		if err := p.GenerateABIReport(notif, overlay); err != nil {
			slog.Warn("Failed to generate ABI report", "reason", err)
			return nil
		}
	}

	notif.SetActivePID(0)

	return nil
}

// BuildXML will take care of building the legacy pspec.xml format, and is called only
// by Build().
func (p *Package) BuildXML(notif PidNotifier, pman *EopkgManager, overlay *Overlay) error {
	// Just straight up build it with eopkg
	slog.Warn("Full sandboxing is not possible with legacy format")

	wdir := p.GetWorkDirInternal()
	xmlFile := filepath.Join(wdir, filepath.Base(p.Path))

	// Bring up sources
	if err := p.BindSources(overlay); err != nil {
		return fmt.Errorf("Cannot continue without sources.\n")
	}

	// Now recopy the assets prior to build
	if err := pman.CopyAssets(); err != nil {
		return err
	}

	// Now build the package, ignore-sandbox in case someone is stupid
	// and activates it in eopkg.conf...
	cmd := eopkgCommand(fmt.Sprintf("eopkg build --ignore-sandbox --yes-all -O %s %s", wdir, xmlFile))

	slog.Info("Now starting build", "package", p.Name)

	if err := ChrootExec(notif, overlay.MountPoint, cmd); err != nil {
		return fmt.Errorf("Failed to start build of package.\n")
	}

	notif.SetActivePID(0)

	// Now we can stop dbus...
	slog.Debug("Stopping D-BUS")

	if err := pman.StopDBUS(); err != nil {
		return fmt.Errorf("Failed to stop d-bus, reason: %w\n", err)
	}

	notif.SetActivePID(0)

	return nil
}

// GenerateABIReport will take care of generating the abireport using abi-wizard.
func (p *Package) GenerateABIReport(notif PidNotifier, overlay *Overlay) error {
	wdir := p.GetWorkDirInternal()

	cmd := fmt.Sprintf("cd %s; abi-wizard %s/YPKG/root/%s/install", wdir, BuildUserHome, p.Name)
	if err := ChrootExec(notif, overlay.MountPoint, cmd); err != nil {
		slog.Warn("Failed to generate abi report", "reason", err)
		return nil
	}

	notif.SetActivePID(0)

	return nil
}

// CollectAssets will search for the build files and copy them back to the
// users current directory. If solbuild was invoked via sudo, solbuild will
// then attempt to set the owner as the original user.
func (p *Package) CollectAssets(overlay *Overlay, usr *UserInfo, manifestTarget string) error {
	collectionDir := p.GetWorkDir(overlay)

	collections, _ := filepath.Glob(filepath.Join(collectionDir, "*.eopkg"))
	if len(collections) < 1 {
		slog.Error("Mysterious lack of eopkg files is mysterious")
		return errors.New("internal error: .eopkg files are missing")
	}

	// Prior to blitting the files out, let's grab the manifest if requested
	if manifestTarget != "" {
		tram := NewTransitManifest(manifestTarget)
		for _, p := range collections {
			if err := tram.AddFile(p); err != nil {
				return fmt.Errorf("Failed to collect eopkg asset for transit manifest %s, reason: %w\n", p, err)
			}
		}

		// $source-$version-$release.tram
		// We omit arch for *now*, Solus isn't multiple architecture yet.
		tramFile := fmt.Sprintf("%s-%s-%d%s", p.Name, p.Version, p.Release, TransitManifestSuffix)
		tramPath := filepath.Join(collectionDir, tramFile)

		// Try to write manifest
		if err := tram.Write(tramPath); err != nil {
			return err
		}

		// Worked, great. Now ensure our next cycle collects, chowns, etc.
		collections = append(collections, tramPath)
	}

	// Collect files from abireport
	abireportfiles, _ := filepath.Glob(filepath.Join(collectionDir, "abi_*"))
	collections = append(collections, abireportfiles...)

	if p.Type == PackageTypeYpkg {
		pspecs, _ := filepath.Glob(filepath.Join(collectionDir, "pspec_*.xml"))
		collections = append(collections, pspecs...)
	}

	slog.Debug("Collecting files", "len", len(collections))

	for _, p := range collections {
		tgt, err := filepath.Abs(filepath.Join(".", filepath.Base(p)))
		if err != nil {
			return fmt.Errorf("Unable to find working directory, reason: %w\n", err)
		}

		slog.Debug("Collecting build artifact", "path", filepath.Base(p))

		if err = disk.CopyFile(p, tgt); err != nil {
			return fmt.Errorf("Unable to collect build file, reason: %w\n", err)
		}

		slog.Debug("Setting file ownership for current user", "uid", usr.UID, "gid", usr.GID, "path", filepath.Base(p))

		if err = os.Chown(tgt, usr.UID, usr.GID); err != nil {
			slog.Error("Error in restoring file ownership", "path", filepath.Base(p), "reason", err)
		}
	}

	return nil
}

// Build will attempt to build the package in the overlayfs system.
func (p *Package) Build(notif PidNotifier, history *PackageHistory, profile *Profile, pman *EopkgManager, overlay *Overlay, manifestTarget string) error {
	slog.Debug("Building package", "name", p.Name, "version", p.Version, "release", p.Release, "type", p.Type,
		"profile", overlay.Back.Name)

	usr := GetUserInfo()

	var env []string
	if p.Type == PackageTypeXML {
		env = SaneEnvironment("root", "/root")
	} else {
		env = SaneEnvironment(BuildUser, BuildUserHome)
	}

	ChrootEnvironment = env

	// Set up environment
	if err := overlay.CleanExisting(); err != nil {
		return err
	}

	// Bring up the root
	if err := p.ActivateRoot(overlay); err != nil {
		return err
	}

	// Ensure source assets are in place
	if err := p.CopyAssets(history, overlay); err != nil {
		return fmt.Errorf("Failed to copy required source assets, reason: %w\n", err)
	}

	slog.Debug("Validating sources")

	if err := p.FetchSources(overlay); err != nil {
		return err
	}

	// Set up package manager
	if err := pman.Init(); err != nil {
		return err
	}

	// Bring up dbus to do Things
	slog.Debug("Starting D-BUS")

	if err := pman.StartDBUS(); err != nil {
		return fmt.Errorf("Failed to start d-bus, reason: %w\n", err)
	}

	// Get the repos in place before asserting anything
	if err := p.ConfigureRepos(notif, overlay, pman, profile); err != nil {
		return fmt.Errorf("Configuring repositories failed, reason: %w\n", err)
	}

	slog.Debug("Upgrading system base")

	if err := pman.Upgrade(); err != nil {
		return fmt.Errorf("Failed to upgrade rootfs, reason: %w\n", err)
	}

	slog.Debug("Asserting system.devel component installation")

	if err := pman.InstallComponent("system.devel"); err != nil {
		return fmt.Errorf("Failed to assert system.devel, reason: %w\n", err)
	}

	// Ensure all directories are in place
	if err := p.CreateDirs(overlay); err != nil {
		return err
	}

	// Call the relevant build function
	if p.Type == PackageTypeYpkg {
		if err := p.BuildYpkg(notif, usr, pman, overlay, history); err != nil {
			return err
		}
	} else {
		if err := p.BuildXML(notif, pman, overlay); err != nil {
			return err
		}
	}

	return p.CollectAssets(overlay, usr, manifestTarget)
}
