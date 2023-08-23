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
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)

// A Repo is a definition of a repository to add to the eopkg root during
// the build process.
type Repo struct {
	Name      string `toml:"-"`         // Name of the repo, set by implementation not yoml
	URI       string `toml:"uri"`       // URI of the repository
	Local     bool   `toml:"local"`     // Local repository for bindmounting
	AutoIndex bool   `toml:"autoindex"` // Enable automatic indexing of the repo
}

// A Profile is a configuration defining what backing image to use, what repos
// to add, etc.
type Profile struct {
	AddRepos    []string         `toml:"add_repos"`    // Allow locking to a single set of repos
	Image       string           `toml:"image"`        // The backing image for this profile
	Name        string           `toml:"-"`            // Name of this profile, set by file name not toml
	RemoveRepos []string         `toml:"remove_repos"` // A set of repos to remove. ["*"] is valid here.
	Repos       map[string]*Repo `toml:"repo"`         // Allow defining custom repos
}

// ProfileSuffix is the fixed extension for solbuild profile files.
var ProfileSuffix = ".profile"

// NewProfile will attempt to load the named profile from the system paths.
func NewProfile(name string) (*Profile, error) {
	for _, p := range ConfigPaths {
		fp := filepath.Join(p, fmt.Sprintf("%s%s", name, ProfileSuffix))
		if !PathExists(fp) {
			continue
		}

		return NewProfileFromPath(fp)
	}

	return nil, ErrInvalidProfile
}

// GetAllProfiles will locate all available profiles for solbuild.
func GetAllProfiles() (map[string]*Profile, error) {
	ret := make(map[string]*Profile)

	for _, p := range ConfigPaths {
		gl := filepath.Join(p, "*.profile")

		profiles, _ := filepath.Glob(gl)

		for _, o := range profiles {
			if profile, err := NewProfileFromPath(o); err == nil {
				ret[profile.Name] = profile
			} else {
				return nil, err
			}
		}
	}

	return ret, nil
}

// NewProfileFromPath will attempt to load a profile from the given file name.
func NewProfileFromPath(path string) (*Profile, error) {
	basename := filepath.Base(path)
	if !strings.HasSuffix(basename, ProfileSuffix) {
		return nil, fmt.Errorf("Not a .profile file: %v", path)
	}

	fi, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer fi.Close()

	profileName := basename[:len(basename)-len(ProfileSuffix)]

	var b []byte

	profile := &Profile{Name: profileName}

	// Read the config file
	if b, err = io.ReadAll(fi); err != nil {
		return nil, err
	}

	if _, err = toml.Decode(string(b), profile); err != nil {
		return nil, err
	}

	// Ensure all repos have a valid name
	for name, repo := range profile.Repos {
		repo.Name = name
	}

	// Ignore a wildcard add
	if len(profile.AddRepos) == 1 && profile.AddRepos[0] == "*" {
		return profile, nil
	}

	// Check all repo names are valid
	for _, r := range profile.AddRepos {
		if _, ok := profile.Repos[r]; !ok {
			return nil, fmt.Errorf("Cannot enable unknown repo %v", r)
		}
	}

	return profile, nil
}
