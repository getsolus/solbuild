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
	"log/slog"
	"os"
	"path/filepath"

	"github.com/getsolus/libosdev/disk"
)

func (b *BackingImage) updatePackages(_ PidNotifier, pkgManager *EopkgManager) error {
	slog.Debug("Initialising package manager")

	if err := pkgManager.Init(); err != nil {
		return fmt.Errorf("Failed to initialise package manager, reason: %w\n", err)
	}

	// Bring up dbus to do Things
	slog.Debug("Starting D-BUS")

	if err := pkgManager.StartDBUS(); err != nil {
		return fmt.Errorf("Failed to start d-bus, reason: %w\n", err)
	}

	slog.Debug("Upgrading builder image")

	if err := pkgManager.Upgrade(); err != nil {
		return fmt.Errorf("Failed to perform upgrade, reason: %w\n", err)
	}

	slog.Debug("Asserting system.devel component")

	if err := pkgManager.InstallComponent("system.devel"); err != nil {
		return fmt.Errorf("Failed to install system.devel, reason: %w\n", err)
	}

	// Cleanup now
	slog.Debug("Stopping D-BUS")

	if err := pkgManager.StopDBUS(); err != nil {
		return fmt.Errorf("Failed to stop d-bus, reason: %w\n", err)
	}

	return nil
}

// Update will attempt to update the backing image to the latest version
// internally.
func (b *BackingImage) Update(notif PidNotifier, pkgManager *EopkgManager) error {
	mountMan := disk.GetMountManager()

	slog.Debug("Updating backing image", "name", b.Name)

	if !PathExists(b.RootDir) {
		if err := os.MkdirAll(b.RootDir, 0o0755); err != nil {
			return fmt.Errorf("Failed to create required directories, reason: %w\n", err)
		}

		slog.Debug("Created root directory", "name", b.Name)
	}

	slog.Debug("Mounting rootfs", "image_path", b.ImagePath, "root_dir", b.RootDir)

	// Mount the rootfs
	if err := mountMan.Mount(b.ImagePath, b.RootDir, "auto", "loop"); err != nil {
		return fmt.Errorf("Failed to mount rootfs %s, reason: %w\n", b.ImagePath, err)
	}

	if err := EnsureEopkgLayout(b.RootDir); err != nil {
		return fmt.Errorf("Failed to fix filesystem layout %s, reason: %w\n", b.ImagePath, err)
	}

	procPoint := filepath.Join(b.RootDir, "proc")

	// Bring up proc
	slog.Debug("Mounting vfs /proc")

	if err := mountMan.Mount("proc", procPoint, "proc", "nosuid", "noexec"); err != nil {
		return fmt.Errorf("Failed to mount /proc, reason: %w\n", err)
	}

	// Hand over to package management to do the updates
	if err := b.updatePackages(notif, pkgManager); err != nil {
		return err
	}

	// Lastly, add the user
	if err := AddBuildUser(b.RootDir); err != nil {
		return err
	}

	slog.Debug("Image successfully updated", "name", b.Name)

	return nil
}
