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
	cmd.Register(&Build)
}

// Build package(s) in a chroot and output the archives
var Build = cmd.Sub{
	Name:  "build",
	Short: "Build the given package(s) in a chroot environment",
	Flags: &BuildFlags{},
	Run:   BuildRun,
}

// BuildFlags are flags for the "build" sub-command
type BuildFlags struct {
	Tmpfs           bool   `short:"t" long:"tmpfs"  desc:"Enable building in a tmpfs"`
	Memory          string `short:"m" long:"memory" desc:"Set the tmpfs size to use"`
	TransitManifest string `long:"transit-manifest" desc:"Create transit manifest for the given target"`
}

// BuildRun carries out the "build" sub-command
func BuildRun(r *cmd.Root, s *cmd.Sub) {
	rFlags := r.Flags.(*GlobalFlags)
	sFlags := s.Flags.(*BuildFlags)
	if rFlags.Debug {
		log.SetLevel(level.Debug)
	}
	if rFlags.NoColor {
		log.SetFormat(format.Un)
	}
	pkgPath := FindLikelyArg()
	if os.Geteuid() != 0 {
		log.Fatalln("You must be root to run build packages")
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
		log.Fatalf("Failed to load package: %s\n", err)
	}
	manager.SetManifestTarget(sFlags.TransitManifest)
	// Set the package
	if err := manager.SetPackage(pkg); err != nil {
		if err == builder.ErrProfileNotInstalled {
			fmt.Fprintf(os.Stderr, "%v: Did you forget to init?\n", err)
		}
		os.Exit(1)
	}
	manager.SetTmpfs(sFlags.Tmpfs, sFlags.Memory)
	if err := manager.Build(); err != nil {
		log.Fatalln("Failed to build packages")
	}
	log.Infoln("Building succeeded")
}
