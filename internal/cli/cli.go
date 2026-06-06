package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/John-Robertt/Auto-OpenWrt/internal/app"
)

const (
	exitOK          = 0
	exitUsageError  = 2
	schemaVersionV1 = 1
)

type commandSpec struct {
	name    string
	usage   string
	summary string
	flags   []flagSpec
}

type flagSpec struct {
	name        string
	description string
}

var commands = []commandSpec{
	{
		name:    "init",
		usage:   "auto-openwrt init [--project <path>] [--force] [--json]",
		summary: "Create project root directories and a sample config.",
		flags: []flagSpec{
			{"--project <path>", "Project root. Defaults to the current directory."},
			{"--force", "Overwrite the sample config without deleting workspace state."},
			{"--json", "Print machine-readable JSON output."},
		},
	},
	{
		name:    "doctor",
		usage:   "auto-openwrt doctor [--project <path>] [--config <path>] [--build <id>] [--json]",
		summary: "Run preflight checks for the host, project root, workspace, Docker, config, and AI CLI.",
		flags: []flagSpec{
			{"--project <path>", "Project root. Defaults to the current directory."},
			{"--config <path>", "User config path. Defaults to <project>/configs/auto-openwrt.yaml."},
			{"--build <id>", "Optional build id to validate."},
			{"--json", "Print machine-readable JSON output."},
			{"--verbose", "Print detailed logs."},
		},
	},
	{
		name:    "build",
		usage:   "auto-openwrt build --build <id> [--project <path>] [--config <path>] [--json]",
		summary: "Build one OpenWrt build through the project pipeline.",
		flags: []flagSpec{
			{"--build <id>", "Build id. Required."},
			{"--project <path>", "Project root. Defaults to the current directory."},
			{"--config <path>", "User config path. Defaults to <project>/configs/auto-openwrt.yaml."},
			{"--json", "Print machine-readable JSON output."},
			{"--verbose", "Print detailed logs."},
		},
	},
	{
		name:    "update",
		usage:   "auto-openwrt update [--project <path>] [--config <path>] [--build <id>] [--json]",
		summary: "Update OpenWrt, feeds, and plugin source-set caches.",
		flags: []flagSpec{
			{"--project <path>", "Project root. Defaults to the current directory."},
			{"--config <path>", "User config path. Defaults to <project>/configs/auto-openwrt.yaml."},
			{"--build <id>", "Optional build id used to limit feeds and plugins."},
			{"--json", "Print machine-readable JSON output."},
			{"--verbose", "Print detailed logs."},
		},
	},
	{
		name:    "logs",
		usage:   "auto-openwrt logs [--project <path>] [--config <path>] [--build <id>] [--run <run-id>] [--latest] [--json]",
		summary: "Show final run records, logs, diagnostics, and adopted patch history.",
		flags: []flagSpec{
			{"--project <path>", "Project root. Defaults to the current directory."},
			{"--config <path>", "User config path. Defaults to <project>/configs/auto-openwrt.yaml."},
			{"--build <id>", "Optional build filter."},
			{"--run <run-id>", "Read a specific final run."},
			{"--latest", "Read the latest final run. This is the default."},
			{"--json", "Print machine-readable JSON output."},
		},
	},
}

func Run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		printRootHelp(stdout)
		return exitOK
	}

	if args[0] == "help" {
		if len(args) == 1 {
			printRootHelp(stdout)
			return exitOK
		}
		if len(args) == 2 {
			return printCommandHelp(args[1], stdout, stderr)
		}
		printUsageError(stderr, "too many arguments for help", "Run auto-openwrt help [command].")
		return exitUsageError
	}

	if isHelpFlag(args[0]) {
		printRootHelp(stdout)
		return exitOK
	}

	cmd, ok := findCommand(args[0])
	if !ok {
		printUsageError(stderr, fmt.Sprintf("unknown command %q", args[0]), "Run auto-openwrt --help to see available commands.")
		return exitUsageError
	}
	if hasHelpFlag(args[1:]) {
		printOneCommandHelp(stdout, cmd)
		return exitOK
	}

	if cmd.name == "init" {
		return runInit(args[1:], stdout, stderr)
	}
	if cmd.name == "doctor" {
		return runDoctor(args[1:], stdout, stderr)
	}
	if cmd.name == "logs" {
		return runLogs(args[1:], stdout, stderr)
	}

	printUsageError(stderr, fmt.Sprintf("%q is not implemented in D2", cmd.name), "Run auto-openwrt "+cmd.name+" --help for the planned command shape.")
	return exitUsageError
}

type initFlags struct {
	project string
	force   bool
	json    bool
}

func runInit(args []string, stdout, stderr io.Writer) int {
	flags, parseErr := parseInitFlags(args)
	if parseErr != nil {
		result := app.Failed("init", absoluteOrEmpty(flags.project), &app.Error{
			Code:       "INVALID_ARGUMENT",
			Message:    parseErr.reason,
			Suggestion: parseErr.suggestion,
			Details:    map[string]any{},
		})
		if flags.json {
			printJSON(stdout, result)
		} else {
			printUsageError(stderr, parseErr.reason, parseErr.suggestion)
		}
		return exitUsageError
	}

	result, code := app.Init(app.InitOptions{
		Project: flags.project,
		Force:   flags.force,
	})
	if flags.json {
		printJSON(stdout, result)
		return code
	}
	if code == exitOK {
		fmt.Fprintf(stdout, "project initialized: %s\n", result.ProjectRoot)
		if result.Paths["config"] != "" {
			fmt.Fprintf(stdout, "config: %s\n", result.Paths["config"])
		}
		fmt.Fprintln(stdout, "next: edit configs/auto-openwrt.yaml, then run auto-openwrt doctor")
		return code
	}
	printAppError(stderr, result.Error)
	return code
}

type doctorFlags struct {
	project string
	config  string
	buildID string
	json    bool
	verbose bool
}

func runDoctor(args []string, stdout, stderr io.Writer) int {
	flags, parseErr := parseDoctorFlags(args)
	if parseErr != nil {
		result := app.Failed("doctor", absoluteOrEmpty(flags.project), &app.Error{
			Code:       "INVALID_ARGUMENT",
			Message:    parseErr.reason,
			Suggestion: parseErr.suggestion,
			Details:    map[string]any{},
		})
		if flags.json {
			printJSON(stdout, result)
		} else {
			printUsageError(stderr, parseErr.reason, parseErr.suggestion)
		}
		return exitUsageError
	}

	result, code := app.Doctor(context.Background(), app.DoctorOptions{
		Project: flags.project,
		Config:  flags.config,
		BuildID: flags.buildID,
	})
	if flags.json {
		printJSON(stdout, result)
		return code
	}
	if code == exitOK {
		fmt.Fprintf(stdout, "doctor run: %s\n", deref(result.RunID))
		fmt.Fprintf(stdout, "health report: %s\n", result.Paths["health_report"])
		return code
	}
	printAppError(stderr, result.Error)
	if result.Paths["health_report"] != "" {
		fmt.Fprintf(stderr, "health report: %s\n", result.Paths["health_report"])
	}
	return code
}

type logsFlags struct {
	project string
	config  string
	buildID string
	runID   string
	latest  bool
	json    bool
}

func runLogs(args []string, stdout, stderr io.Writer) int {
	flags, parseErr := parseLogsFlags(args)
	if parseErr != nil {
		result := app.Failed("logs", absoluteOrEmpty(flags.project), &app.Error{
			Code:       "INVALID_ARGUMENT",
			Message:    parseErr.reason,
			Suggestion: parseErr.suggestion,
			Details:    map[string]any{},
		})
		if flags.json {
			printJSON(stdout, result)
		} else {
			printUsageError(stderr, parseErr.reason, parseErr.suggestion)
		}
		return exitUsageError
	}

	result, code := app.Logs(app.LogsOptions{
		Project: flags.project,
		Config:  flags.config,
		BuildID: flags.buildID,
		RunID:   flags.runID,
		Latest:  flags.latest,
	})
	if flags.json {
		printJSON(stdout, result)
		return code
	}
	if code == exitOK {
		fmt.Fprintf(stdout, "run: %s\n", deref(result.RunID))
		fmt.Fprintf(stdout, "run record: %s\n", result.Paths["run_record"])
		return code
	}
	printAppError(stderr, result.Error)
	return code
}

type parseError struct {
	reason     string
	suggestion string
}

func parseInitFlags(args []string) (initFlags, *parseError) {
	flags := initFlags{project: "."}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--project":
			if i+1 >= len(args) {
				return flags, &parseError{"--project requires a path", "Pass --project <path>."}
			}
			i++
			flags.project = args[i]
		case strings.HasPrefix(arg, "--project="):
			flags.project = strings.TrimPrefix(arg, "--project=")
			if flags.project == "" {
				return flags, &parseError{"--project requires a path", "Pass --project <path>."}
			}
		case arg == "--force":
			flags.force = true
		case arg == "--json":
			flags.json = true
		default:
			if strings.HasPrefix(arg, "-") {
				return flags, &parseError{fmt.Sprintf("unknown flag %q", arg), "Run auto-openwrt init --help to see supported flags."}
			}
			return flags, &parseError{fmt.Sprintf("unexpected argument %q", arg), "Run auto-openwrt init --help to see the command format."}
		}
	}
	return flags, nil
}

func parseDoctorFlags(args []string) (doctorFlags, *parseError) {
	flags := doctorFlags{project: "."}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--project":
			value, next, err := requireValue(args, i, "--project", "Pass --project <path>.")
			if err != nil {
				return flags, err
			}
			flags.project = value
			i = next
		case strings.HasPrefix(arg, "--project="):
			flags.project = strings.TrimPrefix(arg, "--project=")
			if flags.project == "" {
				return flags, &parseError{"--project requires a path", "Pass --project <path>."}
			}
		case arg == "--config":
			value, next, err := requireValue(args, i, "--config", "Pass --config <path>.")
			if err != nil {
				return flags, err
			}
			flags.config = value
			i = next
		case strings.HasPrefix(arg, "--config="):
			flags.config = strings.TrimPrefix(arg, "--config=")
			if flags.config == "" {
				return flags, &parseError{"--config requires a path", "Pass --config <path>."}
			}
		case arg == "--build":
			value, next, err := requireValue(args, i, "--build", "Pass --build <id>.")
			if err != nil {
				return flags, err
			}
			flags.buildID = value
			i = next
		case strings.HasPrefix(arg, "--build="):
			flags.buildID = strings.TrimPrefix(arg, "--build=")
			if flags.buildID == "" {
				return flags, &parseError{"--build requires an id", "Pass --build <id>."}
			}
		case arg == "--json":
			flags.json = true
		case arg == "--verbose":
			flags.verbose = true
		default:
			if strings.HasPrefix(arg, "-") {
				return flags, &parseError{fmt.Sprintf("unknown flag %q", arg), "Run auto-openwrt doctor --help to see supported flags."}
			}
			return flags, &parseError{fmt.Sprintf("unexpected argument %q", arg), "Run auto-openwrt doctor --help to see the command format."}
		}
	}
	return flags, nil
}

func parseLogsFlags(args []string) (logsFlags, *parseError) {
	flags := logsFlags{project: ".", latest: true}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--project":
			value, next, err := requireValue(args, i, "--project", "Pass --project <path>.")
			if err != nil {
				return flags, err
			}
			flags.project = value
			i = next
		case strings.HasPrefix(arg, "--project="):
			flags.project = strings.TrimPrefix(arg, "--project=")
			if flags.project == "" {
				return flags, &parseError{"--project requires a path", "Pass --project <path>."}
			}
		case arg == "--config":
			value, next, err := requireValue(args, i, "--config", "Pass --config <path>.")
			if err != nil {
				return flags, err
			}
			flags.config = value
			i = next
		case strings.HasPrefix(arg, "--config="):
			flags.config = strings.TrimPrefix(arg, "--config=")
			if flags.config == "" {
				return flags, &parseError{"--config requires a path", "Pass --config <path>."}
			}
		case arg == "--build":
			value, next, err := requireValue(args, i, "--build", "Pass --build <id>.")
			if err != nil {
				return flags, err
			}
			flags.buildID = value
			i = next
		case strings.HasPrefix(arg, "--build="):
			flags.buildID = strings.TrimPrefix(arg, "--build=")
			if flags.buildID == "" {
				return flags, &parseError{"--build requires an id", "Pass --build <id>."}
			}
		case arg == "--run":
			value, next, err := requireValue(args, i, "--run", "Pass --run <run-id>.")
			if err != nil {
				return flags, err
			}
			flags.runID = value
			flags.latest = false
			i = next
		case strings.HasPrefix(arg, "--run="):
			flags.runID = strings.TrimPrefix(arg, "--run=")
			if flags.runID == "" {
				return flags, &parseError{"--run requires a run id", "Pass --run <run-id>."}
			}
			flags.latest = false
		case arg == "--latest":
			if flags.runID != "" {
				return flags, &parseError{"--run and --latest cannot be used together", "Use either --run <run-id> or --latest."}
			}
			flags.latest = true
		case arg == "--json":
			flags.json = true
		default:
			if strings.HasPrefix(arg, "-") {
				return flags, &parseError{fmt.Sprintf("unknown flag %q", arg), "Run auto-openwrt logs --help to see supported flags."}
			}
			return flags, &parseError{fmt.Sprintf("unexpected argument %q", arg), "Run auto-openwrt logs --help to see the command format."}
		}
	}
	return flags, nil
}

func requireValue(args []string, index int, flag, suggestion string) (string, int, *parseError) {
	if index+1 >= len(args) {
		return "", index, &parseError{flag + " requires a value", suggestion}
	}
	value := args[index+1]
	if value == "" {
		return "", index, &parseError{flag + " requires a value", suggestion}
	}
	return value, index + 1, nil
}

func printRootHelp(w io.Writer) {
	fmt.Fprintln(w, "Auto-OpenWrt builds OpenWrt firmware from a project-root-backed pipeline.")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Usage:")
	fmt.Fprintln(w, "  auto-openwrt <command> [flags]")
	fmt.Fprintln(w, "  auto-openwrt help [command]")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Commands:")
	for _, cmd := range commands {
		fmt.Fprintf(w, "  %-8s %s\n", cmd.name, cmd.summary)
	}
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Global flags:")
	fmt.Fprintln(w, "  --project <path>  Project root. Defaults to the current directory.")
	fmt.Fprintln(w, "  --config <path>   User config path. Defaults to <project>/configs/auto-openwrt.yaml.")
	fmt.Fprintln(w, "  --json              Print machine-readable JSON output.")
	fmt.Fprintln(w, "  --verbose           Print detailed logs.")
	fmt.Fprintln(w)
	fmt.Fprintf(w, "JSON schema version: %d\n", schemaVersionV1)
}

func printCommandHelp(name string, stdout, stderr io.Writer) int {
	cmd, ok := findCommand(name)
	if !ok {
		printUsageError(stderr, fmt.Sprintf("unknown command %q", name), "Run auto-openwrt --help to see available commands.")
		return exitUsageError
	}
	printOneCommandHelp(stdout, cmd)
	return exitOK
}

func printOneCommandHelp(w io.Writer, cmd commandSpec) {
	fmt.Fprintln(w, cmd.summary)
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Usage:")
	fmt.Fprintf(w, "  %s\n", cmd.usage)
	if len(cmd.flags) == 0 {
		return
	}
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Flags:")
	for _, flag := range cmd.flags {
		fmt.Fprintf(w, "  %-20s %s\n", flag.name, flag.description)
	}
}

func printUsageError(w io.Writer, reason, suggestion string) {
	fmt.Fprintf(w, "error: %s\n", reason)
	fmt.Fprintf(w, "suggestion: %s\n", suggestion)
}

func printAppError(w io.Writer, err *app.Error) {
	if err == nil {
		return
	}
	fmt.Fprintf(w, "error: %s\n", err.Message)
	fmt.Fprintf(w, "suggestion: %s\n", err.Suggestion)
}

func printJSON(w io.Writer, value any) {
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	_ = encoder.Encode(value)
}

func absoluteOrEmpty(path string) string {
	if path == "" {
		return ""
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return path
	}
	return abs
}

func deref(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func findCommand(name string) (commandSpec, bool) {
	for _, cmd := range commands {
		if cmd.name == name {
			return cmd, true
		}
	}
	return commandSpec{}, false
}

func hasHelpFlag(args []string) bool {
	for _, arg := range args {
		if isHelpFlag(arg) {
			return true
		}
	}
	return false
}

func isHelpFlag(arg string) bool {
	return arg == "--help" || arg == "-h"
}
