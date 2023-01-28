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
	"path/filepath"
	"strings"

	log "github.com/DataDrake/waterlog"
	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
)

const (
	// GitSourceDir is the base directory for all cached git sources
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

	log.Infof("ref: %s\n", g.Ref)
	return g, nil
}

// GetHead will attempt to gain the OID for head
func (g *GitSource) GetHead(repo *git.Repository) (string, error) {
	head, err := repo.Head()
	if err != nil {
		return "", err
	}
	return head.Target().String(), nil
}

// submodules will handle setup of the git submodules after a
// reset has taken place.
func (g *GitSource) submodules(work *git.Worktree) error {
	submodules, err := work.Submodules()
	if err != nil {
		return err
	}

	if len(submodules) <= 0 {
		return nil
	}

	opts := git.SubmoduleUpdateOptions{
		Init:              true,
		RecurseSubmodules: git.DefaultSubmoduleRecursionDepth,
	}
	return submodules.Update(&opts)
}

func clone(uri, path, ref string) (*git.Repository, error) {
	refName := plumbing.NewRemoteReferenceName("origin", ref)

	cloneOpts := git.CloneOptions{
		URL:               uri,
		ReferenceName:     refName,
		SingleBranch:      true,
		Depth:             1,
		RecurseSubmodules: git.DefaultSubmoduleRecursionDepth,
		Progress:          os.Stdout,
	}

	return git.PlainClone(path, true, &cloneOpts)
}

// Fetch will attempt to download the git tree locally. If it already exists
// then we'll make an attempt to update it.
func (g *GitSource) Fetch() error {
	// First things first, make sure we have a destination
	if !PathExists(g.ClonePath) {
		log.Debugf("making shallow clone of repo at '%s'\n", g.ClonePath)

		_, err := clone(g.URI, g.ClonePath, g.Ref)
		if err != nil {
			return err
		}

		return nil
	}

	// Repo already on disk, just open it
	log.Debugf("source repo clone found on disk at '%s'\n", g.ClonePath)

	repo, err := git.PlainOpen(g.ClonePath)
	if err != nil {
		return err
	}

	// Get the ref we want
	var hash plumbing.Hash
	if len(g.Ref) != 40 {
		log.Debugf("reference '%s' does not look like a hash; attempting to resolve\n", g.Ref)

		h, err := repo.ResolveRevision(plumbing.Revision(g.Ref))
		if err != nil {
			return err
		}
		hash = *h
	} else {
		hash = plumbing.NewHash(g.Ref)
	}

	log.Debugf("resolved reference: %s\n", hash.String())

	work, err := repo.Worktree()
	if err != nil {
		return err
	}

	checkoutOpts := git.CheckoutOptions{
		Hash:  hash,
		Force: true,
	}

	if err = work.Checkout(&checkoutOpts); err != nil {
		return err
	}

	// Testing file perms
	packDir := filepath.Join(g.ClonePath, ".git", "objects", "pack")
	entries, err := os.ReadDir(packDir)
	if err != nil {
		return err
	}
	for _, de := range entries {
		info, err := de.Info()
		if err != nil {
			log.Errorf("error reading pack file '%s': %s\n", de.Name(), err)
			continue
		}
		log.Infof("pack file '%s' has mode '%s'\n", de.Name(), info.Mode().String())
	}

	return g.submodules(work)
}

// FetchTest will attempt to download the git tree locally. If it already exists
// then we'll make an attempt to update it.
// func (g *GitSource) FetchTest() error {
// 	// First things first, make sure we have a destination
// 	if !PathExists(g.ClonePath) {
// 		log.Debugf("creating clone path at '%s'\n", g.ClonePath)

// 		// Make sure our fetch dir exists
// 		if err := os.MkdirAll(g.ClonePath, 0755); err != nil {
// 			return err
// 		}
// 		// Init an empty repo; we'll checkout and fetch later
// 		r, err := git.PlainInit(g.ClonePath, false)
// 		if err != nil {
// 			return err
// 		}
// 		g.repo = r
// 	} else {
// 		// Repo already on disk, just open it
// 		log.Debugf("source repo clone found on disk at '%s'\n", g.ClonePath)

// 		r, err := git.PlainOpen(g.ClonePath)
// 		if err != nil {
// 			return err
// 		}
// 		g.repo = r
// 	}

// 	// Get or create our origin remote to fetch
// 	remote, err := g.repo.Remote("origin")
// 	if err != nil && err != git.ErrRemoteNotFound {
// 		return err
// 	}
// 	if remote == nil {
// 		// Create a remote for origin
// 		log.Debugln("origin remote not found; creating")

// 		rconf := &config.RemoteConfig{
// 			Name: "origin",
// 			URLs: []string{g.URI},
// 			/* Fetch: []config.RefSpec{config.RefSpec("+refs/heads/*:refs/remotes/origin/*")}, */
// 		}

// 		remote, err = g.repo.CreateRemote(rconf)
// 		if err != nil {
// 			return err
// 		}
// 	}

// 	// Fetch the remote origin
// 	err = remote.Fetch(&git.FetchOptions{
// 		RefSpecs: []config.RefSpec{config.RefSpec("+refs/heads/*:refs/remotes/origin/*")},
// 		Force:    true,
// 		Progress: os.Stdout,
// 	})
// 	if err != nil && err != git.NoErrAlreadyUpToDate {
// 		return err
// 	}

// 	// Get the ref we want
// 	ref := g.Ref
// 	if len(ref) != 40 {
// 		log.Debugf("reference '%s' does not look like a hash; attempting to resolve\n", ref)

// 		hash, err := g.repo.ResolveRevision(plumbing.Revision(ref))
// 		if err != nil {
// 			return err
// 		}

// 		ref = hash.String()
// 	}

// 	log.Debugf("resolved reference: %s\n", ref)

// 	// Fetch at the ref we want
// 	err = remote.Fetch(&git.FetchOptions{
// 		RefSpecs: []config.RefSpec{config.RefSpec(ref + ":refs/heads/master")},
// 		Depth:    1,
// 		Force:    true,
// 		Progress: os.Stdout,
// 	})
// 	if err != nil && err != git.NoErrAlreadyUpToDate {
// 		return err
// 	}

// 	// Create a worktree
// 	work, err := g.repo.Worktree()
// 	if err != nil {
// 		return err
// 	}

// 	// Checkout our ref
// 	opts := git.CheckoutOptions{
// 		Hash:  plumbing.NewHash(g.Ref),
// 		Force: true,
// 	}

// 	if err = work.Checkout(&opts); err != nil {
// 		return err
// 	}

// 	// Check out submodules
// 	return g.submodules(work)
// }

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
