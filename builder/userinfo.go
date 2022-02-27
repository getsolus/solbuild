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
	log "github.com/DataDrake/waterlog"
	"gopkg.in/ini.v1"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
)

// UserInfo is required for ypkg builds, to set the .config/solus/package internally
// and propagate the author details.
type UserInfo struct {
	Name     string // Actual name
	Email    string // Actual email
	UID      int    // Unix User Id
	GID      int    // Unix Group ID
	HomeDir  string // Home directory of the user
	Username string // Textual username
}

const (
	// FallbackUserName is what we fallback to if everything else fails
	FallbackUserName = "Automated Package Build"

	// FallbackUserEmail is what we fallback to if everything else fails
	FallbackUserEmail = "no.email.set.in.config"
)

// SetFromSudo will attempt to set our details from sudo user environment
func (u *UserInfo) SetFromSudo() bool {
	sudoUID := os.Getenv("SUDO_UID")
	sudoGID := os.Getenv("SUDO_GID")
	uid := -1
	gid := -1
	var err error

	if sudoGID == "" {
		sudoGID = sudoUID
	}

	if sudoUID == "" {
		return false
	}

	if uid, err = strconv.Atoi(sudoUID); err != nil {
		log.Errorf("Malformed SUDO_UID in environment %s %s\n", sudoUID, err)
		return false
	}

	if gid, err = strconv.Atoi(sudoGID); err != nil {
		log.Errorf("Malformed SUDO_GID in environment %s %s\n", sudoGID, err)
		return false
	}

	u.UID = uid
	u.GID = gid

	// Try to set the home directory
	usr, err := user.LookupId(sudoUID)
	if err != nil {
		log.Errorf("Failed to lookup SUDO_USER entry %d %s\n", uid, err)
		return false
	}

	// Now store the home directory for that user
	u.HomeDir = usr.HomeDir
	u.Username = usr.Username
	// In case of future fails
	u.Name = usr.Name

	return true
}

// SetFromCurrent will set the UserInfo details from the current user
func (u *UserInfo) SetFromCurrent() {
	u.UID = os.Getuid()
	u.GID = os.Getgid()

	if usr, err := user.Current(); err != nil {
		u.HomeDir = usr.HomeDir
		u.Username = usr.Username
		u.Name = usr.Name
	} else {
		log.Errorf("Failed to lookup current user %d %s\n", u.UID, err)
		u.Username = os.Getenv("USERNAME")
		u.Name = u.Username
		u.HomeDir = filepath.Join("/home", u.Username)
	}
}

// SetFromPackager will set the username/email fields from one of our packager files
func (u *UserInfo) SetFromPackager() bool {
	candidatePaths := []string{
		filepath.Join(u.HomeDir, ".config", "solus", "packager"),
		filepath.Join(u.HomeDir, ".solus", "packager"),
		filepath.Join(u.HomeDir, ".evolveos", "packager"),
	}

	// Attempt to parse one of the packager files
	for _, p := range candidatePaths {
		if !PathExists(p) {
			continue
		}
		cfg, err := ini.Load(p)
		if err != nil {
			log.Errorf("Error loading INI file %s %s\n", p, err)
			continue
		}

		section, err := cfg.GetSection("Packager")
		if err != nil {
			log.Errorf("Missing [Packager] section in file %s\n", p)
			continue
		}

		uname, err := section.GetKey("Name")
		if err != nil {
			log.Errorf("Packager file has missing Name %s %s\n", p, err)
			continue
		}
		email, err := section.GetKey("Email")
		if err != nil {
			log.Errorf("Packager file has missing Email %s %s\n", p, err)
			continue
		}
		u.Name = uname.String()
		u.Email = email.String()
		log.Debugln("Setting packager details from packager INI file")
		return true
	}

	return false
}

// SetFromGit will set the username/email fields from the git config file
func (u *UserInfo) SetFromGit() bool {
	gitConfPath := filepath.Join(u.HomeDir, ".gitconfig")
	if !PathExists(gitConfPath) {
		return false
	}

	cfg, err := ini.Load(gitConfPath)
	if err != nil {
		log.Errorf("Error loading gitconfig %s %s\n", gitConfPath, err)
		return false
	}

	section, err := cfg.GetSection("user")
	if err != nil {
		log.Errorf("Missing [user] section in gitconfig %s\n", gitConfPath)
		return false
	}

	uname, err := section.GetKey("name")
	if err != nil {
		log.Errorf("gitconfig file has missing name %s %s\n", gitConfPath, err)
		return false
	}
	email, err := section.GetKey("email")
	if err != nil {
		log.Errorf("gitconfig file has missing email %s %s\n", gitConfPath, err)
		return false
	}
	u.Name = uname.String()
	u.Email = email.String()
	log.Debugln("Setting packager details from git config")

	return true
}

// GetUserInfo will always succeed, as it will use a fallback policy until it
// finally comes up with a valid combination of name/email to use.
func GetUserInfo() *UserInfo {
	uinfo := &UserInfo{}

	// First up try to set the uid/gid
	if !uinfo.SetFromSudo() {
		uinfo.SetFromCurrent()
	}

	attempts := []func() bool{
		uinfo.SetFromPackager,
		uinfo.SetFromGit,
	}

	for _, a := range attempts {
		if a() {
			return uinfo
		}
	}

	if uinfo.Name == "" {
		uinfo.Name = FallbackUserName
	}
	if uinfo.Email == "" {
		if ho, err := os.Hostname(); err != nil {
			uinfo.Email = fmt.Sprintf("%s@%s", uinfo.Username, ho)
		} else {
			uinfo.Email = FallbackUserEmail
		}
	}

	return uinfo
}

// WritePackager will attempt to write the packager file to given path
func (u *UserInfo) WritePackager(path string) error {
	fi, err := os.Create(path)
	if err != nil {
		return err
	}
	defer fi.Close()
	contents := fmt.Sprintf("[Packager]\nName=%s\nEmail=%s\n", u.Name, u.Email)
	if _, err := fi.WriteString(contents); err != nil {
		return err
	}
	return nil
}
