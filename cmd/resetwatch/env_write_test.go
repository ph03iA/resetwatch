package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestSaveResendEnvCreatesPrivateFile(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := saveResendEnv("re_fixture_secret"); err != nil {
		t.Fatal(err)
	}
	key, err := loadResendEnv()
	if err != nil || key != "re_fixture_secret" {
		t.Fatalf("saved key did not load: %v", err)
	}
	info, err := os.Stat(".env")
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm()&0077 != 0 {
		t.Fatalf("saved .env is not private: %v", err)
	}
}

func TestSaveResendEnvPreservesContent(t *testing.T) {
	cases := []struct{ name, content, want string }{
		{"append", "# keep\nOTHER='unchanged'\n", "# keep\nOTHER='unchanged'\nRESEND_API_KEY='re_fixture_new'\n"},
		{"no final newline", "OTHER=keep", "OTHER=keep\nRESEND_API_KEY='re_fixture_new'\n"},
		{"blank assignment", "# keep\n  RESEND_API_KEY = # personal\nOTHER=keep\n", "# keep\n  RESEND_API_KEY ='re_fixture_new' # personal\nOTHER=keep\n"},
		{"BOM CRLF", "\ufeff# keep\r\nRESEND_API_KEY='old#literal' # keep comment\r\nOTHER=keep\r\n", "\ufeff# keep\r\nRESEND_API_KEY='re_fixture_new' # keep comment\r\nOTHER=keep\r\n"},
		{"BOM first assignment", "\ufeffRESEND_API_KEY=\r\nOTHER=keep\r\n", "\ufeffRESEND_API_KEY='re_fixture_new'\r\nOTHER=keep\r\n"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			writeEnvFixture(t, test.content)
			if err := saveResendEnv("re_fixture_new"); err != nil {
				t.Fatal(err)
			}
			content, err := os.ReadFile(".env")
			if err != nil || string(content) != test.want {
				t.Fatalf("unrelated contents changed: %v", err)
			}
		})
	}
}

func TestSaveResendEnvRejectsInvalidContentWithoutChanges(t *testing.T) {
	cases := []string{
		"RESEND_API_KEY='fixture_secret\n",
		"RESEND_API_KEY=\nRESEND_API_KEY=fixture_secret\n",
		"RESEND_API_KEY\n",
		strings.Repeat("x", maxEnvSize+1),
	}
	for _, content := range cases {
		writeEnvFixture(t, content)
		err := saveResendEnv("re_new_fixture_secret")
		if err == nil || strings.Contains(err.Error(), "fixture_secret") {
			t.Fatal("invalid .env accepted or leaked")
		}
		after, readErr := os.ReadFile(".env")
		if readErr != nil || string(after) != content {
			t.Fatal("failed save changed .env")
		}
	}
}

func TestSaveResendEnvRejectsInvalidKeys(t *testing.T) {
	for _, key := range []string{"", "fixture_secret with spaces", "fixture_secret\nInjected", "fixture_secret\x00", "fixture_secret'\"", strings.Repeat("x", maxSetupInput+1)} {
		t.Chdir(t.TempDir())
		err := saveResendEnv(key)
		if err == nil || strings.Contains(err.Error(), "fixture_secret") {
			t.Fatal("invalid key accepted or leaked")
		}
		if _, err := os.Stat(".env"); !os.IsNotExist(err) {
			t.Fatal("invalid key created .env")
		}
	}
}

func TestSaveResendEnvQuotesLiteralCharacters(t *testing.T) {
	for _, key := range []string{"re_fixture#literal", "re_fixture'quote", "re_fixture\"quote", "re_fixture${OTHER}\\tail=ok"} {
		t.Chdir(t.TempDir())
		if err := saveResendEnv(key); err != nil {
			t.Fatal(err)
		}
		loaded, err := loadResendEnv()
		if err != nil || loaded != key {
			t.Fatalf("literal key did not round-trip: %v", err)
		}
	}
}

func TestSaveResendEnvRejectsReadOnlyAndNonRegular(t *testing.T) {
	writeEnvFixture(t, "# keep\n")
	readonly, err := filepath.Abs(".env")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(".env", 0400); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(readonly, 0600) })
	if err := saveResendEnv("re_fixture_secret"); err == nil {
		t.Fatal("read-only .env accepted")
	}
	t.Chdir(t.TempDir())
	if err := os.Mkdir(".env", 0700); err != nil {
		t.Fatal(err)
	}
	if err := saveResendEnv("re_fixture_secret"); err == nil {
		t.Fatal("directory .env accepted")
	}
}

func TestEnvSnapshotDetectsConcurrentChanges(t *testing.T) {
	writeEnvFixture(t, "OTHER=original\n")
	snapshot, err := readEnvSnapshot(".env")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(".env", []byte("OTHER=changed\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := requireEnvUnchanged(".env", snapshot); err == nil {
		t.Fatal("concurrent edit was accepted")
	}
	if err := requireEnvUnchanged(".env", envSnapshot{}); err == nil {
		t.Fatal("concurrent file creation was accepted")
	}
}

func TestSaveResendEnvRejectsSymlink(t *testing.T) {
	t.Chdir(t.TempDir())
	target := filepath.Join(t.TempDir(), "fixture.env")
	if err := os.WriteFile(target, []byte("OTHER=keep\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, ".env"); err != nil {
		t.Skip("symlink creation is unavailable")
	}
	if err := saveResendEnv("re_fixture_secret"); err == nil {
		t.Fatal("symlink .env accepted")
	}
	content, err := os.ReadFile(target)
	if err != nil || string(content) != "OTHER=keep\n" {
		t.Fatal("symlink target changed")
	}
}

func TestUpdateResendEnvRejectsGrowthBeyondLimit(t *testing.T) {
	const prefix = "RESEND_API_KEY=\n#"
	content := prefix + strings.Repeat("x", maxEnvSize-len(prefix))
	if _, err := updateResendEnv(content, "'re_fixture'"); err == nil {
		t.Fatal("replaced assignment exceeded .env bound")
	}
}
