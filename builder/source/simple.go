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
	"fmt"
	log "github.com/DataDrake/waterlog"
	curl "github.com/andelf/go-curl"
	"github.com/cheggaaa/pb/v3"
	"io/ioutil"
	"net/url"
	"os"
	"path/filepath"
)

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
	//support URI fragments for renaming sources
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

// download utilises CURL to do all downloads
func (s *SimpleSource) download(destination string) error {
	hnd := curl.EasyInit()
	defer hnd.Cleanup()

	hnd.Setopt(curl.OPT_URL, s.URI)
	hnd.Setopt(curl.OPT_FOLLOWLOCATION, 1)

	out, err := os.Create(destination)
	if err != nil {
		return err
	}

	pbar := pb.New64(0)
	pbar.Set(pb.Bytes, true)
	pbar.Set("prefix", filepath.Base(destination))
	pbar.SetMaxWidth(80)

	writer := func(data []byte, udata interface{}) bool {
		if _, err := out.Write(data); err != nil {
			return false
		}
		return true
	}
	progress := func(total, now, utotal, unow float64, udata interface{}) bool {
		pbar.SetTotal(int64(total))
		pbar.SetCurrent(int64(now))

		return true
	}

	hnd.Setopt(curl.OPT_WRITEFUNCTION, writer)
	hnd.Setopt(curl.OPT_NOPROGRESS, false)
	hnd.Setopt(curl.OPT_PROGRESSFUNCTION, progress)
	// Enforce internal 300 second connect timeout in libcurl
	hnd.Setopt(curl.OPT_CONNECTTIMEOUT, 0)
	hnd.Setopt(curl.OPT_USERAGENT, fmt.Sprintf("solbuild 1.5.1.0"))

	pbar.Start()
	defer func() {
		pbar.Finish()
	}()

	return hnd.Perform()
}

// Fetch will download the given source and cache it locally
func (s *SimpleSource) Fetch() error {
	// Now go and download it
	log.Debugf("Downloading source %s\n", s.URI)

	destPath := filepath.Join(SourceStagingDir, s.File)

	// Check staging is available
	if !PathExists(SourceStagingDir) {
		if err := os.MkdirAll(SourceStagingDir, 00755); err != nil {
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
		if err := os.MkdirAll(tgtDir, 00755); err != nil {
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
