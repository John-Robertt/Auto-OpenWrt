package runrecord

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/John-Robertt/Auto-OpenWrt/internal/workspace"
)

func TestCreateCompleteAndFinalStatusIsImmutable(t *testing.T) {
	store, err := workspace.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	runStore := NewStore(store)
	workspaceID := "auto-openwrt"
	runID := "20260607T010203Z-abc123"

	_, runDir, err := runStore.Create(CreateInput{
		Command:     "doctor",
		RunID:       runID,
		ProjectRoot: store.Root,
		WorkspaceID: &workspaceID,
		RelDir:      DoctorRunRelDir(&workspaceID, runID),
		Now:         fixedTime(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(runDir, "run.lock")); err != nil {
		t.Fatal(err)
	}

	if err := runStore.Complete(runDir, FinalSucceeded, "", nil, fixedTime()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(runDir, "run.lock")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("run.lock exists after completion: %v", err)
	}
	record, err := Read(filepath.Join(runDir, "run.json"))
	if err != nil {
		t.Fatal(err)
	}
	if record.FinalStatus == nil || record.FinalStatus.Status != FinalSucceeded {
		t.Fatalf("final_status = %#v", record.FinalStatus)
	}

	err = runStore.Complete(runDir, FinalBlocked, "again", nil, fixedTime())
	var finalErr *FinalStatusExistsError
	if !errors.As(err, &finalErr) {
		t.Fatalf("second final error = %T %[1]v, want FinalStatusExistsError", err)
	}
}

func TestRecoverIncompleteRuns(t *testing.T) {
	store, err := workspace.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	runStore := NewStore(store)
	workspaceID := "auto-openwrt"

	lockedID := "20260607T010203Z-locked"
	_, lockedDir, err := runStore.Create(CreateInput{
		Command:     "doctor",
		RunID:       lockedID,
		ProjectRoot: store.Root,
		WorkspaceID: &workspaceID,
		RelDir:      DoctorRunRelDir(&workspaceID, lockedID),
		Now:         fixedTime(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if lockedDir == "" {
		t.Fatal("locked run dir is empty")
	}

	incompleteID := "20260607T010204Z-incomp"
	_, incompleteDir, err := runStore.Create(CreateInput{
		Command:     "doctor",
		RunID:       incompleteID,
		ProjectRoot: store.Root,
		WorkspaceID: &workspaceID,
		RelDir:      DoctorRunRelDir(&workspaceID, incompleteID),
		Now:         fixedTime(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(incompleteDir, "run.lock")); err != nil {
		t.Fatal(err)
	}

	recovered, err := runStore.RecoverIncomplete(RecoverOptions{
		IsProcessAlive: func(pid int) bool { return false },
		Now:            fixedTime(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(recovered) != 2 {
		t.Fatalf("recovered = %v, want 2 runs", recovered)
	}

	lockedRecord, err := Read(filepath.Join(lockedDir, "run.json"))
	if err != nil {
		t.Fatal(err)
	}
	if lockedRecord.FinalStatus == nil || lockedRecord.FinalStatus.Reason != "interrupted" {
		t.Fatalf("locked final status = %#v", lockedRecord.FinalStatus)
	}
	incompleteRecord, err := Read(filepath.Join(incompleteDir, "run.json"))
	if err != nil {
		t.Fatal(err)
	}
	if incompleteRecord.FinalStatus == nil || incompleteRecord.FinalStatus.Reason != "incomplete-run-record" {
		t.Fatalf("incomplete final status = %#v", incompleteRecord.FinalStatus)
	}
}

func TestFindFinalSkipsIncompleteRuns(t *testing.T) {
	store, err := workspace.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	runStore := NewStore(store)
	workspaceID := "auto-openwrt"
	finalID := "20260607T010203Z-final1"
	incompleteID := "20260607T010204Z-later1"

	_, finalDir, err := runStore.Create(CreateInput{
		Command:     "doctor",
		RunID:       finalID,
		ProjectRoot: store.Root,
		WorkspaceID: &workspaceID,
		RelDir:      DoctorRunRelDir(&workspaceID, finalID),
		Now:         fixedTime(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := runStore.Complete(finalDir, FinalSucceeded, "", nil, fixedTime()); err != nil {
		t.Fatal(err)
	}
	if _, _, err := runStore.Create(CreateInput{
		Command:     "doctor",
		RunID:       incompleteID,
		ProjectRoot: store.Root,
		WorkspaceID: &workspaceID,
		RelDir:      DoctorRunRelDir(&workspaceID, incompleteID),
		Now:         fixedTime(),
	}); err != nil {
		t.Fatal(err)
	}

	record, _, err := runStore.FindFinal(LatestOptions{WorkspaceID: &workspaceID})
	if err != nil {
		t.Fatal(err)
	}
	if record.RunID != finalID {
		t.Fatalf("latest final run = %s, want %s", record.RunID, finalID)
	}
}

func fixedTime() time.Time {
	return time.Date(2026, 6, 7, 1, 2, 3, 0, time.UTC)
}
