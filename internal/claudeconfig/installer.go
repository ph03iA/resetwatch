// Package claudeconfig installs and restores ResetWatch's user-level status line.
package claudeconfig

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	"resetwatch/internal/localfile"
)

var (
	ErrConflict  = errors.New("Claude already has a different status line; replacement requires approval")
	ErrOwnership = errors.New("Claude status line was changed outside ResetWatch; its settings and backup were preserved")
)

type Installer struct {
	Executable string
	StateDir   string
	ConfigDir  string
}

func (i Installer) Install(ctx context.Context, replace bool) error {
	if err := i.validate(ctx); err != nil {
		return err
	}
	doc, err := readDocument(i.settingsPath())
	if err != nil {
		return err
	}
	installed, err := statusEntry(doc.fields["statusLine"], i.launcher())
	if err != nil {
		return err
	}
	backup, err := i.installBackup(doc, installed, replace)
	if err != nil {
		return err
	}
	return i.applyInstall(ctx, doc, backup)
}

func (i Installer) Restore(ctx context.Context) error {
	if err := i.validate(ctx); err != nil {
		return err
	}
	backup, err := i.readBackup()
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	doc, err := readDocument(i.settingsPath())
	if err != nil {
		return err
	}
	if matchesEntry(doc, backup.HadEntry, backup.Original) {
		return os.Remove(i.backupPath())
	}
	if !backup.owns(doc) {
		return ErrOwnership
	}
	return i.restoreEntry(ctx, doc, backup)
}

func (i Installer) applyInstall(ctx context.Context, doc document, backup journal) error {
	if err := ensureDirectory(i.StateDir); err != nil {
		return err
	}
	if err := i.saveBackup(backup); err != nil {
		return err
	}
	doc.fields["statusLine"] = backup.Installed
	if err := ensureDirectory(i.ConfigDir); err != nil {
		return err
	}
	if err := writeDocument(ctx, i.settingsPath(), doc); err != nil {
		return err
	}
	if len(backup.PreviousInstalled) == 0 {
		return nil
	}
	backup.PreviousInstalled = nil
	return i.saveBackup(backup)
}

func (i Installer) saveBackup(backup journal) error {
	data, err := json.Marshal(backup)
	if err != nil || len(data) > 2*maxSettings {
		return errors.New("could not prepare Claude status line backup")
	}
	return localfile.Write(i.backupPath(), data, 0600)
}

func (i Installer) restoreEntry(ctx context.Context, doc document, backup journal) error {
	delete(doc.fields, "statusLine")
	if backup.HadEntry {
		doc.fields["statusLine"] = backup.Original
	}
	if err := writeDocument(ctx, i.settingsPath(), doc); err != nil {
		return err
	}
	return os.Remove(i.backupPath())
}

func (i Installer) validate(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	for _, path := range []string{i.Executable, i.StateDir, i.ConfigDir} {
		if !filepath.IsAbs(path) {
			return errors.New("Claude setup requires absolute executable, state and configuration paths")
		}
	}
	for _, path := range []string{i.StateDir, i.ConfigDir} {
		if err := checkDirectory(path); err != nil {
			return err
		}
	}
	return nil
}

func (i Installer) settingsPath() string { return filepath.Join(i.ConfigDir, "settings.json") }
func (i Installer) backupPath() string   { return filepath.Join(i.StateDir, "claude-statusline.json") }
