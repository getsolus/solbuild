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
	"io"
	"log/slog"
	"net/http"
	"os"

	"github.com/DataDrake/cli-ng/v2/cmd"
	"github.com/cheggaaa/pb/v3"
	"github.com/getsolus/libosdev/commands"

	"github.com/getsolus/solbuild/builder"
	"github.com/getsolus/solbuild/cli/log"
)

func init() {
	commands.SetStderr(os.Stdout)
	cmd.Register(&Init)
	cmd.Register(&cmd.Help)
}

// Init downloads a solbuid image and initializes the profile.
var Init = cmd.Sub{
	Name:  "init",
	Short: "Initialise a solbuild profile",
	Flags: &InitFlags{},
	Run:   InitRun,
}

// InitFlags are flags for the "init" sub-command.
type InitFlags struct {
	AutoUpdate bool `short:"u" long:"update" desc:"Automatically update the new image"`
}

// InitRun carries out the "init" sub-command.
func InitRun(r *cmd.Root, s *cmd.Sub) {
	rFlags := r.Flags.(*GlobalFlags) //nolint:forcetypeassert // guaranteed by callee.
	sFlags := s.Flags.(*InitFlags)   //nolint:forcetypeassert // guaranteed by callee.

	if rFlags.Debug {
		log.Level.Set(slog.LevelDebug)
	}

	if rFlags.NoColor {
		log.SetUncoloredLogger()
	}

	if os.Geteuid() != 0 {
		slog.Error("You must be root to run init profiles")
		os.Exit(1)
	}
	// Now we'll update the newly initialised image
	manager, err := builder.NewManager()
	if err != nil {
		slog.Error(err.Error())
		panic(err)
	}
	// Safety first...
	if err = manager.SetProfile(rFlags.Profile); err != nil {
		slog.Error(err.Error())
		panic(err)
	}

	doInit(manager)

	if sFlags.AutoUpdate {
		doUpdate(manager)
	}
}

func doInit(manager *builder.Manager) {
	prof := manager.GetProfile()
	bk := builder.NewBackingImage(prof.Image)

	if bk.IsInstalled() {
		slog.Warn("Image has already been initialised", "name", prof.Name)
		return
	}

	imgDir := builder.ImagesDir
	// Ensure directories exist
	if !builder.PathExists(imgDir) {
		if err := os.MkdirAll(imgDir, 0o0755); err != nil {
			slog.Error("Failed to create images directory", "path", imgDir, "err", err)
			panic(err)
		}

		slog.Debug("Created images directory", "path", imgDir)
	}
	// Now ensure we actually have said image
	if !bk.IsFetched() {
		if err := downloadImage(bk); err != nil {
			slog.Error("Failed to download image", "err", err)
			panic(err)
		}
	}
	// Decompress the image
	slog.Debug("Decompressing backing image", "source", bk.ImagePathXZ, "target", bk.ImagePath)

	if err := commands.ExecStdoutArgsDir(builder.ImagesDir, "unxz", []string{bk.ImagePathXZ}); err != nil {
		slog.Error("Failed to decompress image", "source", bk.ImagePathXZ, "err", err)
		panic(err)
	}

	slog.Info("Profile successfully initialised")
}

// Downloads an image using net/http.
func downloadImage(bk *builder.BackingImage) (err error) {
	file, err := os.Create(bk.ImagePathXZ)
	if err != nil {
		return fmt.Errorf("failed to create file '%s', reason: '%w'", bk.ImagePathXZ, err)
	}

	defer func() {
		if err != nil {
			os.Remove(bk.ImagePathXZ)
		}
	}()
	defer file.Close()

	resp, err := http.Get(bk.ImageURI)
	if err != nil {
		return fmt.Errorf("failed to fetch image '%s', reason: '%w'", bk.ImageURI, err)
	}

	defer resp.Body.Close()
	bar := pb.New64(resp.ContentLength).Set(pb.Bytes, true)
	reader := bar.NewProxyReader(resp.Body)
	bar.Start()

	defer bar.Finish()

	bytesRemaining := resp.ContentLength
	done := false
	buf := make([]byte, 32*1024)

	for !done {
		bytesRead, err := reader.Read(buf)
		if errors.Is(err, io.EOF) {
			done = true
		} else if err != nil {
			return fmt.Errorf("failed to fetch image '%s', reason: '%w'", bk.ImageURI, err)
		}

		if _, err = file.Write(buf[:bytesRead]); err != nil {
			return fmt.Errorf("failed to write image '%s', reason: '%w'", bk.ImagePathXZ, err)
		}

		bytesRemaining -= int64(bytesRead)
	}

	return nil
}

// doUpdate will perform an update to the image after the initial init stage.
func doUpdate(manager *builder.Manager) {
	if err := manager.Update(); err != nil {
		slog.Error("Update failed", "reason", err)
	}
}
