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
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/getsolus/libosdev/commands"
	"github.com/zeebo/blake3"
)

// ChrootEnvironment is the env used by ChrootExec calls.
var ChrootEnvironment []string

func init() {
	ChrootEnvironment = nil
}

// PidNotifier provides a simple way to set the PID on a blocking process.
type PidNotifier interface {
	SetActivePID(pid int)
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

		slog.Debug("Killing child process in chroot", "pid", pid)

		if err := syscall.Kill(pid, syscall.SIGTERM); err != nil {
			slog.Error("Error terminating process, attempting force kill", "pid", pid)
			time.Sleep(400 * time.Millisecond)

			if err := syscall.Kill(pid, syscall.SIGKILL); err != nil {
				slog.Error("Error killing (-9) process", "pid", pid)
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
		fmt.Sprintf("CCACHE_DIR=%s", path.Join(BuildUserHome, ".ccache")),
		fmt.Sprintf("SCCACHE_DIR=%s", path.Join(BuildUserHome, ".cache", "sccache")),
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
	slog.Debug("Executing in chroot", "dir", dir, "command", command)

	args := []string{dir, "/bin/sh", "-c", command}
	c := exec.Command("chroot", args...)
	c.Stdout = os.Stdout
	c.Stderr = os.Stdout
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
	c.Stderr = os.Stdout
	c.Stdin = os.Stdin
	c.Env = ChrootEnvironment

	if err := c.Start(); err != nil {
		return err
	}

	notif.SetActivePID(c.Process.Pid)

	return c.Wait()
}

func ChrootShell(notif PidNotifier, dir, command, workdir string) error {
	// Hold an fd for the og root
	fd, err := os.Open("/")
	if err != nil {
		return err
	}

	// Remember our working directory
	wd, err2 := os.Getwd()
	if err2 != nil {
		return err2
	}

	// Ensure chroot directory is available
	if err = os.Chdir(dir); err != nil {
		return err
	}

	if err = syscall.Chroot(dir); err != nil {
		fd.Close()
		return err
	}

	if err = os.Chdir("/"); err != nil {
		return err
	}

	// Spawn a shell
	args := []string{"--login"}
	c := exec.Command("/bin/bash", args...)
	c.Stdout = os.Stdout
	c.Stderr = os.Stdout
	c.Stdin = os.Stdin
	c.Env = ChrootEnvironment
	c.Dir = workdir

	if err = c.Start(); err != nil {
		goto CLEANUP
	}

	notif.SetActivePID(c.Process.Pid)

	if err = c.Wait(); err != nil {
		goto CLEANUP
	}

CLEANUP:
	// Return to our original root and working directory
	defer fd.Close()

	if err = fd.Chdir(); err != nil {
		return err
	}

	if err = syscall.Chroot("."); err != nil {
		return err
	}

	if err = os.Chdir(wd); err != nil {
		return err
	}

	return err
}

func StartSccache(dir string) {
	var buf bytes.Buffer

	c := exec.Command("chroot", dir, "/bin/su", "root", "-c", "sccache --start-server")
	c.Stdout = &buf
	c.Stderr = &buf
	c.Env = slices.Clone(ChrootEnvironment)
	c.Env = append(c.Env, "SCCACHE_IDLE_TIMEOUT=0")
	c.SysProcAttr = &syscall.SysProcAttr{Setsid: true}

	slog.Debug("Starting sccache server")

	if err := c.Run(); err != nil {
		slog.Warn("Unable to start sccache server", "err", err, "output", buf.String())
	}
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

	slog.Debug("Adding build user to system", "user", BuildUser, "uid", BuildUserID, "gid", BuildUserGID,
		"home", BuildUserHome, "shell", BuildUserShell, "gecos", BuildUserGecos)

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
		slog.Error(fmt.Sprintf("Invalid Memory Size: %s: %s is not numeric\n", s, allButLast))
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

	slog.Error(fmt.Sprintf("Invalid Memory Size: %s doesn't end in a valid memory unit, e.g. G\n", s))

	return false
}

func hashFileBytes(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	h := blake3.New()
	if _, err := io.Copy(h, f); err != nil {
		return nil, err
	}
	return h.Sum(nil), nil
}

func hashFile(path string) (string, error) {
	bytes, err := hashFileBytes(path)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", bytes), nil
}

func xxh3128HashFile(path string) (string, error) {
	cmd := exec.Command("xxh128sum", path)

	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("Failed to run xxh128sum %s, reason: %w", path, err)
	}
	return strings.Split(string(output), " ")[0], nil
}
