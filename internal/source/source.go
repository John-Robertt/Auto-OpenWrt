package source

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/John-Robertt/Auto-OpenWrt/internal/config"
	"github.com/John-Robertt/Auto-OpenWrt/internal/workspace"
)

const SchemaVersion = 1

type Manager struct {
	Store  workspace.Store
	Git    GitRunner
	Docker DockerRunner
	Now    func() time.Time
}

type GitRunner interface {
	Run(ctx context.Context, dir string, args ...string) GitResult
}

type GitResult struct {
	Stdout   string
	Stderr   string
	ExitCode int
	Err      error
}

type UpdateResult struct {
	SummaryPath  string
	Snapshots    []SourceSetSnapshot
	SourceSetIDs []string
}

type UpdateSummary struct {
	SchemaVersion int                 `json:"schema_version"`
	WorkspaceID   string              `json:"workspace_id"`
	BuildID       *string             `json:"build_id,omitempty"`
	RunID         string              `json:"run_id"`
	SourceSetIDs  []string            `json:"source_set_ids"`
	Snapshots     []SourceSetSnapshot `json:"snapshots"`
	UpdatedAt     string              `json:"updated_at"`
}

type SourceSetSnapshot struct {
	SchemaVersion int                  `json:"schema_version"`
	SourceSetID   string               `json:"source_set_id"`
	BuildIDs      []string             `json:"build_ids"`
	UpdatedAt     string               `json:"updated_at"`
	OpenWrt       RepositorySnapshot   `json:"openwrt"`
	Feeds         []RepositorySnapshot `json:"feeds"`
	Plugins       []PluginSnapshot     `json:"plugins"`
}

type RepositorySnapshot struct {
	Name       string `json:"name"`
	Repo       string `json:"repo"`
	Branch     string `json:"branch"`
	Path       string `json:"path,omitempty"`
	Commit     string `json:"commit"`
	CachePath  string `json:"cache_path"`
	DirtyState bool   `json:"dirty_state"`
}

type PluginSnapshot struct {
	Name       string `json:"name"`
	Type       string `json:"type"`
	Repo       string `json:"repo"`
	Branch     string `json:"branch"`
	Path       string `json:"path"`
	Commit     string `json:"commit"`
	CachePath  string `json:"cache_path"`
	DirtyState bool   `json:"dirty_state"`
	Risk       string `json:"risk"`
}

type RepositoryError struct {
	Name     string
	Repo     string
	Command  []string
	ExitCode int
	Stderr   string
	Err      error
}

func (e *RepositoryError) Error() string {
	return fmt.Sprintf("update repository %s failed: %v", e.Name, e.Err)
}

func (m Manager) Update(ctx context.Context, input UpdateInput) (*UpdateResult, error) {
	if m.Git == nil {
		m.Git = ExecGitRunner{}
	}
	if m.Now == nil {
		m.Now = func() time.Time { return time.Now().UTC() }
	}
	updatedAt := m.Now().UTC().Format(time.RFC3339)

	snapshots := make([]SourceSetSnapshot, 0, len(input.Plans))
	for _, plan := range input.Plans {
		snapshot, err := m.updateOne(ctx, plan, updatedAt)
		if err != nil {
			return nil, err
		}
		snapshots = append(snapshots, snapshot)
	}
	sort.Slice(snapshots, func(i, j int) bool { return snapshots[i].SourceSetID < snapshots[j].SourceSetID })

	sourceSetIDs := make([]string, 0, len(snapshots))
	for _, snapshot := range snapshots {
		sourceSetIDs = append(sourceSetIDs, snapshot.SourceSetID)
	}
	summary := UpdateSummary{
		SchemaVersion: SchemaVersion,
		WorkspaceID:   input.WorkspaceID,
		BuildID:       input.BuildID,
		RunID:         input.RunID,
		SourceSetIDs:  sourceSetIDs,
		Snapshots:     snapshots,
		UpdatedAt:     updatedAt,
	}
	summaryPath := filepath.Join(input.RunDir, "source-update-summary.json")
	if err := writeJSON(summaryPath, summary); err != nil {
		return nil, err
	}
	return &UpdateResult{
		SummaryPath:  summaryPath,
		Snapshots:    snapshots,
		SourceSetIDs: sourceSetIDs,
	}, nil
}

type UpdateInput struct {
	WorkspaceID string
	BuildID     *string
	RunID       string
	RunDir      string
	Plans       []config.SourceSetPlan
}

func (m Manager) updateOne(ctx context.Context, plan config.SourceSetPlan, updatedAt string) (SourceSetSnapshot, error) {
	root := filepath.Join(m.Store.Root, "sources", "source-sets", plan.SourceSetID)
	openwrtPath := filepath.Join(root, "openwrt")
	openwrt, err := m.updateRepository(ctx, repositoryRequest{
		Name:      "openwrt",
		Repo:      plan.OpenWrt.Repo,
		Branch:    plan.OpenWrt.Branch,
		CachePath: openwrtPath,
	})
	if err != nil {
		return SourceSetSnapshot{}, err
	}

	feeds := make([]RepositorySnapshot, 0, len(plan.Feeds))
	for _, feed := range plan.Feeds {
		snapshot, err := m.updateRepository(ctx, repositoryRequest{
			Name:      feed.Name,
			Repo:      feed.Repo,
			Branch:    feed.Branch,
			Path:      feed.Path,
			CachePath: filepath.Join(root, "feeds", feed.Name),
		})
		if err != nil {
			return SourceSetSnapshot{}, err
		}
		feeds = append(feeds, snapshot)
	}

	plugins := make([]PluginSnapshot, 0, len(plan.Plugins))
	for _, plugin := range plan.Plugins {
		repoSnapshot, err := m.updateRepository(ctx, repositoryRequest{
			Name:      plugin.Name,
			Repo:      plugin.Repo,
			Branch:    plugin.Branch,
			Path:      plugin.Path,
			CachePath: filepath.Join(root, "plugins", plugin.Name),
		})
		if err != nil {
			return SourceSetSnapshot{}, err
		}
		plugins = append(plugins, PluginSnapshot{
			Name:       repoSnapshot.Name,
			Type:       plugin.Type,
			Repo:       repoSnapshot.Repo,
			Branch:     repoSnapshot.Branch,
			Path:       plugin.Path,
			Commit:     repoSnapshot.Commit,
			CachePath:  repoSnapshot.CachePath,
			DirtyState: repoSnapshot.DirtyState,
			Risk:       DetectPluginRisk(plugin, repoSnapshot.CachePath),
		})
	}

	snapshot := SourceSetSnapshot{
		SchemaVersion: SchemaVersion,
		SourceSetID:   plan.SourceSetID,
		BuildIDs:      append([]string{}, plan.BuildIDs...),
		UpdatedAt:     updatedAt,
		OpenWrt:       openwrt,
		Feeds:         feeds,
		Plugins:       plugins,
	}
	sort.Strings(snapshot.BuildIDs)
	if err := writeJSON(filepath.Join(root, "source-set.json"), snapshot); err != nil {
		return SourceSetSnapshot{}, err
	}
	return snapshot, nil
}

type repositoryRequest struct {
	Name      string
	Repo      string
	Branch    string
	Path      string
	CachePath string
}

func (m Manager) updateRepository(ctx context.Context, request repositoryRequest) (RepositorySnapshot, error) {
	if _, err := os.Stat(request.CachePath); errors.Is(err, os.ErrNotExist) {
		if result := m.Git.Run(ctx, "", "clone", "--branch", request.Branch, request.Repo, request.CachePath); !result.Success() {
			return RepositorySnapshot{}, repositoryError(request, result, []string{"git", "clone", "--branch", request.Branch, request.Repo, request.CachePath})
		}
	} else if err != nil {
		return RepositorySnapshot{}, err
	} else {
		commands := [][]string{
			{"fetch", "origin", request.Branch},
			{"checkout", request.Branch},
			{"reset", "--hard", "origin/" + request.Branch},
		}
		for _, args := range commands {
			if result := m.Git.Run(ctx, request.CachePath, args...); !result.Success() {
				return RepositorySnapshot{}, repositoryError(request, result, append([]string{"git"}, args...))
			}
		}
	}
	if result := m.Git.Run(ctx, request.CachePath, "clean", "-fdx"); !result.Success() {
		return RepositorySnapshot{}, repositoryError(request, result, []string{"git", "clean", "-fdx"})
	}
	head := m.Git.Run(ctx, request.CachePath, "rev-parse", "HEAD")
	if !head.Success() {
		return RepositorySnapshot{}, repositoryError(request, head, []string{"git", "rev-parse", "HEAD"})
	}
	return RepositorySnapshot{
		Name:       request.Name,
		Repo:       request.Repo,
		Branch:     request.Branch,
		Path:       request.Path,
		Commit:     strings.TrimSpace(head.Stdout),
		CachePath:  request.CachePath,
		DirtyState: false,
	}, nil
}

func DetectPluginRisk(plugin config.PluginRepository, cachePath string) string {
	if plugin.Risk != "" {
		return plugin.Risk
	}
	if plugin.Type == "patch" {
		return "patch"
	}
	if strings.Contains(plugin.Name, "luci-app-") || strings.Contains(plugin.Path, "luci-app-") {
		return "luci-app"
	}
	makefile := filepath.Join(cachePath, filepath.FromSlash(plugin.Path), "Makefile")
	data, err := os.ReadFile(makefile)
	if err == nil && bytes.Contains(data, []byte("KernelPackage/")) {
		return "kernel-module"
	}
	return "unknown"
}

type ExecGitRunner struct{}

func (ExecGitRunner) Run(ctx context.Context, dir string, args ...string) GitResult {
	cmd := exec.CommandContext(ctx, "git", args...)
	if dir != "" {
		cmd.Dir = dir
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	result := GitResult{Stdout: stdout.String(), Stderr: stderr.String(), Err: err}
	if err == nil {
		return result
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		result.ExitCode = exitErr.ExitCode()
	} else {
		result.ExitCode = -1
	}
	return result
}

func (r GitResult) Success() bool {
	return r.Err == nil
}

func repositoryError(request repositoryRequest, result GitResult, command []string) *RepositoryError {
	return &RepositoryError{
		Name:     request.Name,
		Repo:     request.Repo,
		Command:  command,
		ExitCode: result.ExitCode,
		Stderr:   strings.TrimSpace(result.Stderr),
		Err:      result.Err,
	}
}

func writeJSON(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return workspace.AtomicWriteFile(path, data, 0o644)
}
