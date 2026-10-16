// Package localfile replaces private configuration files without truncating their originals.
package localfile

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
)

// Write syncs a sibling temporary file before replacing a regular target.
func Write(path string, data []byte, perm fs.FileMode) error {
	if err := regularTarget(path); err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	defer file.Close()
	if err := prepare(file, data, perm); err != nil {
		return err
	}
	if err := regularTarget(path); err != nil {
		return err
	}
	return replace(file.Name(), path)
}

func prepare(file *os.File, data []byte, perm fs.FileMode) error {
	if err := file.Chmod(perm.Perm()); err != nil {
		return err
	}
	if _, err := file.Write(data); err != nil {
		return err
	}
	if err := file.Sync(); err != nil {
		return err
	}
	return file.Close()
}

func regularTarget(path string) error {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return errors.New("configuration target must be a regular file")
	}
	return nil
}
