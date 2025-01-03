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
	login "github.com/coreos/go-systemd/v22/login1"

	"github.com/getsolus/solbuild/builder"
	"github.com/getsolus/solbuild/cli/log"
)

func init() {
	cmd.Register(&Build)
}

// Build package(s) in a chroot and output the archives.
var Build = cmd.Sub{
	Name:  "build",
	Short: "Build the given package(s) in a chroot environment",
	Flags: &BuildFlags{},
	Args:  &BuildArgs{},
	Run:   BuildRun,
}

// BuildFlags are flags for the "build" sub-command.
//
//nolint:tagalign
type BuildFlags struct {
	Tmpfs           bool   `short:"t" long:"tmpfs"              desc:"Enable building in a tmpfs"`
	Memory          string `short:"m" long:"memory"             desc:"Set the tmpfs size to use, e.g. 8G"`
	TransitManifest string `          long:"transit-manifest"   desc:"Create transit manifest for the given target"`
	ABIReport       bool   `short:"r" long:"disable-abi-report" desc:"Don't generate an ABI report of the completed build"`
	History         bool   `short:"h" long:"history"            desc:"Enable history generation for this build"`
}

// BuildArgs are arguments for the "build" sub-command.
type BuildArgs struct {
	Path []string `zero:"yes" desc:"Location of [package.yml|pspec.xml] file to build."`
}

// BuildRun carries out the "build" sub-command.
func BuildRun(r *cmd.Root, s *cmd.Sub) {
	rFlags := r.Flags.(*GlobalFlags) //nolint:forcetypeassert // guaranteed by callee.
	sFlags := s.Flags.(*BuildFlags)  //nolint:forcetypeassert // guaranteed by callee.
	sArgs := s.Args.(*BuildArgs)     //nolint:forcetypeassert // guaranteed by callee.

	if rFlags.Debug {
		log.Level.Set(slog.LevelDebug)
	}

	if rFlags.NoColor {
		log.SetUncoloredLogger()

		builder.DisableColors = true
	}

	if sFlags.ABIReport {
		slog.Debug("Not attempting generation of an ABI report")

		builder.DisableABIReport = true
	}

	// Allow loading a build recipe from an arbitrary location
	// (Convert from []string to string to allow usage of cli-ng's zero (optional) property.)
	pkgPath := strings.Join(sArgs.Path, "")
	if len(pkgPath) == 0 {
		// Otherwise look for a suitable file in the current directory
		pkgPath = FindLikelyArg()
	}

	if len(pkgPath) == 0 {
		log.Panic("No package.yml or pspec.xml file in current directory and no file provided.")
	}

	if os.Geteuid() != 0 {
		log.Panic("You must be root to run build packages")
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

	// Enable history generation
	if sFlags.History {
		manager.Config.EnableHistory = true
	}

	pkg, err := builder.NewPackage(pkgPath)
	if err != nil {
		log.Panic("Failed to load package", "err", err)
	}

	manager.SetManifestTarget(sFlags.TransitManifest)
	// Set the package
	if err = manager.SetPackage(pkg); err != nil {
		if errors.Is(err, builder.ErrProfileNotInstalled) {
			fmt.Fprintf(os.Stderr, "%v: Did you forget to init?\n", err)
		}

		os.Exit(1)
	}

	// Handle tmpfs and memory size options
	if sFlags.Tmpfs {
		switch {
		case sFlags.Memory != "":
			manager.SetTmpfs(sFlags.Tmpfs, sFlags.Memory)
		case sFlags.Memory == "" && manager.Config.TmpfsSize != "":
			manager.SetTmpfs(sFlags.Tmpfs, manager.Config.TmpfsSize)
		default:
			log.Panic("tmpfs: No memory size specified")
		}
	}

	if sFlags.Memory != "" && !sFlags.Tmpfs {
		if !manager.Config.EnableTmpfs {
			slog.Error("tmpfs: Memory size specified but tmpfs was not enabled, pass -t to enable tmpfs")
		} else {
			manager.SetTmpfs(manager.Config.EnableTmpfs, sFlags.Memory)
		}
	}

	// Set a inhibitor lock to prevent system from accidentally going down
	conn, err := login.New()
	if err != nil {
		slog.Error("org.freedesktop.login1: Failed to initialize dbus connection")
	}

	if !conn.Connected() {
		slog.Error("org.freedesktop.login1: Not connected to dbus system bus")
	}

	inhibitMsg := fmt.Sprintf("Build in Progress: %s-%s-%d. Please wait for the build to complete",
		pkg.Name, pkg.Version, pkg.Release)

	fd, err := conn.Inhibit("shutdown:idle:sleep", "solbuild", inhibitMsg, "block")
	if err != nil {
		slog.Error("org.freedesktop.login1: Failed to send inhibitor lock")
	}
	// defer release the inhibitor lock
	defer fd.Close()

	if err := manager.Build(); err != nil {
		log.Panic("Failed to build packages", "err", err)
	}

	slog.Info("Building succeeded")
}
