// Package claudeconfig restores Claude Code status lines that older ResetWatch versions replaced.
package claudeconfig

import (
	"context"
	"errors"
	"os"
	"path/filepath"
)

var ErrOwnership = errors.New("Claude status line was changed outside ResetWatch; its settings and backup were preserved")

type Installer struct {
	StateDir  string
	ConfigDir string
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
	for _, path := range []string{i.StateDir, i.ConfigDir} {
		if !filepath.IsAbs(path) {
			return errors.New("Claude restore requires absolute state and configuration paths")
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
