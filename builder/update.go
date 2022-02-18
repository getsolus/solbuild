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
	"github.com/getsolus/libosdev/disk"
	log "github.com/DataDrake/waterlog"
	"os"
	"path/filepath"
)

func (b *BackingImage) updatePackages(notif PidNotifier, pkgManager *EopkgManager) error {
	log.Debugln("Initialising package manager")

	if err := pkgManager.Init(); err != nil {
		return fmt.Errorf("Failed to initialise package manager, reason: %s\n", err)
	}

	// Bring up dbus to do Things
	log.Debugln("Starting D-BUS")
	if err := pkgManager.StartDBUS(); err != nil {
		return fmt.Errorf("Failed to start d-bus, reason: %s\n", err)
	}

	log.Debugln("Upgrading builder image")
	if err := pkgManager.Upgrade(); err != nil {
		return fmt.Errorf("Failed to perform upgrade, reason: %s\n", err)
	}

	log.Debugln("Asserting system.devel component")
	if err := pkgManager.InstallComponent("system.devel"); err != nil {
		return fmt.Errorf("Failed to install system.devel, reason: %s\n", err)
	}

	// Cleanup now
	log.Debugln("Stopping D-BUS")
	if err := pkgManager.StopDBUS(); err != nil {
		return fmt.Errorf("Failed to stop d-bus, reason: %s\n", err)
	}

	return nil
}

// Update will attempt to update the backing image to the latest version
// internally.
func (b *BackingImage) Update(notif PidNotifier, pkgManager *EopkgManager) error {
	mountMan := disk.GetMountManager()
	log.Debugf("Updating backing image %s\n", b.Name)

	if !PathExists(b.RootDir) {
		if err := os.MkdirAll(b.RootDir, 00755); err != nil {
			return fmt.Errorf("Failed to create required directories, reason: %s\n", err)
		}
		log.Debugf("Created root directory %s\n", b.Name)
	}

	log.Debugf("Mounting rootfs %s %s\n", b.ImagePath, b.RootDir)

	// Mount the rootfs
	if err := mountMan.Mount(b.ImagePath, b.RootDir, "auto", "loop"); err != nil {
		return fmt.Errorf("Failed to mount rootfs %s, reason: %s\n", b.ImagePath, err)
	}

	if err := EnsureEopkgLayout(b.RootDir); err != nil {
		return fmt.Errorf("Failed to fix filesystem layout %s, reason: %s\n", b.ImagePath, err)
	}

	procPoint := filepath.Join(b.RootDir, "proc")

	// Bring up proc
	log.Debugln("Mounting vfs /proc")
	if err := mountMan.Mount("proc", procPoint, "proc", "nosuid", "noexec"); err != nil {
		return fmt.Errorf("Failed to mount /proc, reason: %s\n", err)
	}

	// Hand over to package management to do the updates
	if err := b.updatePackages(notif, pkgManager); err != nil {
		return err
	}

	// Lastly, add the user
	if err := AddBuildUser(b.RootDir); err != nil {
		return err
	}

	log.Debugf("Image successfully updated %s\n", b.Name)

	return nil
}
