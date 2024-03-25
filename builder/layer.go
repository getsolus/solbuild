package builder

import (
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
)

type Layer struct {
	hash    string
	deps    []string
	profile *Profile
	back    *BackingImage
}

func (l *Layer) BasePath() string {
	return filepath.Join(LayersDir, l.hash)
}

func (l *Layer) RequestOverlay(notif PidNotifier, pman *EopkgManager) (err error, ovly *Overlay) {
	if PathExists(filepath.Join(l.BasePath(), "content")) {
		return l.Create(notif, pman)
	} else {
		return
	}
}

func (l *Layer) Create(notif PidNotifier, pman *EopkgManager) (err error, ovly *Overlay) {
	basePath := l.BasePath()
	contentPath := filepath.Join(basePath, "content")

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
	cmd := fmt.Sprintf("eopkg it -y %s", strings.Join(l.deps, " "))
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
