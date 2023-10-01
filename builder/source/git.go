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
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const (
	// GitSourceDir is the base directory for all cached git sources.
	GitSourceDir = "/var/lib/solbuild/sources/git"
)

// A GitSource as referenced by `ypkg` build spec. A git source must have
// a valid ref to check out to.
type GitSource struct {
	URI       string
	Ref       string
	BaseName  string
	ClonePath string // This is where we will have cloned into
}

// NewGit will create a new GitSource for the given URI & ref combination.
func NewGit(uri, ref string) (*GitSource, error) {
	// Ensure we have a valid URL first.
	urlObj, err := url.Parse(uri)
	if err != nil {
		return nil, err
	}

	bs := filepath.Base(urlObj.Path)
	if !strings.HasSuffix(bs, ".git") {
		bs += ".git"
	}

	// This is where we intend to clone to locally
	clonePath := filepath.Join(GitSourceDir, urlObj.Host, filepath.Dir(urlObj.Path), bs)

	g := &GitSource{
		URI:       uri,
		Ref:       ref,
		BaseName:  bs,
		ClonePath: clonePath,
	}

	return g, nil
}

// submodules will handle setup of the git submodules after a
// reset has taken place.
func (g *GitSource) submodules() error {
	cmd := exec.Command("git", "submodule", "update", "--init", "--recursive")

	cmd.Dir = g.ClonePath
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	return cmd.Run()
}

// clone shallow clones an upstream git repository to the local disk.
func clone(uri, path, ref string) error {
	var cmd *exec.Cmd

	// Check if the reference is a commit
	if len(ref) == 40 {
		// Init a new repository at the checkout path
		initCmd := exec.Command("git", "init", path)

		if err := initCmd.Run(); err != nil {
			return err
		}

		// Set the default remote to the upstream URI
		addRemoteCmd := exec.Command("git", "remote", "add", "origin", uri)
		addRemoteCmd.Dir = path

		if err := addRemoteCmd.Run(); err != nil {
			return err
		}

		// Shallow fetch the reference we want
		fetchCmd := exec.Command("git", "fetch", "--depth", "1", "origin", ref)
		fetchCmd.Dir = path

		if err := fetchCmd.Run(); err != nil {
			return err
		}

		// Set the next command to run to checkout the head
		cmd = exec.Command("git", "checkout", "FETCH_HEAD")
		cmd.Dir = path
	} else {
		// Not a git commit, so shallow clone the repo at the reference
		cmd = exec.Command("git", "clone", "--depth", "1", "--branch", ref, uri, path)
	}

	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	return cmd.Run()
}

// reset fetches the new git reference (a tag or commit SHA1 hash) and
// hard resets the repository on that reference.
func reset(path, ref string) error {
	fetchArgs := []string{
		"fetch",
		"--depth",
		"1",
		"origin",
	}

	// We have to add the tag keyword if the ref is a tag, otherwise
	// git won't actually fetch the tag
	if len(ref) != 40 {
		fetchArgs = append(fetchArgs, "tag")
	}

	fetchArgs = append(fetchArgs, ref)

	fetchCmd := exec.Command("git", fetchArgs...)
	fetchCmd.Dir = path

	if err := fetchCmd.Run(); err != nil {
		return err
	}

	resetCmd := exec.Command("git", "reset", "--hard", ref)
	resetCmd.Dir = path

	return resetCmd.Run()
}

// Fetch will attempt to download the git tree locally. If it already exists
// then we'll make an attempt to update it.
func (g *GitSource) Fetch() error {
	// First things first, make sure we have a destination
	if !PathExists(g.ClonePath) {
		if err := clone(g.URI, g.ClonePath, g.Ref); err != nil {
			return err
		}
	} else {
		// Repo already exists locally, try to reset to the new reference
		if err := reset(g.ClonePath, g.Ref); err != nil {
			return err
		}
	}

	// Check out submodules
	return g.submodules()
}

// IsFetched will check if we have the ref available, if not it will return
// false so that Fetch() can do the hard work.
func (g *GitSource) IsFetched() bool {
	return false
}

// GetBindConfiguration will return a config that enables bind mounting
// the bare git clone from the host side into the container, at which
// point ypkg can git clone from the bare git into a new tree and check
// out, make changes, etc.
func (g *GitSource) GetBindConfiguration(sourcedir string) BindConfiguration {
	return BindConfiguration{
		g.ClonePath,
		filepath.Join(sourcedir, g.BaseName),
	}
}

// GetIdentifier will return a human readable string to represent this
// git source in the event of errors.
func (g *GitSource) GetIdentifier() string {
	return fmt.Sprintf("%s#%s", g.URI, g.Ref)
}
