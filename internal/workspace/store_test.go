package workspace

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestInitCreatesRequiredDirsAndConfig(t *testing.T) {
	store, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	result, err := store.Init([]byte("version: 1\n"), "auto-openwrt", false)
	if err != nil {
		t.Fatal(err)
	}

	if result.Root != store.Root {
		t.Fatalf("root = %s, want %s", result.Root, store.Root)
	}
	assertFileContent(t, result.ConfigPath, "version: 1\n")
	for _, rel := range RequiredDirs {
		assertDir(t, store.Abs(rel))
	}
	for _, rel := range WorkspaceStateDirs {
		assertDir(t, store.Abs(filepath.ToSlash(filepath.Join("workspaces", "auto-openwrt", rel))))
	}
}

func TestInitExistingConfigFailsButKeepsDirectoriesIdempotent(t *testing.T) {
	store, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	configPath := store.Abs(ConfigPath)
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, []byte("existing\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err = store.Init([]byte("new\n"), "auto-openwrt", false)

	var exists *ConfigExistsError
	if !errors.As(err, &exists) {
		t.Fatalf("error = %T %[1]v, want ConfigExistsError", err)
	}
	assertFileContent(t, configPath, "existing\n")
	for _, rel := range RequiredDirs {
		assertDir(t, store.Abs(rel))
	}
}

func TestInitForceOverwritesOnlyConfig(t *testing.T) {
	store, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Init([]byte("old\n"), "auto-openwrt", false); err != nil {
		t.Fatal(err)
	}
	keepPath := store.Abs("workspaces/auto-openwrt/runs/doctor/keep.txt")
	if err := os.WriteFile(keepPath, []byte("keep\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := store.Init([]byte("new\n"), "auto-openwrt", true); err != nil {
		t.Fatal(err)
	}

	assertFileContent(t, store.Abs(ConfigPath), "new\n")
	assertFileContent(t, keepPath, "keep\n")
}

func assertDir(t *testing.T, path string) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !info.IsDir() {
		t.Fatalf("%s is not a directory", path)
	}
}

func assertFileContent(t *testing.T, path, want string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != want {
		t.Fatalf("%s content = %q, want %q", path, string(data), want)
	}
}
