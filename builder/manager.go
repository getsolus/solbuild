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
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/getsolus/libeopkg/index"
	"github.com/getsolus/libosdev/disk"
	"github.com/go-git/go-git/v5"
	"github.com/ulikunitz/xz"

	"github.com/getsolus/solbuild/cli/log"
)

var (
	// ErrManagerInitialised is returned when the library user attempts to set
	// a core part of the Manager after it's already been initialised.
	ErrManagerInitialised = errors.New("The manager has already been initialised")

	// ErrNoPackage is returned when we've got no package.
	ErrNoPackage = errors.New("You must first set a package to build it")

	// ErrNotImplemented is returned as a placeholder when developing functionality.
	ErrNotImplemented = errors.New("Function not yet implemented")

	// ErrProfileNotInstalled is returned when a profile is not yet installed.
	ErrProfileNotInstalled = errors.New("Profile is not installed")

	// ErrInvalidProfile is returned when there is an invalid profile.
	ErrInvalidProfile = errors.New("Invalid profile")

	// ErrInvalidImage is returned when the backing image is unknown.
	ErrInvalidImage = errors.New("Invalid image")

	// ErrInterrupted is returned when the build is interrupted.
	ErrInterrupted = errors.New("The operation was cancelled by the user")
)

// A Manager is responsible for cleanly managing the entire session within solbuild,
// i.e. setup, teardown, cleaning up, etc.
//
// The consumer should create a new manager instance and only use these methods,
// not bypass and use API methods.
type Manager struct {
	Config *Config // Our config from the merged system/vendor configs

	image      *BackingImage // Storage for the overlay
	overlay    *Overlay      // OverlayFS configuration
	pkg        *Package      // Current package, if any
	pkgManager *EopkgManager // Package manager, if any
	lock       *sync.Mutex   // Lock on all operations to prevent.. damage.
	profile    *Profile      // The profile we've been requested to use
	layer      *Layer
	resolver   *Resolver

	lockfile *LockFile // We track the global lock for each operation
	didStart bool      // Whether we got anything done.

	cancelled  bool // Whether or not we've been cancelled
	updateMode bool // Whether we're just updating an image

	history *PackageHistory // Given package history, if any

	manifestTarget string // Generate manifest if set

	activePID int // Active PID
}

// NewManager will return a newly initialised manager instance.
func NewManager() (*Manager, error) {
	// First things first, setup the namespace
	if err := ConfigureNamespace(); err != nil {
		return nil, err
	}

	man := &Manager{
		cancelled:  false,
		activePID:  0,
		updateMode: false,
		lockfile:   nil,
		didStart:   false,
	}

	// Now load the configuration in
	if config, err := NewConfig(); err == nil {
		man.Config = config
	} else {
		slog.Error("Failed to load solbuild configuration", "err", err)
		return nil, err
	}

	man.lock = new(sync.Mutex)

	return man, nil
}

// SetActivePID will set the active task PID.
func (m *Manager) SetActivePID(pid int) {
	m.lock.Lock()
	defer m.lock.Unlock()
	m.activePID = pid
}

// SetManifestTarget will set the manifest target to be used
// An empty target (default) means no manifest.
func (m *Manager) SetManifestTarget(target string) {
	m.lock.Lock()
	defer m.lock.Unlock()
	m.manifestTarget = strings.TrimSpace(target)
}

// SetCommands overrides the eopkg binary used for all eopkg commands.
func (m *Manager) SetCommands(eopkg string, ypkg string) {
	m.lock.Lock()
	defer m.lock.Unlock()

	if eopkg != "" {
		installCommand = eopkg
		xmlBuildCommand = eopkg
	}

	if ypkg != "" {
		ypkgBuildCommand = ypkg
	}

	slog.Debug("Set binaries",
		"eopkg", installCommand,
		"eopkg_xml", xmlBuildCommand,
		"ypkg", ypkgBuildCommand)
}

// SetProfile will attempt to initialise the manager with a given profile
// Currently this is locked to a backing image specification, but in future
// will be expanded to support profiles *based* on backing images.
func (m *Manager) SetProfile(profile string) error {
	m.lock.Lock()
	defer m.lock.Unlock()

	// Passed an empty profile from the CLI flags, so set our default profile
	// as the one to use.
	if profile == "" {
		slog.Info("Using default profile", "name", m.Config.DefaultProfile)

		profile = m.Config.DefaultProfile
	}

	prof, err := NewProfile(profile)
	if err != nil {
		EmitProfileError(profile)
		return err
	}

	if !IsValidImage(prof.Image) {
		EmitImageError(prof.Image)
		return ErrInvalidImage
	}

	if m.image != nil {
		return ErrManagerInitialised
	}

	m.profile = prof
	m.image = NewBackingImage(m.profile.Image)

	return nil
}

// GetProfile will return the profile associated with this builder.
func (m *Manager) GetProfile() *Profile {
	m.lock.Lock()
	defer m.lock.Unlock()

	return m.profile
}

// SetPackage will set the package associated with this manager.
// This package will be used in build & chroot operations only.
func (m *Manager) SetPackage(pkg *Package) error {
	m.lock.Lock()
	defer m.lock.Unlock()

	if m.pkg != nil {
		return ErrManagerInitialised
	}

	if !m.image.IsInstalled() {
		return ErrProfileNotInstalled
	}

	if m.Config.EnableHistory {
		slog.Info("History generation enabled")

		// Obtain package history for git builds
		if pkg.Type == PackageTypeYpkg {
			repo, err := git.PlainOpenWithOptions(filepath.Dir(pkg.Path),
				&git.PlainOpenOptions{DetectDotGit: true})
			if err != nil && !errors.Is(err, git.ErrRepositoryNotExists) {
				return fmt.Errorf("cannot open Git repository: %w", err)
			}

			if err == nil {
				if history, err := NewPackageHistory(repo, pkg.Path); err == nil {
					slog.Debug("Obtained package history")

					m.history = history
				} else {
					slog.Warn("Failed to obtain package git history", "err", err)
				}
			}
		}
	}

	m.pkg = pkg
	m.overlay = NewOverlay(m.Config, m.profile, m.image, m.pkg)
	m.pkgManager = NewEopkgManager(m, m.overlay.MountPoint)

	return nil
}

// IsCancelled will determine if the build has been cancelled, this will result
// in a lot of locking between all operations.
func (m *Manager) IsCancelled() bool {
	m.lock.Lock()
	defer m.lock.Unlock()

	return m.cancelled
}

// SetCancelled will mark the build manager as cancelled, so it should not attempt
// to start any new operations whatsoever.
func (m *Manager) SetCancelled() {
	m.lock.Lock()
	defer m.lock.Unlock()
	m.cancelled = true
}

// Cleanup will take care of any teardown operations. It takes an exclusive lock
// and ensures all cleaning is handled before anyone else is permitted to continue,
// at which point error propagation and the IsCancelled() function should be enough
// logic to go on.
func (m *Manager) Cleanup() {
	if !m.didStart {
		return
	}

	slog.Debug("Acquiring global lock")
	m.lock.Lock()
	defer m.lock.Unlock()
	slog.Debug("Cleaning up")

	if m.pkgManager != nil {
		// Potentially unnecessary but meh
		m.pkgManager.StopDBUS()
		// Always needed
		m.pkgManager.Cleanup()
	}

	deathPoint := ""
	if m.overlay != nil {
		deathPoint = m.overlay.MountPoint
	}

	if m.updateMode {
		deathPoint = m.image.RootDir
	}

	// Try to kill the active root PID first
	if m.activePID > 0 {
		syscall.Kill(-m.activePID, syscall.SIGKILL)
		time.Sleep(2 * time.Second)
		syscall.Kill(-m.activePID, syscall.SIGKILL)
		m.activePID = 0
	}

	// Still might have *something* alive in there, kill it with fire.
	if deathPoint != "" {
		for range 10 {
			MurderDeathKill(deathPoint)
		}
	}

	if m.pkg != nil {
		m.overlay.DeactivateRoot()
	}

	// Deactivation may have started something off, kill them too
	if deathPoint != "" {
		MurderDeathKill(deathPoint)
	}

	// Unmount anything we may have mounted
	disk.GetMountManager().UnmountAll()

	// Remove the layer if it's unfinished
	if m.layer != nil {
		if err := m.layer.RemoveIfNotCreated(); err != nil {
			slog.Error("Failure in cleaning incomplete layer", "err", err)
		}
	}

	// Finally clean out the lock files
	if m.lockfile != nil {
		if err := m.lockfile.Unlock(); err != nil {
			slog.Error("Failure in unlocking root", "err", err)
		}

		if err := m.lockfile.Clean(); err != nil {
			slog.Error("Failure in cleaning lockfile", "err", err)
		}
	}
}

// doLock will handle the relevant locking operation for the given path.
func (m *Manager) doLock(path, opType string) error {
	// Handle file locking
	lock, err := NewLockFile(path)
	if err != nil {
		slog.Error("Failed to lock root", "op_type", opType, "path", path, "err", err)
		return err
	}

	m.lockfile = lock

	if err = m.lockfile.Lock(); err != nil {
		if errors.Is(err, ErrOwnedLockFile) {
			slog.Error("Failed to lock root - another process is using it", "process", m.lockfile.GetOwnerProcess(), "pid", m.lockfile.GetOwnerPID(), "err", err)
		} else {
			slog.Error("Failed to lock root", "pid", m.lockfile.GetOwnerPID(), "err", err)
		}

		return err
	}

	m.didStart = true

	return nil
}

// SigIntCleanup will take care of cleaning up the build process.
func (m *Manager) SigIntCleanup() {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, os.Interrupt, syscall.SIGTERM)

	go func() {
		<-ch
		slog.Warn("CTRL+C interrupted, cleaning up")
		m.SetCancelled()
		m.Cleanup()
		slog.Error("Exiting due to interruption")
		os.Exit(1)
	}()
}

// Build will attempt to build the package associated with this manager,
// automatically handling any required cleanups.
func (m *Manager) Build() error {
	if m.IsCancelled() {
		return ErrInterrupted
	}

	m.lock.Lock()
	if m.pkg == nil {
		m.lock.Unlock()
		return ErrNoPackage
	}
	m.lock.Unlock()

	// Now get on with the real work!
	defer m.Cleanup()
	m.SigIntCleanup()

	// Now set our options according to the config
	m.overlay.EnableTmpfs = m.Config.EnableTmpfs
	m.overlay.TmpfsSize = m.Config.TmpfsSize

	if !ValidMemSize(m.overlay.TmpfsSize) && m.overlay.EnableTmpfs {
		log.Panic("Invalid memory size specified", "tmpfs_size", m.overlay.TmpfsSize)
	}

	if err := m.doLock(m.overlay.LockPath, "building"); err != nil {
		return err
	}

	if err := m.InitResolver(); err != nil {
		return err
	}
	slog.Debug("Successfully initialized resolver")

	if err := m.prepareLayer(); err != nil {
		return fmt.Errorf("Failed to prepare layer: %w", err)
	}

	return m.pkg.Build(m, m.history, m.GetProfile(), m.pkgManager, m.overlay, m.resolver, m.manifestTarget)
	// TODO: should we put layer here, so we can output the hash later?
}

// Chroot will enter the build environment to allow users to introspect it.
func (m *Manager) Chroot() error {
	if m.IsCancelled() {
		return ErrInterrupted
	}

	m.lock.Lock()
	if m.pkg == nil {
		m.lock.Unlock()
		return ErrNoPackage
	}
	m.lock.Unlock()

	// Now get on with the real work!
	defer m.Cleanup()
	m.SigIntCleanup()

	if err := m.doLock(m.overlay.LockPath, "chroot"); err != nil {
		return err
	}

	return m.pkg.Chroot(m, m.pkgManager, m.overlay)
}

// Update will attempt to update the base image.
func (m *Manager) Update() error {
	if m.IsCancelled() {
		return ErrInterrupted
	}

	m.lock.Lock()
	if m.image == nil {
		m.lock.Unlock()
		return ErrInvalidProfile
	}

	if !m.image.IsInstalled() {
		m.lock.Unlock()
		return ErrProfileNotInstalled
	}

	m.updateMode = true
	m.pkgManager = NewEopkgManager(m, m.image.RootDir)
	m.lock.Unlock()

	defer m.Cleanup()
	m.SigIntCleanup()

	if err := m.doLock(m.image.LockPath, "updating"); err != nil {
		return err
	}

	return m.image.Update(m, m.pkgManager)
}

// Index will attempt to index the given directory for eopkgs.
func (m *Manager) Index(dir string) error {
	if m.IsCancelled() {
		return ErrInterrupted
	}

	m.lock.Lock()
	if m.pkg == nil {
		m.lock.Unlock()
		return ErrNoPackage
	}
	m.lock.Unlock()

	// Now get on with the real work!
	defer m.Cleanup()
	m.SigIntCleanup()

	// Now set our options according to the config
	m.overlay.EnableTmpfs = m.Config.EnableTmpfs
	m.overlay.TmpfsSize = m.Config.TmpfsSize

	if !ValidMemSize(m.overlay.TmpfsSize) && m.overlay.EnableTmpfs {
		log.Panic("Invalid memory size specified", "tmpfs_size", m.overlay.TmpfsSize)
	}

	if err := m.doLock(m.overlay.LockPath, "indexing"); err != nil {
		return err
	}

	return m.pkg.Index(m, dir, m.overlay)
}

// SetTmpfs sets the manager tmpfs option.
func (m *Manager) SetTmpfs(enable bool, size string) {
	if m.IsCancelled() {
		return
	}

	m.lock.Lock()
	defer m.lock.Unlock()

	if m.overlay != nil {
		m.Config.EnableTmpfs = enable
		m.Config.TmpfsSize = strings.TrimSpace(size)
	}
}

func (m *Manager) InitResolver() error {
	m.resolver = NewResolver()

	if m.profile == nil {
		return errors.New("Profile not initialized!")
	}

	profile := m.profile
	/// nameToUrl := make(map[string]string)
	repos := []string{}

	if strings.Contains(profile.Image, "unstable") {
		// nameToUrl["Solus"] = "https://cdn.getsol.us/repo/unstable/eopkg-index.xml.xz"
		repos = append(repos, "https://cdn.getsol.us/repo/unstable/eopkg-index.xml.xz")
		// repos = append(repos, "https://packages.getsol.us/unstable/eopkg-index.xml.xz")
	} else if strings.Contains(profile.Image, "stable") {
		// nameToUrl["Solus"] = "https://cdn.getsol.us/repo/shannon/eopkg-index.xml.xz"
		repos = append(repos, "https://cdn.getsol.us/repo/shannon/eopkg-index.xml.xz")
	} else {
		slog.Warn("Unrecognized image name, not adding default repo", "image", profile.Image)
	}

	// Realistically, remove can only be * or Solus
	// for _, remove := range profile.RemoveRepos {
	// 	if remove == "*" {
	// 		repos = []string{}
	// 		continue
	// 	}

	// 	if idx := slices.Index(repos, remove); idx != -1 {
	// 		repos = slices.Delete(repos, idx, idx+1)
	// 	} else {
	// 		slog.Warn("Cannot remove noexistent repo", "name", remove)
	// 	}
	// }
	if len(profile.RemoveRepos) != 0 {
		repos = []string{}
		if len(profile.RemoveRepos) > 1 {
			slog.Warn("Unexpectedly requested removing of more than 1 repo", "removes", profile.RemoveRepos)
		}
	}

	for _, add := range profile.AddRepos {
		if repo := profile.Repos[add]; repo != nil {
			if repo.Local {
				repos = append(repos, fmt.Sprintf("file://%s/eopkg-index.xml", repo.URI))
			} else {
				repos = append(repos, repo.URI)
			}
		} else {
			slog.Warn("Cannot add nonexistent repo", "name", add)
		}
	}

	for _, repo := range repos {
		slog.Debug("Fetching repo", "url", repo)

		var r io.Reader

		if len(repo) > 7 && repo[0:7] == "file://" {
			// local repo
			if file, err := os.Open(repo[7:]); err != nil {
				return fmt.Errorf("Failed to open index file %s", repo[7:])
			} else {
				r = file
			}
		} else {
			// remote repo
			ext := path.Ext(repo)
			resp, err := http.Get(repo)
			if err != nil {
				// slog.Error("Failed to fetch", "url", repo, "error", err)
				return fmt.Errorf("Failed to fetch %s: %w", repo, err)
			}

			if ext == ".xz" {
				// slog.Debug("Decoding .xz")
				if r, err = xz.NewReader(resp.Body); err != nil {
					// slog.Error("Failed to init xz reader", "error", err)
					return fmt.Errorf("Failed to init xz reader for %s: %w", repo, err)
				}
			} else if ext == ".xml" {
				r = resp.Body
			} else {
				// slog.Error("Unrecognized repo url extension", "url", repo, "ext", ext)
				return fmt.Errorf("Unrecognized repo url extension %s for %s", ext, repo)
			}
		}

		dec := xml.NewDecoder(r)
		var i index.Index
		if err := dec.Decode(&i); err != nil {
			// slog.Error("Failed to decode index", "error", err)
			return fmt.Errorf("Failed to decode index for %s: %w", repo, err)
		}

		m.resolver.AddIndex(&i)
		slog.Info("Parsed and added repo to resolver", "url", repo)
	}

	return nil
}

func (m *Manager) prepareLayer() error {
	p := m.pkg
	deps, err := p.CalcDeps(m.resolver)
	if err != nil {
		return fmt.Errorf("Failed to calculate dependencies: %w", err)
	}
	slog.Debug("Calculated dependencies", "deps", deps)

	m.layer = &Layer{
		deps:    deps,
		profile: m.profile,
		back:    m.overlay.Back,
	}

	contentPath, err := m.layer.RequestOverlay(m)
	if err != nil {
		return err
	}
	m.overlay.LayerDir = contentPath
	return nil
}
