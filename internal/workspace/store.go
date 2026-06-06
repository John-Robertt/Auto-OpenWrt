package workspace

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

const (
	ConfigPath = "configs/auto-openwrt.yaml"
)

var RequiredDirs = []string{
	"configs",
	"sources/source-sets",
	"cache/downloads",
	"cache/build",
	"runs/doctor",
	"workspaces",
}

var WorkspaceStateDirs = []string{
	"config/resolved",
	"worktrees",
	"runs/doctor",
	"runs/update",
	"artifacts/.staging",
	"diagnostics",
	"checkpoints",
	"patches/adopted",
	"locks",
}

type Store struct {
	Root string
}

type InitResult struct {
	Root       string
	ConfigPath string
	Dirs       []string
}

type ConfigExistsError struct {
	Path string
}

func (e *ConfigExistsError) Error() string {
	return fmt.Sprintf("config already exists: %s", e.Path)
}

type WriteError struct {
	Path string
	Err  error
}

func (e *WriteError) Error() string {
	return fmt.Sprintf("write %s: %v", e.Path, e.Err)
}

func (e *WriteError) Unwrap() error {
	return e.Err
}

func New(root string) (Store, error) {
	if root == "" {
		root = "."
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return Store{}, err
	}
	return Store{Root: filepath.Clean(absRoot)}, nil
}

func (s Store) Init(sampleConfig []byte, workspaceID string, force bool) (InitResult, error) {
	if err := s.createRequiredDirs(); err != nil {
		return InitResult{}, err
	}
	if workspaceID != "" {
		if err := s.createWorkspaceDirs(workspaceID); err != nil {
			return InitResult{}, err
		}
	}

	configPath := s.Abs(ConfigPath)
	if _, err := os.Stat(configPath); err == nil && !force {
		return InitResult{}, &ConfigExistsError{Path: configPath}
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return InitResult{}, &WriteError{Path: configPath, Err: err}
	}

	if err := AtomicWriteFile(configPath, sampleConfig, 0o644); err != nil {
		return InitResult{}, &WriteError{Path: configPath, Err: err}
	}

	dirs := make([]string, 0, len(RequiredDirs))
	for _, rel := range RequiredDirs {
		dirs = append(dirs, s.Abs(rel))
	}
	if workspaceID != "" {
		for _, rel := range WorkspaceStateDirs {
			dirs = append(dirs, s.Abs(filepath.ToSlash(filepath.Join("workspaces", workspaceID, rel))))
		}
	}
	return InitResult{
		Root:       s.Root,
		ConfigPath: configPath,
		Dirs:       dirs,
	}, nil
}

func (s Store) Abs(rel string) string {
	return filepath.Join(s.Root, filepath.FromSlash(rel))
}

func (s Store) createRequiredDirs() error {
	for _, rel := range RequiredDirs {
		path := s.Abs(rel)
		if err := os.MkdirAll(path, 0o755); err != nil {
			return &WriteError{Path: path, Err: err}
		}
	}
	return nil
}

func (s Store) createWorkspaceDirs(workspaceID string) error {
	for _, rel := range WorkspaceStateDirs {
		path := s.Abs(filepath.ToSlash(filepath.Join("workspaces", workspaceID, rel)))
		if err := os.MkdirAll(path, 0o755); err != nil {
			return &WriteError{Path: path, Err: err}
		}
	}
	return nil
}

func AtomicWriteFile(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	temp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tempPath := temp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tempPath)
		}
	}()

	if _, err := temp.Write(data); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Chmod(perm); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tempPath, path); err != nil {
		return err
	}
	cleanup = false
	return nil
}
