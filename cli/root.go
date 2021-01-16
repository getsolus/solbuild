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
	"github.com/DataDrake/cli-ng/cmd"
	"os"
)

func init() {
	cmd.Register(&cmd.GenManPages)
	cmd.Register(&cmd.Help)
}

// Root is the root command for sobuild
var Root = cmd.Root{
	Name:  "solbuild",
	Short: "solbuild is the Solus package builder",
	Flags: &GlobalFlags{},
}

// GlobalFlags are availabe to all sub-commands
type GlobalFlags struct {
	Debug   bool   `short:"d" long:"debug"    desc:"Enable debug message"`
	NoColor bool   `short:"n" long:"no-color" desc:"Disable color output"`
	Profile string `short:"p" long:"profile"  desc:"Build profile to use"`
}

// FindLikelyArg will look in the current directory to see if common path names exist,
// for when it is acceptable to omit a filename.
func FindLikelyArg() string {
	lookPaths := []string{
		"package.yml",
		"pspec.xml",
	}
	for _, p := range lookPaths {
		if st, err := os.Stat(p); err == nil {
			if st != nil {
				return p
			}
		}
	}
	return ""
}
