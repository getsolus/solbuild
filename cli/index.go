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
	"fmt"
	"github.com/DataDrake/cli-ng/cmd"
	log "github.com/DataDrake/waterlog"
	"github.com/DataDrake/waterlog/format"
	"github.com/DataDrake/waterlog/level"
	"github.com/getsolus/solbuild/builder"
	"os"
)

func init() {
	cmd.Register(&Index)
}

// Index generates index files for a local repository
var Index = cmd.Sub{
	Name:  "index",
	Short: "Create repo index in the given directory",
	Flags: &IndexFlags{},
	Args:  &IndexArgs{},
	Run:   IndexRun,
}

// IndexFlags are flags for the "index" sub-command
type IndexFlags struct {
	Tmpfs  bool   `short:"t" long:"tmpfs"  desc:"Enable building in a tmpfs"`
	Memory string `short:"m" long:"memory" desc:"Set the tmpfs size to use"`
}

// IndexArgs are args for the "index" sub-command
type IndexArgs struct {
	Dir string `desc:"Output directory the generated index files"`
}

// IndexRun carries out the "index" sub-command
func IndexRun(r *cmd.Root, s *cmd.Sub) {
	rFlags := r.Flags.(*GlobalFlags)
	sFlags := s.Flags.(*IndexFlags)
	if rFlags.Debug {
		log.SetLevel(level.Debug)
	}
	if rFlags.NoColor {
		log.SetFormat(format.Un)
	}
	if os.Geteuid() != 0 {
		log.Fatalln("You must be root to use index")
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
	// Set the package
	if err := manager.SetPackage(&builder.IndexPackage); err != nil {
		if err == builder.ErrProfileNotInstalled {
			fmt.Fprintf(os.Stderr, "%v: Did you forget to init?\n", err)
		}
		os.Exit(1)
	}
	manager.SetTmpfs(sFlags.Tmpfs, sFlags.Memory)
	args := s.Args.(*IndexArgs)
	if err := manager.Index(args.Dir); err != nil {
		log.Fatalln("Index failure")
	}
	log.Infoln("Indexing complete")
}
