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

package source

import (
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"io/ioutil"
	"net/url"
	"os"
	"path/filepath"
	"time"

	log "github.com/DataDrake/waterlog"
	"github.com/cavaliergopher/grab/v3"
	"github.com/cheggaaa/pb/v3"
)

const progressBarTemplate string = `{{with string . "prefix"}}{{.}} {{end}}{{printf "%25s" (counters .) }} {{bar . }} {{printf "%7s" (percent .) }} {{printf "%14s" (speed . "%s/s" "??/s")}}{{with string . "suffix"}} {{.}}{{end}}`

// A SimpleSource is a tarball or other source for a package
type SimpleSource struct {
	URI  string
	File string // Basename of the file

	legacy    bool   // If this is ypkg or not
	validator string // Validation key for this source

	url *url.URL
}

// NewSimple will create a new source instance
func NewSimple(uri, validator string, legacy bool) (*SimpleSource, error) {
	// Ensure the URI is actually valid.
	uriObj, err := url.Parse(uri)
	if err != nil {
		return nil, err
	}
	fileName := filepath.Base(uriObj.Path)
	// support URI fragments for renaming sources
	if uriObj.Fragment != "" {
		fileName = uriObj.Fragment
		uriObj.Fragment = ""
	}

	ret := &SimpleSource{
		URI:       uriObj.String(),
		File:      fileName,
		legacy:    legacy,
		validator: validator,
		url:       uriObj,
	}
	return ret, nil
}

// GetIdentifier will return the URI associated with this source.
func (s *SimpleSource) GetIdentifier() string {
	return s.URI
}

// GetBindConfiguration will return the pair for binding our tarballs.
func (s *SimpleSource) GetBindConfiguration(rootfs string) BindConfiguration {
	return BindConfiguration{
		BindSource: s.GetPath(s.validator),
		BindTarget: filepath.Join(rootfs, s.File),
	}
}

// GetPath gets the path on the filesystem of the source
func (s *SimpleSource) GetPath(hash string) string {
	return filepath.Join(SourceDir, hash, s.File)
}

// GetSHA1Sum will return the sha1sum for the given path
func (s *SimpleSource) GetSHA1Sum(path string) (string, error) {
	inp, err := ioutil.ReadFile(path)
	if err != nil {
		return "", err
	}
	hash := sha1.New()
	hash.Write(inp)
	sum := hash.Sum(nil)
	return hex.EncodeToString(sum), nil
}

// GetSHA256Sum will return the sha1sum for the given path
func (s *SimpleSource) GetSHA256Sum(path string) (string, error) {
	inp, err := ioutil.ReadFile(path)
	if err != nil {
		return "", err
	}
	hash := sha256.New()
	hash.Write(inp)
	sum := hash.Sum(nil)
	return hex.EncodeToString(sum), nil
}

// IsFetched will determine if the source is already present
func (s *SimpleSource) IsFetched() bool {
	return PathExists(s.GetPath(s.validator))
}

// download downloads simple files using go grab
func (s *SimpleSource) download(destination string) error {
	req, err := grab.NewRequest(destination, s.URI)
	if err != nil {
		return err
	}

	// Ensure the checksum matches
	if !s.legacy {
		sum, err := hex.DecodeString(s.validator)
		if err != nil {
			return err
		}
		req.SetChecksum(sha256.New(), sum, false)
	}

	resp := grab.NewClient().Do(req)

	// Setup our progress bar
	pbar := pb.Start64(resp.Size())
	pbar.Set(pb.Bytes, true)
	pbar.SetTemplateString(progressBarTemplate)
	defer pbar.Finish()

	// Timer to integrate into pbar (30fps)
	t := time.NewTicker(32 * time.Millisecond)
	defer t.Stop()

	for {
		select {
		case <-t.C:
			pbar.SetCurrent(resp.BytesComplete())
		case <-resp.Done:
			// Ensure progressbar completes to 100%
			pbar.SetCurrent(resp.BytesComplete())
			if err := resp.Err(); err != nil {
				log.Errorf("Error downloading %s: %v\n", s.URI, err)
				return err
			}
			return nil
		}
	}
}

// Fetch will download the given source and cache it locally
func (s *SimpleSource) Fetch() error {
	// Now go and download it
	log.Debugf("Downloading source %s\n", s.URI)

	destPath := filepath.Join(SourceStagingDir, s.File)

	// Check staging is available
	if !PathExists(SourceStagingDir) {
		if err := os.MkdirAll(SourceStagingDir, 0o0755); err != nil {
			return err
		}
	}

	// Grab the file
	if err := s.download(destPath); err != nil {
		return err
	}

	hash, err := s.GetSHA256Sum(destPath)
	if err != nil {
		return err
	}

	// Make the target directory
	tgtDir := filepath.Join(SourceDir, hash)
	if !PathExists(tgtDir) {
		if err := os.MkdirAll(tgtDir, 0o0755); err != nil {
			return err
		}
	}
	// Move from staging into hash based directory
	dest := filepath.Join(tgtDir, s.File)
	if err := os.Rename(destPath, dest); err != nil {
		return err
	}
	// If the file has a sha1sum set, symlink it to the sha256sum because
	// it's a legacy archive (pspec.xml)
	if s.legacy {
		sha, err := s.GetSHA1Sum(dest)
		if err != nil {
			return err
		}
		tgtLink := filepath.Join(SourceDir, sha)
		if err := os.Symlink(hash, tgtLink); err != nil {
			return err
		}
	}
	return nil
}
