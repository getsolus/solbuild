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
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	log "github.com/DataDrake/waterlog"
	"github.com/getsolus/libosdev/commands"
	"github.com/getsolus/libosdev/disk"
)

// ChrootEnvironment is the env used by ChrootExec calls.
var ChrootEnvironment []string

func init() {
	ChrootEnvironment = nil
}

// PidNotifier provides a simple way to set the PID on a blocking process.
type PidNotifier interface {
	SetActivePID(int)
}

// ActivateRoot will do the hard work of actually bring up the overlayfs
// system to allow manipulation of the roots for builds, etc.
func (p *Package) ActivateRoot(overlay *Overlay) error {
	log.Debugln("Configuring overlay storage")

	// Now mount the overlayfs
	if err := overlay.Mount(); err != nil {
		return err
	}

	// Add build user
	if p.Type == PackageTypeYpkg {
		if err := AddBuildUser(overlay.MountPoint); err != nil {
			return err
		}
	}

	log.Debugln("Bringing up virtual filesystems")
	return overlay.MountVFS()
}

// DeactivateRoot will tear down the previously activated root.
func (p *Package) DeactivateRoot(overlay *Overlay) {
	MurderDeathKill(overlay.MountPoint)
	mountMan := disk.GetMountManager()
	commands.SetStdin(nil)
	overlay.Unmount()
	log.Debugln("Requesting unmount of all remaining mountpoints")
	mountMan.UnmountAll()
}

// MurderDeathKill will find all processes with a root matching the given root
// and set about killing them, to assist in clean closing.
func MurderDeathKill(root string) error {
	path, err := filepath.EvalSymlinks(root)
	if err != nil {
		return err
	}

	var files []os.DirEntry

	if files, err = os.ReadDir("/proc"); err != nil {
		return err
	}

	for _, f := range files {
		fpath := filepath.Join("/proc", f.Name(), "cwd")

		spath, err := filepath.EvalSymlinks(fpath)
		if err != nil {
			continue
		}

		if spath != path {
			continue
		}

		spid := f.Name()
		var pid int

		if pid, err = strconv.Atoi(spid); err != nil {
			return fmt.Errorf("POSIX Weeps - broken pid identifier %s, reason: %w\n", spid, err)
		}

		log.Debugf("Killing child process in chroot %d\n", pid)

		if err := syscall.Kill(pid, syscall.SIGTERM); err != nil {
			log.Errorf("Error terminating process, attempting force kill %d\n", pid)
			time.Sleep(400 * time.Millisecond)
			if err := syscall.Kill(pid, syscall.SIGKILL); err != nil {
				log.Errorf("Error killing (-9) process %d\n", pid)
			}
		}
	}
	return nil
}

// TouchFile will create the file if it doesn't exist, enabling use of bind
// mounts.
func TouchFile(path string) error {
	w, err := os.OpenFile(path, os.O_RDONLY|os.O_CREATE, 0o0644)
	if err != nil {
		return err
	}
	defer w.Close()
	return nil
}

// SaneEnvironment will generate a clean environment for the chroot'd
// processes to use.
func SaneEnvironment(username, home string) []string {
	environment := []string{
		"PATH=/usr/bin:/usr/sbin:/bin/:/sbin",
		"LANG=en_US.UTF-8",
		"LC_ALL=en_US.UTF-8",
		fmt.Sprintf("HOME=%s", home),
		fmt.Sprintf("USER=%s", username),
		fmt.Sprintf("USERNAME=%s", username),
	}
	// Consider an option to even filter these out
	permitted := []string{
		"http_proxy",
		"https_proxy",
		"no_proxy",
		"ftp_proxy",
		"TERM",
	}
	if !DisableColors {
		permitted = append(permitted, "TERM")
	}
	for _, p := range permitted {
		env := os.Getenv(p)
		if env == "" {
			p = strings.ToUpper(p)
			env = os.Getenv(p)
		}
		if env == "" {
			continue
		}
		environment = append(environment,
			fmt.Sprintf("%s=%s", p, env))
	}
	if DisableColors {
		environment = append(environment, "TERM=dumb")
	}
	return environment
}

// ChrootExec is a simple wrapper to return a correctly set up chroot command,
// so that we can store the PID, for long running tasks.
func ChrootExec(notif PidNotifier, dir, command string) error {
	args := []string{dir, "/bin/sh", "-c", command}
	c := exec.Command("chroot", args...)
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	c.Stdin = nil
	c.Env = ChrootEnvironment
	c.SysProcAttr = &syscall.SysProcAttr{Setsid: true}

	if err := c.Start(); err != nil {
		return err
	}
	notif.SetActivePID(c.Process.Pid)
	return c.Wait()
}

// ChrootExecStdin is almost identical to ChrootExec, except it permits a stdin
// to be associated with the command.
func ChrootExecStdin(notif PidNotifier, dir, command string) error {
	args := []string{dir, "/bin/sh", "-c", command}
	c := exec.Command("chroot", args...)
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	c.Stdin = os.Stdin
	c.Env = ChrootEnvironment

	if err := c.Start(); err != nil {
		return err
	}
	notif.SetActivePID(c.Process.Pid)
	return c.Wait()
}

// AddBuildUser will attempt to add the solbuild user & group if they've not
// previously been added
// Note this should be changed when Solus goes fully stateless for /etc/passwd.
func AddBuildUser(rootfs string) error {
	pwd, err := NewPasswd(filepath.Join(rootfs, "etc"))
	if err != nil {
		return fmt.Errorf("Unable to discover chroot users, reason: %w\n", err)
	}
	// User already exists
	if _, ok := pwd.Users[BuildUser]; ok {
		return nil
	}
	log.Debugf("Adding build user to system: user='%s' uid='%d' gid='%d' home='%s' shell='%s' gecos='%s'\n", BuildUser, BuildUserID, BuildUserGID, BuildUserHome, BuildUserShell, BuildUserGecos)

	// Add the build group
	if err := commands.AddGroup(rootfs, BuildUser, BuildUserGID); err != nil {
		return fmt.Errorf("Failed to add build group to system, reason: %w\n", err)
	}

	if err := commands.AddUser(rootfs, BuildUser, BuildUserGecos, BuildUserHome, BuildUserShell, BuildUserID, BuildUserGID); err != nil {
		return fmt.Errorf("Failed to add build user to system, reason: %w\n", err)
	}
	return nil
}

// FileSha256sum is a quick wrapper to grab the sha256sum for the given file.
func FileSha256sum(path string) (string, error) {
	mfile, err := MapFile(path)
	if err != nil {
		return "", err
	}
	defer mfile.Close()
	h := sha256.New()
	// Pump from memory into hash for zero-copy sha1sum
	h.Write(mfile.Data)
	return hex.EncodeToString(h.Sum(nil)), nil
}

// ValidMemSize will determine if a string is a valid memory size,
// it must start with a number and end with a valid unit size.
func ValidMemSize(s string) bool {
	if s == "" {
		return false
	}

	// Size is numeric?
	allButLast := s[0 : len(s)-1]
	_, err := strconv.ParseFloat(allButLast, 64)
	if err != nil {
		log.Errorf("Invalid Memory Size: %s: %s is not numeric\n", s, allButLast)
		return false
	}

	// Size ends with valid memory unit?
	lastChar := s[len(s)-1:]
	validLastChars := []string{"G", "T", "P", "E"}
	for _, v := range validLastChars {
		if v == lastChar {
			return true
		}
	}
	log.Errorf("Invalid Memory Size: %s doesn't end in a valid memory unit, e.g. G\n", s)
	return false
}
