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
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/getsolus/libosdev/disk"
	"github.com/go-git/go-git/v5"

	"github.com/getsolus/solbuild/cli/log"
)

var (
	// ErrManagerInitialised is returned when the library user attempts to set
	// a core part of the Manager after it's already been initialised.
	ErrManagerInitialised = errors.New("the manager has already been initialised")

	// ErrNoPackage is returned when we've got no package.
	ErrNoPackage = errors.New("you must first set a package to build it")

	// ErrNotImplemented is returned as a placeholder when developing functionality.
	ErrNotImplemented = errors.New("function not yet implemented")

	// ErrProfileNotInstalled is returned when a profile is not yet installed.
	ErrProfileNotInstalled = errors.New("profile is not installed")

	// ErrInvalidProfile is returned when there is an invalid profile.
	ErrInvalidProfile = errors.New("invalid profile")

	// ErrInvalidImage is returned when the backing image is unknown.
	ErrInvalidImage = errors.New("invalid image")

	// ErrInterrupted is returned when the build is interrupted.
	ErrInterrupted = errors.New("the operation was cancelled by the user")
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
		m.pkg.DeactivateRoot(m.overlay)
	}

	// Deactivation may have started something off, kill them too
	if deathPoint != "" {
		MurderDeathKill(deathPoint)
	}

	// Unmount anything we may have mounted
	disk.GetMountManager().UnmountAll()

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

	if err := m.checkPackage(); err != nil {
		return err
	}

	// Now get on with the real work!
	m.SigIntCleanup()
	defer m.Cleanup()

	// Now set our options according to the config
	m.overlay.EnableTmpfs = m.Config.EnableTmpfs
	m.overlay.TmpfsSize = m.Config.TmpfsSize

	if !ValidMemSize(m.overlay.TmpfsSize) && m.overlay.EnableTmpfs {
		log.Panic("Invalid memory size specified", "tmpfs_size", m.overlay.TmpfsSize)
	}

	if err := m.doLock(m.overlay.LockPath, "building"); err != nil {
		return err
	}

	return m.pkg.Build(m, m.history, m.GetProfile(), m.pkgManager, m.overlay, m.manifestTarget)
}

// Chroot will enter the build environment to allow users to introspect it.
func (m *Manager) Chroot() error {
	if m.IsCancelled() {
		return ErrInterrupted
	}

	if err := m.checkPackage(); err != nil {
		return err
	}

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

	if err := m.prepareUpdate(); err != nil {
		return err
	}

	m.SigIntCleanup()
	defer m.Cleanup()

	if err := m.doLock(m.image.LockPath, "updating"); err != nil {
		return err
	}

	return m.image.Update(m, m.pkgManager)
}

func (m *Manager) prepareUpdate() error {
	m.lock.Lock()
	defer m.lock.Unlock()

	if m.image == nil {
		return ErrInvalidProfile
	}

	if !m.image.IsInstalled() {
		return ErrProfileNotInstalled
	}

	m.updateMode = true
	m.pkgManager = NewEopkgManager(m, m.image.RootDir)

	return nil
}

// Index will attempt to index the given directory for eopkgs.
func (m *Manager) Index(dir string) error {
	if m.IsCancelled() {
		return ErrInterrupted
	}

	if err := m.checkPackage(); err != nil {
		return err
	}

	// Now get on with the real work!
	m.SigIntCleanup()
	defer m.Cleanup()

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

func (m *Manager) checkPackage() error {
	m.lock.Lock()
	defer m.lock.Unlock()

	if m.pkg == nil {
		return ErrNoPackage
	}

	return nil
}
