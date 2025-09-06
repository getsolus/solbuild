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
	if err != nil || st == nil {
		return nil //nolint:nilerr // File doesn't exist, move on
	}

	if st.Mode().IsDir() {
		var files []os.DirEntry

		if files, err = os.ReadDir(source); err != nil {
			return err
		}

		for _, f := range files {
			spath := filepath.Join(source, f.Name())
			dpath := filepath.Join(destdir, filepath.Base(source))

			if err = CopyAll(spath, dpath); err != nil {
				return err
			}
		}
	} else {
		if !PathExists(destdir) {
			slog.Debug("Creating target directory", "dir", destdir)

			if err = os.MkdirAll(destdir, 0o0755); err != nil {
				return fmt.Errorf("failed to create target directory: %s, reason: %w", destdir, err)
			}
		}

		tgt := filepath.Join(destdir, filepath.Base(source))
		slog.Debug("Copying source", "source", source, "target", tgt)

		if err = disk.CopyFile(source, tgt); err != nil {
			return fmt.Errorf("failed to copy source asset to target: source=%q, target=%q, reason: %w", source, tgt, err)
		}
	}

	return nil
}
