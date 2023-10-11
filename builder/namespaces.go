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
	"syscall"
)

// ConfigureNamespace will unshare() context, entering a new namespace.
func ConfigureNamespace() error {
	slog.Debug("Configuring container namespace")

	if err := syscall.Unshare(syscall.CLONE_NEWNS | syscall.CLONE_NEWIPC); err != nil {
		return fmt.Errorf("Failed to configure namespace, reason: %w\n", err)
	}

	return nil
}

// DropNetworking will unshare() the context networking capabilities.
func DropNetworking() error {
	slog.Debug("Dropping container networking")

	if err := syscall.Unshare(syscall.CLONE_NEWNET | syscall.CLONE_NEWUTS); err != nil {
		return fmt.Errorf("Failed to drop networking capabilities, reason: %w\n", err)
	}

	return nil
}
