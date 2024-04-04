package builder

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/zeebo/blake3"
)

type Layer struct {
	deps    []Dep
	profile *Profile
	back    *BackingImage
	created bool
	hash    string
}

func (l Layer) MarshalJSON() ([]byte, error) {
	var imageHash string
	var err error
	if PathExists(l.back.ImagePath) {
		if imageHash, err = xxh3128HashFile(l.back.ImagePath); err != nil {
			return nil, err
		}
		// } else if PathExists(l.back.ImagePath) {
		// 	if imageHash, err = hashFile(l.back.ImagePath); err != nil {
		// 		return
		// 	}
	} else {
		return nil, fmt.Errorf("Backing image doens't exist at %s", l.back.ImagePath)
	}

	return json.Marshal(struct {
		Deps      []Dep `json:"deps"`
		ImageHash string
	}{Deps: l.deps, ImageHash: imageHash})
}

func (l *Layer) Hash() string {
	if l.hash == "" {
		jsonBytes, err := json.Marshal(l)
		if err != nil {
			l.hash = LayersFakeHash
		} else {
			hashBytes := blake3.Sum256(jsonBytes)
			l.hash = base64.StdEncoding.EncodeToString(hashBytes[:])
		}
	}
	return l.hash

}

func (l *Layer) BasePath() string {
	return filepath.Join(LayersDir, l.Hash())
}

func (l *Layer) RequestOverlay(notif PidNotifier) (contentPath string, err error) {
	contentPath = filepath.Join(l.BasePath(), "content")
	if !PathExists(contentPath) || l.Hash() == LayersFakeHash {
		slog.Info("Creating layer", "hash", l.Hash())
		return l.Create(notif)
	} else {
		slog.Info("Reusing layer", "hash", l.Hash())
		return
	}
}

func (l *Layer) RemoveIfNotCreated() {
	slog.Debug("Layer not fully created, removing...", "path", l.BasePath())
	if !l.created {
		os.RemoveAll(l.BasePath())
	}
}

func (l *Layer) Create(notif PidNotifier) (contentPath string, err error) {
	basePath := l.BasePath()
	contentPath = filepath.Join(basePath, "content")

	depsOverlay := Overlay{
		Back:           l.back,
		Package:        nil,
		BaseDir:        basePath,
		WorkDir:        filepath.Join(basePath, "work"),
		UpperDir:       contentPath,
		ImgDir:         filepath.Join(basePath, "img"),
		MountPoint:     filepath.Join(basePath, "union"),
		LockPath:       filepath.Join(basePath, "lock"),
		EnableTmpfs:    false,
		mountedImg:     false,
		mountedOverlay: false,
		mountedVFS:     false,
		mountedTmpfs:   false,
	}

	if err = depsOverlay.CleanExisting(); err != nil {
		err = fmt.Errorf("Failed to cleanup existing overlay (if exists), reason: %w", err)
		return
	}

	if err = depsOverlay.EnsureDirs(); err != nil {
		err = fmt.Errorf("Failed to ensure dirs for overlay, reason: %w", err)
		return
	}

	// Mount overlay
	if err = depsOverlay.ActivateRoot(); err != nil {
		err = fmt.Errorf("Failed to activate overlay, reason: %w", err)
		return
	}
	defer depsOverlay.DeactivateRoot()

	pman := NewEopkgManager(notif, depsOverlay.MountPoint)

	// Init pman
	if err = pman.Init(); err != nil {
		err = fmt.Errorf("Failed to init pman, reason: %w", err)
		return
	}
	defer pman.Cleanup()

	// Bring up dbus to do Things
	slog.Debug("Starting D-BUS")

	if err = pman.StartDBUS(); err != nil {
		err = fmt.Errorf("Failed to start d-bus, reason: %w", err)
		return
	}

	// Get the repos in place before asserting anything
	if err = pman.ConfigureRepos(notif, &depsOverlay, l.profile); err != nil {
		err = fmt.Errorf("Configuring repositories failed, reason: %w", err)
		return
	}

	// Now, install/upgrade everything!
	slog.Debug("Upgrading system base and other core packages")

	if err = pman.Upgrade(); err != nil {
		err = fmt.Errorf("Failed to upgrade layer rootfs, reason: %w", err)
		return
	}

	slog.Debug("Asserting system.devel component installation")
	if err = pman.InstallComponent("system.devel"); err != nil {
		err = fmt.Errorf("Failed to assert system.devel in layer, reason: %w", err)
		return
	}

	// Install our dependencies
	pkgs := make([]string, len(l.deps))
	for idx, dep := range l.deps {
		pkgs[idx] = dep.Name
	}
	slog.Debug("Installing dependencies", "size", len(pkgs), "pkgs", pkgs)
	cmd := fmt.Sprintf("eopkg it -y %s", strings.Join(pkgs, " "))
	if DisableColors {
		cmd += " -n"
	}

	slog.Debug("Installing dependencies")

	if err = ChrootExec(notif, depsOverlay.MountPoint, cmd); err != nil {
		err = fmt.Errorf("Failed to install dependencies, reason: %w", err)
		return
	}
	notif.SetActivePID(0)

	return
}
