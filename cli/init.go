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
	"net/http"
	"os"

	"github.com/DataDrake/cli-ng/v2/cmd"
	log "github.com/DataDrake/waterlog"
	"github.com/DataDrake/waterlog/format"
	"github.com/DataDrake/waterlog/level"
	"github.com/cheggaaa/pb/v3"
	"github.com/getsolus/libosdev/commands"

	"github.com/getsolus/solbuild/builder"
)

func init() {
	cmd.Register(&Init)
	cmd.Register(&cmd.Help)
}

// Init downloads a solbuid image and initializes the profile
var Init = cmd.Sub{
	Name:  "init",
	Short: "Initialise a solbuild profile",
	Flags: &InitFlags{},
	Run:   InitRun,
}

// InitFlags are flags for the "init" sub-command
type InitFlags struct {
	AutoUpdate bool `short:"u" long:"update" desc:"Automatically update the new image"`
}

// InitRun carries out the "init" sub-command
func InitRun(r *cmd.Root, s *cmd.Sub) {
	rFlags := r.Flags.(*GlobalFlags) //nolint:forcetypeassert // guaranteed by callee.
	sFlags := s.Flags.(*InitFlags)   //nolint:forcetypeassert // guaranteed by callee.

	if rFlags.Debug {
		log.SetLevel(level.Debug)
	}
	if rFlags.NoColor {
		log.SetFormat(format.Un)
	}
	if os.Geteuid() != 0 {
		log.Fatalln("You must be root to run init profiles")
	}
	// Now we'll update the newly initialised image
	manager, err := builder.NewManager()
	if err != nil {
		log.Fatalln(err.Error())
	}
	// Safety first..
	if err = manager.SetProfile(rFlags.Profile); err != nil {
		log.Fatalln(err.Error())
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
		log.Warnf("'%s' has already been initialised\n", prof.Name)
		return
	}
	imgDir := builder.ImagesDir
	// Ensure directories exist
	if !builder.PathExists(imgDir) {
		if err := os.MkdirAll(imgDir, 0o0755); err != nil {
			log.Fatalf("Failed to create images directory '%s', reason: %s", imgDir, err)
		}
		log.Debugf("Created images directory '%s'\n", imgDir)
	}
	// Now ensure we actually have said image
	if !bk.IsFetched() {
		if err := downloadImage(bk); err != nil {
			log.Fatalln(err.Error())
		}
	}
	// Decompress the image
	log.Debugf("Decompressing backing image, source: '%s' target: '%s'\n", bk.ImagePathXZ, bk.ImagePath)
	if err := commands.ExecStdoutArgsDir(builder.ImagesDir, "unxz", []string{bk.ImagePathXZ}); err != nil {
		log.Fatalf("Failed to decompress image '%s', reason: %s\n", bk.ImagePathXZ, err)
	}
	log.Infoln("Profile successfully initialised")
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

// doUpdate will perform an update to the image after the initial init stage
func doUpdate(manager *builder.Manager) {
	if err := manager.Update(); err != nil {
		log.Fatalf("Update failed, reason: '%s'\n", err)
	}
}
