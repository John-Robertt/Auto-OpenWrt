package app

const (
	SchemaVersion = 1

	ExitOK             = 0
	ExitUsageError     = 2
	ExitHealthBlocked  = 3
	ExitWorkspaceError = 8
)

type Result struct {
	SchemaVersion int               `json:"schema_version"`
	Command       string            `json:"command"`
	Status        string            `json:"status"`
	ProjectRoot   string            `json:"project_root"`
	WorkspaceID   *string           `json:"workspace_id"`
	SourceSetID   *string           `json:"source_set_id"`
	BuildID       *string           `json:"build_id"`
	RunID         *string           `json:"run_id"`
	Paths         map[string]string `json:"paths"`
	Error         *Error            `json:"error"`
}

type Error struct {
	Code       string         `json:"code"`
	Message    string         `json:"message"`
	Suggestion string         `json:"suggestion"`
	Details    map[string]any `json:"details"`
}

func Succeeded(command, projectRoot string, paths map[string]string) Result {
	if paths == nil {
		paths = map[string]string{}
	}
	return Result{
		SchemaVersion: SchemaVersion,
		Command:       command,
		Status:        "succeeded",
		ProjectRoot:   projectRoot,
		Paths:         paths,
		Error:         nil,
	}
}

func Failed(command, projectRoot string, err *Error) Result {
	return Result{
		SchemaVersion: SchemaVersion,
		Command:       command,
		Status:        "failed",
		ProjectRoot:   projectRoot,
		Paths:         map[string]string{},
		Error:         err,
	}
}

func Blocked(command, projectRoot string, err *Error) Result {
	return Result{
		SchemaVersion: SchemaVersion,
		Command:       command,
		Status:        "blocked",
		ProjectRoot:   projectRoot,
		Paths:         map[string]string{},
		Error:         err,
	}
}
