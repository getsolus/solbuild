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

	"github.com/DataDrake/cli-ng/v2/cmd"

	"github.com/getsolus/solbuild/builder"
	"github.com/getsolus/solbuild/cli/log"
)

func init() {
	cmd.Register(&Index)
}

// Index generates index files for a local repository.
var Index = cmd.Sub{
	Name:  "index",
	Short: "Create repo index in the given directory",
	Flags: &IndexFlags{},
	Args:  &IndexArgs{},
	Run:   IndexRun,
}

// IndexFlags are flags for the "index" sub-command.
type IndexFlags struct {
	Tmpfs  bool   `short:"t" long:"tmpfs"  desc:"Enable building in a tmpfs"`
	Memory string `short:"m" long:"memory" desc:"Set the tmpfs size to use"`
}

// IndexArgs are args for the "index" sub-command.
type IndexArgs struct {
	Dir string `desc:"Output directory the generated index files"`
}

// IndexRun carries out the "index" sub-command.
func IndexRun(r *cmd.Root, s *cmd.Sub) {
	rFlags := r.Flags.(*GlobalFlags) //nolint:forcetypeassert // guaranteed by callee.
	sFlags := s.Flags.(*IndexFlags)  //nolint:forcetypeassert // guaranteed by callee.
	args := s.Args.(*IndexArgs)      //nolint:forcetypeassert // guaranteed by callee.

	if rFlags.Debug {
		log.Level.Set(slog.LevelDebug)
	}

	if rFlags.NoColor {
		log.SetUncoloredLogger()
	}

	if os.Geteuid() != 0 {
		log.Panic("You must be root to use index")
	}
	// Initialise the build manager
	manager, err := builder.NewManager()
	if err != nil {
		os.Exit(1)
	}

	manager.SetCommands(rFlags.Eopkg, rFlags.YPKG)

	// Safety first...
	if err = manager.SetProfile(rFlags.Profile); err != nil {
		os.Exit(1)
	}
	// Set the package
	if err := manager.SetPackage(&builder.IndexPackage); err != nil {
		if errors.Is(err, builder.ErrProfileNotInstalled) {
			fmt.Fprintf(os.Stderr, "%v: Did you forget to init?\n", err)
		}

		os.Exit(1)
	}

	manager.SetTmpfs(sFlags.Tmpfs, sFlags.Memory)

	if err := manager.Index(args.Dir); err != nil {
		log.Panic("Index failure")
	}

	slog.Info("Indexing complete")
}
