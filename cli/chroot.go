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

package cli

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/DataDrake/cli-ng/v2/cmd"

	"github.com/getsolus/solbuild/builder"
	"github.com/getsolus/solbuild/cli/log"
)

func init() {
	cmd.Register(&Chroot)
}

// Chroot opens an interactive shell inside the chroot environment.
var Chroot = cmd.Sub{
	Name:  "chroot",
	Short: "Interactively chroot into the package's build environment",
	Args:  &ChrootArgs{},
	Run:   ChrootRun,
}

// ChrootArgs are arguments for the "chroot" sub-command.
type ChrootArgs struct {
	Path []string `zero:"yes" desc:"Chroot into the environment for a [package.yml|pspec.xml] receipe."`
}

// ChrootRun carries out the "chroot" sub-command.
func ChrootRun(r *cmd.Root, s *cmd.Sub) {
	rFlags := r.Flags.(*GlobalFlags) //nolint:forcetypeassert // guaranteed by callee.
	sArgs := s.Args.(*ChrootArgs)    //nolint:forcetypeassert // guaranteed by callee.

	if rFlags.Debug {
		log.Level.Set(slog.LevelDebug)
	}

	if rFlags.NoColor {
		log.SetUncoloredLogger()

		builder.DisableColors = true
	}

	// Allow chrooting into an environment for a build recipe for a given file
	// (Convert from []string to string to allow usage of cli-ng's zero (optional) property.)
	pkgPath := strings.Join(sArgs.Path, "")
	if len(pkgPath) == 0 {
		// Otherwise look for a suitable file to chroot into from the current directory
		pkgPath = FindLikelyArg()
	}

	if len(pkgPath) == 0 {
		log.Panic("No package.yml or pspec.xml found in current directory and no file provided.")
	}

	if os.Geteuid() != 0 {
		log.Panic("You must be root to use chroot")
	}

	// Initialise the build manager
	manager, err := builder.NewManager()
	if err != nil {
		os.Exit(1)
	}
	// Safety first..
	if err = manager.SetProfile(rFlags.Profile); err != nil {
		os.Exit(1)
	}

	pkg, err := builder.NewPackage(pkgPath)
	if err != nil {
		log.Panic("Failed to load package: %s\n", err)
	}
	// Set the package
	if err := manager.SetPackage(pkg); err != nil {
		if errors.Is(err, builder.ErrProfileNotInstalled) {
			fmt.Fprintf(os.Stderr, "%v: Did you forget to init?\n", err)
		}

		os.Exit(1)
	}

	if err := manager.Chroot(); err != nil {
		log.Panic("Chroot failure")
	}

	slog.Info("Chroot complete")
}
