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

	log "github.com/DataDrake/waterlog"
	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
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

// GetHead will attempt to gain the OID for head.
func (g *GitSource) GetHead(repo *git.Repository) (string, error) {
	head, err := repo.Head()
	if err != nil {
		return "", err
	}

	return head.Target().String(), nil
}

// submodules will handle setup of the git submodules after a
// reset has taken place.
func (g *GitSource) submodules() error {
	// We call out to git directly because go-git takes far longer (if it
	// ever actually completes) and takes way more resources than just
	// calling git directly.
	cmd := exec.Command("git", "submodule", "update", "--init", "--recursive")

	cmd.Dir = g.ClonePath
	cmd.Stdout = os.Stdout

	return cmd.Run()
}

func clone(uri, path, ref string) (*git.Repository, error) {
	// var refName plumbing.ReferenceName

	// if len(ref) == 40 {
	// 	refName = plumbing.NewBranchReferenceName(ref)
	// } else {
	// 	refName = plumbing.NewTagReferenceName(ref)
	// }

	cloneOpts := git.CloneOptions{
		URL: uri,
		// ReferenceName: refName,
		SingleBranch: true,
		Depth:        1,
		Progress:     os.Stdout,
		Tags:         git.NoTags,
	}

	return git.PlainClone(path, false, &cloneOpts)
}

func fixPackfilePerms(path string) error {
	packDir := filepath.Join(path, ".git", "objects", "pack")
	files, err := os.ReadDir(packDir)
	if err != nil {
		return err
	}

	for _, file := range files {
		filePath := filepath.Join(packDir, file.Name())

		if err = os.Chmod(filePath, 644); err != nil {
			return err
		}
	}

	return nil
}

func (g *GitSource) resolveHash(repo *git.Repository) (*plumbing.Hash, error) {
	var hash plumbing.Hash

	if len(g.Ref) != 40 {
		log.Debugf("reference '%s' does not look like a hash; attempting to resolve\n", g.Ref)

		h, err := repo.ResolveRevision(plumbing.Revision(g.Ref))

		if err != nil {
			return nil, err
		}

		hash = *h
	} else {
		hash = plumbing.NewHash(g.Ref)
	}

	return &hash, nil
}

// Fetch will attempt to download the git tree locally. If it already exists
// then we'll make an attempt to update it.
func (g *GitSource) Fetch() error {
	// First things first, make sure we have a destination
	var (
		repo *git.Repository
		err  error
	)

	if !PathExists(g.ClonePath) {
		//log.Debugf("making shallow clone of repo at '%s'\n", g.ClonePath)

		//_, err := clone(g.URI, g.ClonePath, g.Ref)
		//if err != nil {
		//	return err
		//}

		//log.Debugln("fixing packfile permissions")

		// For some reason, go-git creates the packfiles with read permissions
		// for only the file owner, unlike git. This means that cloned sources
		// cannot be copied to the work dir. So, attempt to fix the permissions.
		//if err = fixPackfilePerms(g.ClonePath); err != nil {
		//	return err
		//}

		repo, err = git.PlainClone(g.ClonePath, false, &git.CloneOptions{
			URL:   g.URI,
			Depth: 1,
			Tags:  git.NoTags,
		})

		if err != nil {
			return err
		}

		//if _, err := repo.CreateRemote(&config.RemoteConfig{
		//	Name:  git.DefaultRemoteName,
		//	URLs:  []string{g.URI},
		//	Fetch: []config.RefSpec{config.RefSpec(fmt.Sprintf(config.DefaultFetchRefSpec, git.DefaultRemoteName))},
		//}); err != nil {
		//	return err
		//}
	} else {
		repo, err = git.PlainOpen(g.ClonePath)

		if err != nil {
			return err
		}
	}

	// Repo already on disk, just open it
	//log.Debugf("source repo clone found on disk at '%s'\n", g.ClonePath)

	//repo, err := git.PlainOpen(g.ClonePath)
	//if err != nil {
	//	return err
	//}

	// Get the ref we want
	hash, err := g.resolveHash(repo)

	if err != nil {
		return err
	}

	log.Debugf("resolved revision: %s\n", hash.String())
	log.Debugln("fetching revision from remote")

	if err = repo.Fetch(&git.FetchOptions{
		Depth:      1,
		Tags:       git.AllTags,
		RemoteName: git.DefaultRemoteName,
		RemoteURL:  g.URI,
		RefSpecs: []config.RefSpec{
			config.RefSpec(fmt.Sprintf("%s:%s", hash.String(), hash.String())),
		},
	}); err != nil {
		return err
	}

	work, err := repo.Worktree()

	if err != nil {
		return err
	}

	if err = work.Reset(&git.ResetOptions{
		Commit: *hash,
		Mode:   git.HardReset,
	}); err != nil {
		return err
	}

	// Check out submodules
	log.Debugln("updating submodules")
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
