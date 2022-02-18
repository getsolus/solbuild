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
	"io/ioutil"
	"os"
	"path/filepath"
)

// CopyAll will copy the source asset into the given destdir.
// If the source is a directory, it will be recursively copied
// into the directory destdir.
//
// Note that all directories are created as 00755, as solbuild
// has no interest in the individual folder permissions, just
// the files themselves.
func CopyAll(source, destdir string) error {
	// We double stat, get over it.
	st, err := os.Stat(source)
	// File doesn't exist, move on
	if err != nil || st == nil {
		return nil
	}

	if st.Mode().IsDir() {
		var files []os.FileInfo
		if files, err = ioutil.ReadDir(source); err != nil {
			return err
		}
		for _, f := range files {
			spath := filepath.Join(source, f.Name())
			dpath := filepath.Join(destdir, filepath.Base(source))
			if err := CopyAll(spath, dpath); err != nil {
				return err
			}
		}
	} else {
		if !PathExists(destdir) {
            log.Debugf("Creating target directory: %s\n", destdir)
			if err = os.MkdirAll(destdir, 00755); err != nil {
				return fmt.Errorf("Failed to create target directory: %s, reason: %s\n", destdir, err)
			}
		}
		tgt := filepath.Join(destdir, filepath.Base(source))
        log.Debugf("Copying source asset %s to %s\n", source, tgt)
		if err = disk.CopyFile(source, tgt); err != nil {
			return fmt.Errorf("Failed to copy source asset to target: source='%s' target='%s', reason: %s\n", source, tgt, err)
		}
	}
	return nil
}
