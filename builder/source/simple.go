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
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/cavaliergopher/grab/v3"
	"github.com/cheggaaa/pb/v3"

	"github.com/getsolus/solbuild/util"
)

const progressBarTemplate string = `{{with string . "prefix"}}{{.}} {{end}}{{printf "%25s" (counters .) }} {{bar . }} {{printf "%7s" (percent .) }} {{printf "%14s" (speed . "%s/s" "??/s")}}{{with string . "suffix"}} {{.}}{{end}}`

// A SimpleSource is a tarball or other source for a package.
type SimpleSource struct {
	URI  string
	File string // Basename of the file

	legacy    bool   // If this is ypkg or not
	validator string // Validation key for this source

	url *url.URL
}

// NewSimple will create a new source instance.
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

// GetPath gets the path on the filesystem of the source.
func (s *SimpleSource) GetPath(hash string) string {
	return filepath.Join(SourceDir, hash, s.File)
}

// GetSHA1Sum will return the sha1sum for the given path.
func (s *SimpleSource) GetSHA1Sum(path string) (string, error) {
	inp, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}

	hash := sha1.New()
	hash.Write(inp)
	sum := hash.Sum(nil)

	return hex.EncodeToString(sum), nil
}

// GetSHA256Sum will return the sha1sum for the given path.
func (s *SimpleSource) GetSHA256Sum(path string) (string, error) {
	inp, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}

	hash := sha256.New()
	hash.Write(inp)
	sum := hash.Sum(nil)

	return hex.EncodeToString(sum), nil
}

// IsFetched will determine if the source is already present.
func (s *SimpleSource) IsFetched() bool {
	return PathExists(s.GetPath(s.validator))
}

// download downloads simple files using go grab.
func (s *SimpleSource) download(destination string) error {
	if IsFileURI(s.url) {
		return CopyFile(s.url.Path, destination)
	}

	// Some web servers (*cough* sourceforge) have strange redirection behavior. It's possible to work around this by clearing the Referer header on every redirect
	headHttpClient := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			for k := range req.Header {
				if strings.ToLower(k) == "referer" {
					delete(req.Header, k)
				}
			}
			return nil
		},
		Transport: &http.Transport{
			DisableCompression: true,
			Proxy:              http.ProxyFromEnvironment,
		},
	}

	// Do a HEAD request, following all redirects until we get the final URL.
	headResp, err := headHttpClient.Head(s.URI)
	if err != nil {
		return err
	}
	defer headResp.Body.Close()

	finalURL := headResp.Request.URL.String()
	if s.URI != finalURL {
		slog.Info("Source URL redirected", "uri", finalURL)
	}

	req, err := grab.NewRequest(destination, finalURL)
	if err != nil {
		return err
	}

	// Indicate that we will accept any response content-type. Some servers will fail without this (like netfilter.org)
	// https://developer.mozilla.org/en-US/docs/Web/HTTP/Headers/Accept#sect1
	req.HTTPRequest.Header.Add("Accept", "*/*")

	// Request content without modification or compression
	// https://developer.mozilla.org/en-US/docs/Web/HTTP/Headers/Accept-Encoding#identity
	req.HTTPRequest.Header.Add("Accept-Encoding", "identity")

	// Ensure the checksum matches
	if !s.legacy {
		sum, err := hex.DecodeString(s.validator)
		if err != nil {
			return err
		}

		req.SetChecksum(sha256.New(), sum, false)
	}

	// Create a client with compression disabled.
	// See: https://github.com/cavaliergopher/grab/blob/v3.0.1/v3/client.go#L53
	client := &grab.Client{
		// To be fully compliant with the User-Agent spec we need to include the version
		// https://developer.mozilla.org/en-US/docs/Web/HTTP/Headers/User-Agent
		UserAgent: "solbuild/" + util.SolbuildVersion,
		HTTPClient: &http.Client{
			Transport: &http.Transport{
				DisableCompression: true,
				Proxy:              http.ProxyFromEnvironment,
			},
		},
	}
	resp := client.Do(req)

	// Show our progress bar
	s.showProgress(resp)

	if err := resp.Err(); err != nil {
		slog.Error("Error downloading", "uri", s.URI, "err", err)

		return err
	}

	return nil
}

func onTTY() bool {
	s, _ := os.Stdout.Stat()

	return s.Mode()&os.ModeCharDevice > 0
}

func (s *SimpleSource) showProgress(resp *grab.Response) {
	if !onTTY() {
		slog.Info("Downloading source", "uri", s.URI)

		return
	}

	pbar := pb.Start64(resp.Size())
	pbar.Set(pb.Bytes, true)
	pbar.SetTemplateString(progressBarTemplate)
	pbar.SetWriter(os.Stdout)

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

			return
		}
	}
}

// Fetch will download the given source and cache it locally.
func (s *SimpleSource) Fetch() error {
	// Now go and download it
	slog.Debug("Downloading source", "uri", s.URI)

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
