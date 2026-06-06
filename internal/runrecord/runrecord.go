package runrecord

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"math/big"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/John-Robertt/Auto-OpenWrt/internal/workspace"
)

const (
	SchemaVersion = 1

	StatusPending   = "pending"
	StatusRunning   = "running"
	StatusSucceeded = "succeeded"
	StatusFailed    = "failed"
	StatusSkipped   = "skipped"

	FinalSucceeded = "succeeded"
	FinalFailed    = "failed"
	FinalBlocked   = "blocked"
)

type ErrorObject struct {
	Code       string         `json:"code"`
	Message    string         `json:"message"`
	Suggestion string         `json:"suggestion"`
	Details    map[string]any `json:"details"`
}

type Stage struct {
	ID          string       `json:"id"`
	Name        string       `json:"name"`
	Status      string       `json:"status"`
	StartedAt   string       `json:"started_at,omitempty"`
	EndedAt     string       `json:"ended_at,omitempty"`
	ResultPaths []string     `json:"result_paths,omitempty"`
	Error       *ErrorObject `json:"error,omitempty"`
	Suggestion  string       `json:"suggestion,omitempty"`
}

type FinalStatus struct {
	Status string       `json:"status"`
	Reason string       `json:"reason,omitempty"`
	At     string       `json:"at"`
	Error  *ErrorObject `json:"error,omitempty"`
}

type RunRecord struct {
	SchemaVersion int               `json:"schema_version"`
	RunID         string            `json:"run_id"`
	Command       string            `json:"command"`
	ProjectRoot   string            `json:"project_root"`
	WorkspaceID   *string           `json:"workspace_id"`
	SourceSetID   *string           `json:"source_set_id"`
	BuildID       *string           `json:"build_id"`
	StartedAt     string            `json:"started_at"`
	EndedAt       string            `json:"ended_at,omitempty"`
	Stages        []Stage           `json:"stages"`
	Paths         map[string]string `json:"paths"`
	FinalStatus   *FinalStatus      `json:"final_status,omitempty"`
	Error         *ErrorObject      `json:"error,omitempty"`
}

type RunLock struct {
	SchemaVersion int    `json:"schema_version"`
	RunID         string `json:"run_id"`
	Command       string `json:"command"`
	PID           int    `json:"pid"`
	StartedAt     string `json:"started_at"`
}

type Store struct {
	Workspace workspace.Store
}

type CreateInput struct {
	Command     string
	RunID       string
	ProjectRoot string
	WorkspaceID *string
	SourceSetID *string
	BuildID     *string
	RelDir      string
	Now         time.Time
}

type FinalStatusExistsError struct {
	Path string
}

func (e *FinalStatusExistsError) Error() string {
	return "run final status already exists: " + e.Path
}

func NewStore(store workspace.Store) Store {
	return Store{Workspace: store}
}

func DoctorRunRelDir(workspaceID *string, runID string) string {
	if workspaceID == nil || *workspaceID == "" {
		return filepath.ToSlash(filepath.Join("runs", "doctor", runID))
	}
	return filepath.ToSlash(filepath.Join("workspaces", *workspaceID, "runs", "doctor", runID))
}

func BuildRunRelDir(workspaceID, buildID, runID string) string {
	return filepath.ToSlash(filepath.Join("workspaces", workspaceID, "runs", buildID, runID))
}

func GenerateRunID(root string) (string, error) {
	for attempts := 0; attempts < 32; attempts++ {
		runID, err := randomRunID(time.Now().UTC())
		if err != nil {
			return "", err
		}
		exists, err := RunIDExists(root, runID)
		if err != nil {
			return "", err
		}
		if !exists {
			return runID, nil
		}
	}
	return "", fmt.Errorf("could not generate unique run id")
}

func RunIDExists(root, runID string) (bool, error) {
	found := false
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return nil
			}
			return err
		}
		if !entry.IsDir() {
			return nil
		}
		if entry.Name() == runID {
			found = true
			return fs.SkipAll
		}
		return nil
	})
	return found, err
}

func (s Store) Create(input CreateInput) (RunRecord, string, error) {
	if input.Now.IsZero() {
		input.Now = time.Now().UTC()
	}
	runDir := s.Workspace.Abs(input.RelDir)
	if err := os.MkdirAll(runDir, 0o755); err != nil {
		return RunRecord{}, "", err
	}

	now := formatTime(input.Now)
	record := RunRecord{
		SchemaVersion: SchemaVersion,
		RunID:         input.RunID,
		Command:       input.Command,
		ProjectRoot:   input.ProjectRoot,
		WorkspaceID:   input.WorkspaceID,
		SourceSetID:   input.SourceSetID,
		BuildID:       input.BuildID,
		StartedAt:     now,
		Stages: []Stage{{
			ID:        "run.create",
			Name:      "创建 run record",
			Status:    StatusSucceeded,
			StartedAt: now,
			EndedAt:   now,
		}},
		Paths: map[string]string{
			"run_record": filepath.Join(runDir, "run.json"),
		},
	}
	if err := writeJSON(filepath.Join(runDir, "run.json"), record, 0o644); err != nil {
		return RunRecord{}, "", err
	}
	lock := RunLock{
		SchemaVersion: SchemaVersion,
		RunID:         input.RunID,
		Command:       input.Command,
		PID:           os.Getpid(),
		StartedAt:     now,
	}
	if err := writeJSON(filepath.Join(runDir, "run.lock"), lock, 0o644); err != nil {
		return RunRecord{}, "", err
	}
	return record, runDir, nil
}

func (s Store) StartStage(runDir, id, name string, now time.Time) error {
	return s.Update(runDir, func(record *RunRecord) error {
		stage := findStage(record.Stages, id)
		value := Stage{
			ID:        id,
			Name:      name,
			Status:    StatusRunning,
			StartedAt: formatTime(defaultNow(now)),
		}
		if stage >= 0 {
			record.Stages[stage] = mergeStage(record.Stages[stage], value)
			return nil
		}
		record.Stages = append(record.Stages, value)
		return nil
	})
}

func (s Store) FinishStage(runDir, id, status string, resultPaths []string, errObj *ErrorObject, suggestion string, now time.Time) error {
	return s.Update(runDir, func(record *RunRecord) error {
		stage := findStage(record.Stages, id)
		if stage < 0 {
			record.Stages = append(record.Stages, Stage{ID: id, Name: id})
			stage = len(record.Stages) - 1
		}
		record.Stages[stage].Status = status
		record.Stages[stage].EndedAt = formatTime(defaultNow(now))
		record.Stages[stage].ResultPaths = resultPaths
		record.Stages[stage].Error = errObj
		record.Stages[stage].Suggestion = suggestion
		return nil
	})
}

func (s Store) SetPath(runDir, key, value string) error {
	return s.Update(runDir, func(record *RunRecord) error {
		if record.Paths == nil {
			record.Paths = map[string]string{}
		}
		record.Paths[key] = value
		return nil
	})
}

func (s Store) Finalize(runDir, status, reason string, errObj *ErrorObject, now time.Time) error {
	return s.Update(runDir, func(record *RunRecord) error {
		if record.FinalStatus != nil {
			return &FinalStatusExistsError{Path: filepath.Join(runDir, "run.json")}
		}
		timestamp := formatTime(defaultNow(now))
		record.FinalStatus = &FinalStatus{
			Status: status,
			Reason: reason,
			At:     timestamp,
			Error:  errObj,
		}
		record.EndedAt = timestamp
		record.Error = errObj
		return nil
	})
}

func (s Store) Complete(runDir, status, reason string, errObj *ErrorObject, now time.Time) error {
	if err := s.Finalize(runDir, status, reason, errObj, now); err != nil {
		return err
	}
	lockPath := filepath.Join(runDir, "run.lock")
	if err := os.Remove(lockPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func (s Store) Update(runDir string, mutate func(*RunRecord) error) error {
	path := filepath.Join(runDir, "run.json")
	record, err := Read(path)
	if err != nil {
		return err
	}
	if err := mutate(&record); err != nil {
		return err
	}
	return writeJSON(path, record, 0o644)
}

func Read(path string) (RunRecord, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return RunRecord{}, err
	}
	var record RunRecord
	if err := json.Unmarshal(data, &record); err != nil {
		return RunRecord{}, err
	}
	return record, nil
}

type RecoverOptions struct {
	IsProcessAlive func(pid int) bool
	Now            time.Time
}

func (s Store) RecoverIncomplete(options RecoverOptions) ([]string, error) {
	isAlive := options.IsProcessAlive
	if isAlive == nil {
		isAlive = processAlive
	}
	now := defaultNow(options.Now)
	runDirs, err := discoverRunDirs(s.Workspace.Root)
	if err != nil {
		return nil, err
	}

	var recovered []string
	for _, runDir := range runDirs {
		recordPath := filepath.Join(runDir, "run.json")
		lockPath := filepath.Join(runDir, "run.lock")

		record, recordErr := Read(recordPath)
		if recordErr != nil && !errors.Is(recordErr, os.ErrNotExist) {
			return recovered, recordErr
		}
		if recordErr == nil && record.FinalStatus != nil {
			continue
		}

		lock, lockErr := readLock(lockPath)
		switch {
		case lockErr == nil && isAlive(lock.PID):
			continue
		case lockErr == nil:
			if recordErr != nil {
				continue
			}
			errObj := ErrorObject{
				Code:       "RUN_INTERRUPTED",
				Message:    "上一次运行被中断",
				Suggestion: "检查中断前的日志后重新运行命令",
				Details:    map[string]any{"reason": "interrupted", "pid": lock.PID},
			}
			if err := s.Complete(runDir, FinalBlocked, "interrupted", &errObj, now); err != nil {
				return recovered, err
			}
			recovered = append(recovered, record.RunID)
		case errors.Is(lockErr, os.ErrNotExist):
			if recordErr != nil {
				continue
			}
			errObj := ErrorObject{
				Code:       "INCOMPLETE_RUN_RECORD",
				Message:    "发现未完成的运行记录",
				Suggestion: "重新运行命令以创建新的 run",
				Details:    map[string]any{"reason": "incomplete-run-record"},
			}
			if err := s.Finalize(runDir, FinalBlocked, "incomplete-run-record", &errObj, now); err != nil {
				return recovered, err
			}
			recovered = append(recovered, record.RunID)
		default:
			return recovered, lockErr
		}
	}
	return recovered, nil
}

type LatestOptions struct {
	WorkspaceID *string
	BuildID     *string
	RunID       string
}

func (s Store) FindFinal(options LatestOptions) (RunRecord, string, error) {
	records, err := s.finalRecords(options)
	if err != nil {
		return RunRecord{}, "", err
	}
	if options.RunID != "" {
		for _, item := range records {
			if item.Record.RunID == options.RunID {
				return item.Record, item.Path, nil
			}
		}
		return RunRecord{}, "", os.ErrNotExist
	}
	if len(records) == 0 {
		return RunRecord{}, "", os.ErrNotExist
	}
	sort.Slice(records, func(i, j int) bool {
		return records[i].Record.RunID > records[j].Record.RunID
	})
	return records[0].Record, records[0].Path, nil
}

type finalRecord struct {
	Record RunRecord
	Path   string
}

func (s Store) finalRecords(options LatestOptions) ([]finalRecord, error) {
	var records []finalRecord
	err := filepath.WalkDir(s.Workspace.Root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return nil
			}
			return err
		}
		if entry.IsDir() || entry.Name() != "run.json" {
			return nil
		}
		record, err := Read(path)
		if err != nil {
			return err
		}
		if record.FinalStatus == nil {
			return nil
		}
		if options.WorkspaceID != nil {
			if record.WorkspaceID == nil || *record.WorkspaceID != *options.WorkspaceID {
				return nil
			}
		}
		if options.BuildID != nil {
			if record.BuildID == nil || *record.BuildID != *options.BuildID {
				return nil
			}
		}
		records = append(records, finalRecord{Record: record, Path: path})
		return nil
	})
	return records, err
}

func discoverRunDirs(root string) ([]string, error) {
	if _, err := os.Stat(root); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	seen := map[string]bool{}
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return nil
			}
			return err
		}
		if entry.IsDir() {
			return nil
		}
		if entry.Name() == "run.json" || entry.Name() == "run.lock" {
			seen[filepath.Dir(path)] = true
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	dirs := make([]string, 0, len(seen))
	for dir := range seen {
		dirs = append(dirs, dir)
	}
	sort.Strings(dirs)
	return dirs, nil
}

func randomRunID(now time.Time) (string, error) {
	const alphabet = "abcdefghijklmnopqrstuvwxyz0123456789"
	var suffix strings.Builder
	for i := 0; i < 6; i++ {
		n, err := rand.Int(rand.Reader, big.NewInt(int64(len(alphabet))))
		if err != nil {
			return "", err
		}
		suffix.WriteByte(alphabet[n.Int64()])
	}
	return now.UTC().Format("20060102T150405Z") + "-" + suffix.String(), nil
}

func writeJSON(path string, value any, perm os.FileMode) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return workspace.AtomicWriteFile(path, data, perm)
}

func readLock(path string) (RunLock, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return RunLock{}, err
	}
	var lock RunLock
	if err := json.Unmarshal(data, &lock); err != nil {
		return RunLock{}, err
	}
	return lock, nil
}

func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return process.Signal(syscall.Signal(0)) == nil
}

func findStage(stages []Stage, id string) int {
	for i, stage := range stages {
		if stage.ID == id {
			return i
		}
	}
	return -1
}

func mergeStage(existing, next Stage) Stage {
	if existing.Name != "" && next.Name == "" {
		next.Name = existing.Name
	}
	if existing.StartedAt != "" && next.StartedAt == "" {
		next.StartedAt = existing.StartedAt
	}
	return next
}

func defaultNow(now time.Time) time.Time {
	if now.IsZero() {
		return time.Now().UTC()
	}
	return now.UTC()
}

func formatTime(now time.Time) string {
	return now.UTC().Format(time.RFC3339)
}
