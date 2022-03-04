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
	log "github.com/DataDrake/waterlog"
	"github.com/joebonrichie/abi-wizard/abi"
	"os"
)

// GenerateABIReport will take care of generating the abireport using abi-wizard
func (p *Package) GenerateABIReport(notif PidNotifier, overlay *Overlay) error {
	// Chroot into the overlay
	chrootexit, err := Chroot(overlay.MountPoint)
	if err != nil {
		return fmt.Errorf("Failed to chroot into %s to generate an ABI report, reason: %s\n", overlay.MountPoint, err)
	}

	// The folder to run abi-wizard against
	installroot := fmt.Sprintf("%s/YPKG/root/%s/install", BuildUserHome, p.Name)
	// Where to save our abi_* files
	wdir := p.GetWorkDirInternal()

	// Generate the ABI report with abi-wizard
	// purposely don't err here to exit the chroot gracefully
	r := make(abi.Report)
	if err := r.Add(installroot, installroot); err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
	}
	missing, err := r.Resolve()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to resolve symbols, reason: %s\n", err)
	}
	for _, lib := range missing {
		fmt.Fprintf(os.Stderr, "Missing library: %s\n", lib)
	}
	if err = r.Save(wdir); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to save ABI reports, reason: %s\n", err)
	}

	// Return to our original root
	if err := chrootexit(); err != nil {
		log.Fatalf("Failed to exit the chroot after generating an ABI report, reason: %s\n", err)
	}
	log.Goodln("Successfully generated ABI report")
	notif.SetActivePID(0) // needed now?
	return nil
}
