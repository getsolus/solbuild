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
	"os"
	"path/filepath"

	log "github.com/DataDrake/waterlog"
	"github.com/getsolus/libosdev/disk"
)

var (
	// ErrCannotContinue is a stock error return.
	ErrCannotContinue = errors.New("Index cannot continue")

	// IndexBindTarget is where we always mount the repo.
	IndexBindTarget = "/hostRepo/Index"
)

// Index will attempt to index the given directory.
func (p *Package) Index(notif PidNotifier, dir string, overlay *Overlay) error {
	log.Debugf("Beginning indexer: profile='%s'\n", overlay.Back.Name)

	mman := disk.GetMountManager()

	ChrootEnvironment = SaneEnvironment("root", "/root")

	// Check the source exists first!
	if !PathExists(dir) {
		log.Errorf("Directory does not exist dir='%s'\n", dir)
		return ErrCannotContinue
	}

	// Indexer will always create new dirs..
	if err := overlay.CleanExisting(); err != nil {
		return err
	}

	if err := p.ActivateRoot(overlay); err != nil {
		return err
	}

	// Create the target
	target := filepath.Join(overlay.MountPoint, IndexBindTarget[1:])
	if err := os.MkdirAll(target, 0o0755); err != nil {
		log.Errorf("Cannot create bind target %s, reason: %s\n", target, err)
		return err
	}

	log.Debugf("Bind mounting directory for indexing %s\n", dir)

	if err := mman.BindMount(dir, target); err != nil {
		log.Errorf("Cannot bind mount directory %s, reason: %s\n", target, err)
		return err
	}

	// Ensure it gets cleaned up
	overlay.ExtraMounts = append(overlay.ExtraMounts, target)

	log.Debugln("Now indexing")

	command := fmt.Sprintf("cd %s; %s", IndexBindTarget, eopkgCommand("eopkg index --skip-signing ."))
	if err := ChrootExec(notif, overlay.MountPoint, command); err != nil {
		log.Errorf("Indexing failed: dir='%s', reason: %s\n", dir, err)
		return err
	}

	return nil
}
