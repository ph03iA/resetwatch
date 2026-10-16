package main

import (
	"errors"
	"io"
	"os"
	"strings"
)

const maxEnvSize = 64 * 1024

var errEnvSyntax = errors.New("invalid RESEND_API_KEY syntax in .env")

func (a *application) resendAPIKey() (string, error) {
	if key := strings.TrimSpace(a.getenv("RESEND_API_KEY")); key != "" {
		return key, nil
	}
	key, err := loadResendEnv()
	if err != nil {
		return "", err
	}
	if key == "" {
		return "", errors.New("RESEND_API_KEY is missing; set it in the environment or in .env in the current directory")
	}
	return key, nil
}

func loadResendEnv() (string, error) {
	file, err := openDotEnv()
	if err != nil || file == nil {
		return "", err
	}
	defer file.Close()
	return readResendEnv(file)
}

func openDotEnv() (*os.File, error) {
	info, err := os.Lstat(".env")
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, errors.New("could not inspect .env")
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New(".env must be a regular file")
	}
	file, err := os.Open(".env")
	if err != nil {
		return nil, errors.New("could not open .env")
	}
	return file, nil
}

func readResendEnv(reader io.Reader) (string, error) {
	content, err := io.ReadAll(io.LimitReader(reader, maxEnvSize+1))
	if err != nil {
		return "", errors.New("could not read .env")
	}
	if len(content) > maxEnvSize {
		return "", errors.New(".env exceeds the 64 KiB limit")
	}
	return parseResendEnv(string(content))
}

func parseResendEnv(content string) (string, error) {
	var key string
	found := false
	for _, line := range strings.Split(strings.TrimPrefix(content, "\ufeff"), "\n") {
		name, value, assigned := strings.Cut(strings.TrimSpace(line), "=")
		if strings.TrimSpace(name) != "RESEND_API_KEY" {
			continue
		}
		if !assigned {
			return "", errEnvSyntax
		}
		if found {
			return "", errors.New("duplicate RESEND_API_KEY in .env")
		}
		var err error
		key, err = dotenvValue(value)
		if err != nil {
			return "", err
		}
		found = true
	}
	return key, nil
}

func dotenvValue(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", nil
	}
	if value[0] == '\'' || value[0] == '"' {
		return quotedDotenvValue(value)
	}
	value, _, _ = strings.Cut(value, "#")
	if strings.ContainsAny(value, "'\"") {
		return "", errEnvSyntax
	}
	return strings.TrimSpace(value), nil
}

func quotedDotenvValue(value string) (string, error) {
	end := strings.IndexByte(value[1:], value[0])
	if end < 0 {
		return "", errEnvSyntax
	}
	suffix := strings.TrimSpace(value[end+2:])
	if suffix != "" && !strings.HasPrefix(suffix, "#") {
		return "", errEnvSyntax
	}
	return strings.TrimSpace(value[1 : end+1]), nil
}
