package claudeconfig

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"reflect"

	"resetwatch/internal/localfile"
)

const maxSettings = 1 << 20

type document struct {
	fields map[string]json.RawMessage
	data   []byte
	exists bool
	mode   fs.FileMode
}

func readDocument(path string) (document, error) {
	data, mode, err := readRegular(path, maxSettings)
	if errors.Is(err, os.ErrNotExist) {
		return document{fields: make(map[string]json.RawMessage), mode: 0600}, nil
	}
	if err != nil {
		return document{}, err
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(data, &fields) != nil || fields == nil {
		return document{}, errors.New("Claude settings must contain a valid JSON object")
	}
	return document{fields: fields, data: data, exists: true, mode: mode}, nil
}

func readRegular(path string, limit int64) ([]byte, fs.FileMode, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, 0, err
	}
	if !info.Mode().IsRegular() {
		return nil, 0, errors.New("Claude configuration and backup files must be regular files")
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, 0, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil || int64(len(data)) > limit {
		return nil, 0, errors.New("Claude configuration or backup is unreadable or exceeds its size limit")
	}
	return data, info.Mode().Perm(), nil
}

func writeDocument(ctx context.Context, path string, doc document) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	data, err := json.Marshal(doc.fields)
	if err != nil || len(data)+1 > maxSettings {
		return errors.New("updated Claude settings exceed the supported JSON size")
	}
	current, err := readDocument(path)
	if err != nil {
		return err
	}
	if current.exists != doc.exists || current.mode != doc.mode || !bytes.Equal(current.data, doc.data) {
		return errors.New("Claude settings changed during setup; retry without overwriting the new settings")
	}
	return localfile.Write(path, append(data, '\n'), doc.mode)
}

func ensureDirectory(path string) error {
	if err := os.MkdirAll(path, 0700); err != nil {
		return err
	}
	return checkDirectory(path)
}

func checkDirectory(path string) error {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("Claude setup directories must be real directories")
	}
	return nil
}

func matchesEntry(doc document, had bool, value json.RawMessage) bool {
	current, present := doc.fields["statusLine"]
	if present != had {
		return false
	}
	if !present {
		return true
	}
	var left, right any
	if json.Unmarshal(current, &left) != nil || json.Unmarshal(value, &right) != nil {
		return false
	}
	return reflect.DeepEqual(left, right)
}

func statusEntry(previous json.RawMessage, command string) (json.RawMessage, error) {
	var fields map[string]json.RawMessage
	if json.Unmarshal(previous, &fields) != nil || fields == nil {
		fields = make(map[string]json.RawMessage)
	}
	encoded, err := json.Marshal(command)
	if err != nil {
		return nil, errors.New("could not prepare Claude launcher")
	}
	fields["type"], fields["command"] = json.RawMessage(`"command"`), encoded
	return json.Marshal(fields)
}
