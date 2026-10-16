package main

import (
	"bytes"
	"errors"
	"io"
	"os"
	"strings"
	"unicode"

	"resetwatch/internal/localfile"
)

type envSnapshot struct {
	content []byte
	info    os.FileInfo
}

func saveResendEnv(key string) error {
	encoded, err := encodeEnvKey(key)
	if err != nil {
		return err
	}
	snapshot, err := readEnvSnapshot(".env")
	if err != nil {
		return err
	}
	content, err := updateResendEnv(string(snapshot.content), encoded)
	if err != nil {
		return err
	}
	if err := requireEnvUnchanged(".env", snapshot); err != nil {
		return err
	}
	if err := localfile.Write(".env", []byte(content), 0600); err != nil {
		return errors.New("could not safely save .env; check file permissions and retry setup")
	}
	return nil
}

func encodeEnvKey(key string) (string, error) {
	if err := validateResendEnvKey(key); err != nil {
		return "", err
	}
	if !strings.ContainsRune(key, '\'') {
		return "'" + key + "'", nil
	}
	return "\"" + key + "\"", nil
}

func validateResendEnvKey(key string) error {
	if key == "" || len(key) > maxSetupInput || strings.ContainsFunc(key, unicode.IsSpace) || strings.ContainsFunc(key, unicode.IsControl) {
		return errors.New("a valid RESEND_API_KEY is required")
	}
	if strings.ContainsRune(key, '\'') && strings.ContainsRune(key, '"') {
		return errors.New("RESEND_API_KEY cannot contain both quote characters")
	}
	return nil
}

func readEnvSnapshot(path string) (envSnapshot, error) {
	info, err := inspectEnvForWrite(path)
	if err != nil || info == nil {
		return envSnapshot{}, err
	}
	file, err := os.Open(path)
	if err != nil {
		return envSnapshot{}, errors.New("could not open .env")
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil || !os.SameFile(info, opened) {
		return envSnapshot{}, errors.New(".env changed while opening it; retry setup")
	}
	content, err := io.ReadAll(io.LimitReader(file, maxEnvSize+1))
	if err != nil {
		return envSnapshot{}, errors.New("could not read .env")
	}
	if len(content) > maxEnvSize {
		return envSnapshot{}, errors.New(".env exceeds the 64 KiB limit")
	}
	return envSnapshot{content: content, info: opened}, nil
}

func inspectEnvForWrite(path string) (os.FileInfo, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, errors.New("could not inspect .env")
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New(".env must be a regular file")
	}
	if info.Mode().Perm()&0222 == 0 {
		return nil, errors.New(".env is read-only")
	}
	return info, nil
}

func requireEnvUnchanged(path string, previous envSnapshot) error {
	current, err := readEnvSnapshot(path)
	if err != nil {
		return err
	}
	if !sameEnvSnapshot(previous, current) {
		return errors.New(".env changed during setup; retry without overwriting it")
	}
	return nil
}

func sameEnvSnapshot(previous, current envSnapshot) bool {
	if previous.info == nil || current.info == nil {
		return previous.info == nil && current.info == nil
	}
	return os.SameFile(previous.info, current.info) && bytes.Equal(previous.content, current.content)
}

func updateResendEnv(content, encoded string) (string, error) {
	if _, err := parseResendEnv(content); err != nil {
		return "", err
	}
	lines := strings.SplitAfter(content, "\n")
	for index, line := range lines {
		name, _, _ := strings.Cut(strings.TrimSpace(strings.TrimPrefix(line, "\ufeff")), "=")
		if strings.TrimSpace(name) == "RESEND_API_KEY" {
			lines[index] = replaceEnvAssignment(line, encoded)
			return boundedEnvContent(strings.Join(lines, ""))
		}
	}
	newline := "\n"
	if strings.Contains(content, "\r\n") {
		newline = "\r\n"
	}
	if content != "" && !strings.HasSuffix(content, "\n") {
		content += newline
	}
	content += "RESEND_API_KEY=" + encoded + newline
	return boundedEnvContent(content)
}

func boundedEnvContent(content string) (string, error) {
	if len(content) > maxEnvSize {
		return "", errors.New("updated .env would exceed the 64 KiB limit")
	}
	return content, nil
}

func replaceEnvAssignment(line, encoded string) string {
	ending := ""
	if strings.HasSuffix(line, "\n") {
		ending = "\n"
		line = strings.TrimSuffix(line, "\n")
	}
	if strings.HasSuffix(line, "\r") {
		ending = "\r" + ending
		line = strings.TrimSuffix(line, "\r")
	}
	name, value, _ := strings.Cut(line, "=")
	return name + "=" + encoded + envAssignmentComment(value) + ending
}

func envAssignmentComment(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if value[0] == '\'' || value[0] == '"' {
		end := strings.IndexByte(value[1:], value[0])
		value = value[end+2:]
	}
	_, comment, found := strings.Cut(value, "#")
	if found {
		return " #" + comment
	}
	return ""
}
