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
	"github.com/DataDrake/cli-ng/v2/cmd"
)

const (
	// SolbuildVersion is the current public version of solbuild
	SolbuildVersion = "1.5.2.2"
)

func init() {
	cmd.Register(&Version)
}

// Version prints out the version of this executable
var Version = cmd.Sub{
	Name:  "version",
	Short: "Print the solbuild version and exit",
	Run:   VersionRun,
}

// VersionRun carries out the "version" sub-command
func VersionRun(_ *cmd.Root, _ *cmd.Sub) {
	fmt.Printf("solbuild version %v\n\nCopyright © 2016-2021 Solus Project\n", SolbuildVersion)
	fmt.Println("Licensed under the Apache License, Version 2.0")
}
