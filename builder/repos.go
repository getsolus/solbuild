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
	"os"
	"path/filepath"

	"github.com/getsolus/libosdev/disk"
)

const (
	// BindRepoDir is where we make repos available from the host side.
	BindRepoDir = "/hostRepos"
)

func (e *EopkgManager) addRepos(notif PidNotifier, o *Overlay, repos []*Repo) error {
	if len(repos) < 1 {
		return nil
	}

	for _, repo := range repos {
		if repo.Local {
			slog.Debug("Adding local repo to system", "name", repo.Name, "uri", repo.URI)

			if err := e.addLocalRepo(notif, o, repo); err != nil {
				return fmt.Errorf("Failed to add local repo to system %s, reason: %w\n", repo.Name, err)
			}

			continue
		}

		slog.Debug("Adding repo to system", "name", repo.Name, "uri", repo.URI)

		if err := e.AddRepo(repo.Name, repo.URI); err != nil {
			return fmt.Errorf("Failed to add repo to system %s, reason: %w\n", repo.Name, err)
		}
	}

	return nil
}

func (e *EopkgManager) removeRepos(repos []string) error {
	if len(repos) < 1 {
		return nil
	}

	for _, id := range repos {
		slog.Debug("Removing repository", "repo", id)

		if err := e.RemoveRepo(id); err != nil {
			return fmt.Errorf("Failed to remove repository %s, reason: %w\n", id, err)
		}
	}

	return nil
}

func (e *EopkgManager) addLocalRepo(notif PidNotifier, o *Overlay, repo *Repo) error {
	// Ensure the source exists too. Sorta helpful like that.
	if !PathExists(repo.URI) {
		return fmt.Errorf("Local repo does not exist")
	}

	mman := disk.GetMountManager()

	// Ensure the target mountpoint actually exists ...
	tgt := filepath.Join(o.MountPoint, BindRepoDir[1:], repo.Name)
	if !PathExists(tgt) {
		if err := os.MkdirAll(tgt, 0o0755); err != nil {
			return err
		}
	}

	// BindMount the directory into place
	if err := mman.BindMount(repo.URI, tgt); err != nil {
		return err
	}

	o.ExtraMounts = append(o.ExtraMounts, tgt)

	// Attempt to autoindex the repo
	if repo.AutoIndex {
		slog.Debug("Reindexing repository", "name", repo.Name)

		command := fmt.Sprintf("cd %s/%s; %s", BindRepoDir, repo.Name, eopkgCommand(installCommand+" index --skip-signing ."))
		err := ChrootExec(notif, o.MountPoint, command)
		notif.SetActivePID(0)

		if err != nil {
			return err
		}
	} else {
		tgtIndex := filepath.Join(tgt, "eopkg-index.xml.xz")
		if !PathExists(tgtIndex) {
			slog.Warn("Repository index doesn't exist. Please index it to use it.", "repo", repo.Name)
		}
	}

	// Now add the local repo
	chrootLocal := filepath.Join(BindRepoDir, repo.Name, "eopkg-index.xml.xz")

	return e.AddRepo(repo.Name, chrootLocal)
}

func (e *EopkgManager) ConfigureRepos(notif PidNotifier, o *Overlay, profile *Profile) error {
	repos, err := e.GetRepos()
	if err != nil {
		return err
	}

	var removals []string

	// Find out which repos to remove
	if len(profile.RemoveRepos) == 1 && profile.RemoveRepos[0] == "*" {
		for _, r := range repos {
			removals = append(removals, r.ID)
		}
	} else {
		removals = append(removals, profile.RemoveRepos...)
	}

	if err := e.removeRepos(removals); err != nil {
		return err
	}

	var addRepos []*Repo

	if (len(profile.AddRepos) == 1 && profile.AddRepos[0] == "*") || len(profile.AddRepos) == 0 {
		for _, repo := range profile.Repos {
			addRepos = append(addRepos, repo)
		}
	} else {
		for _, id := range profile.AddRepos {
			addRepos = append(addRepos, profile.Repos[id])
		}
	}

	return e.addRepos(notif, o, addRepos)
}
