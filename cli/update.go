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
	cmd.Register(&Update)
}

// Update updates a solbuild image with the latest available packages.
var Update = cmd.Sub{
	Name:  "update",
	Alias: "up",
	Short: "Update a solbuild profile",
	Run:   UpdateRun,
}

// UpdateRun carries out the "update" sub-command.
func UpdateRun(r *cmd.Root, c *cmd.Sub) {
	rFlags := r.Flags.(*GlobalFlags) //nolint:forcetypeassert // guaranteed by callee.
	if rFlags.Debug {
		log.Level.Set(slog.LevelDebug)
	}

	if rFlags.NoColor {
		log.SetUncoloredLogger()
	}

	if os.Geteuid() != 0 {
		log.Panic("You must be root to run init profiles")
	}
	// Initialise the build manager
	manager, err := builder.NewManager()
	if err != nil {
		log.Panic(err.Error())
	}

	manager.SetCommands(rFlags.Eopkg, rFlags.YPKG)

	// Safety first...
	if err = manager.SetProfile(rFlags.Profile); err != nil {
		if errors.Is(err, builder.ErrProfileNotInstalled) {
			fmt.Fprintf(os.Stderr, "%v: Did you forget to init?\n", err)
		}

		os.Exit(1)
	}

	if err := manager.Update(); err != nil {
		if errors.Is(err, builder.ErrProfileNotInstalled) {
			fmt.Fprintf(os.Stderr, "%v: Did you forget to init?\n", err)
		}

		os.Exit(1)
	}
}
