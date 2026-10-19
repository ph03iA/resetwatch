package claudeconfig

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
)

type journal struct {
	Version           int             `json:"version"`
	SettingsPath      string          `json:"settings_path"`
	HadEntry          bool            `json:"had_entry"`
	Original          json.RawMessage `json:"original,omitempty"`
	Installed         json.RawMessage `json:"installed"`
	PreviousInstalled json.RawMessage `json:"previous_installed,omitempty"`
}

func (i Installer) readBackup() (journal, error) {
	data, _, err := readRegular(i.backupPath(), 2*maxSettings)
	if err != nil {
		return journal{}, err
	}
	var backup journal
	if json.Unmarshal(data, &backup) != nil || backup.Version != 1 {
		return journal{}, errors.New("Claude status line backup is invalid or from an unsupported version")
	}
	if filepath.Clean(backup.SettingsPath) != filepath.Clean(i.settingsPath()) {
		return journal{}, errors.New("Claude status line backup belongs to a different settings file")
	}
	if !backup.validEntries() {
		return journal{}, errors.New("Claude status line backup contains invalid entries")
	}
	return backup, nil
}

func (backup journal) validEntries() bool {
	if !validInstalledEntry(backup.Installed) || backup.HadEntry && !json.Valid(backup.Original) {
		return false
	}
	return len(backup.PreviousInstalled) == 0 || validInstalledEntry(backup.PreviousInstalled)
}

func (backup journal) owns(doc document) bool {
	if matchesEntry(doc, true, backup.Installed) {
		return true
	}
	return len(backup.PreviousInstalled) != 0 && matchesEntry(doc, true, backup.PreviousInstalled)
}

func validInstalledEntry(data json.RawMessage) bool {
	var entry struct{ Type, Command string }
	if json.Unmarshal(data, &entry) != nil {
		return false
	}
	return entry.Type == "command" && strings.TrimSpace(entry.Command) != ""
}
