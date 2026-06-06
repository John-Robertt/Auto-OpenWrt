package docscheck

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type Result struct {
	Issues []Issue
}

type Issue struct {
	File    string
	Message string
}

type document struct {
	absPath     string
	relRepoPath string
	relDocsPath string
	meta        metadata
}

type metadata struct {
	status      string
	owner       string
	lastUpdated string
	dependsOn   []string
}

var validOwners = map[string]bool{
	"product":      true,
	"architecture": true,
	"engineering":  true,
}

var validDocumentStatuses = map[string]bool{
	"draft":      true,
	"proposed":   true,
	"accepted":   true,
	"superseded": true,
}

var validADRStatuses = map[string]bool{
	"draft":      true,
	"proposed":   true,
	"accepted":   true,
	"deprecated": true,
	"superseded": true,
}

func Check(docsRoot string) (Result, error) {
	absDocsRoot, err := filepath.Abs(docsRoot)
	if err != nil {
		return Result{}, err
	}
	info, err := os.Stat(absDocsRoot)
	if err != nil {
		return Result{}, err
	}
	if !info.IsDir() {
		return Result{}, fmt.Errorf("%s is not a directory", docsRoot)
	}

	repoRoot := filepath.Dir(absDocsRoot)
	var result Result
	docs := map[string]document{}

	err = filepath.WalkDir(absDocsRoot, func(filePath string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || filepath.Ext(filePath) != ".md" {
			return nil
		}

		relRepoPath, err := filepath.Rel(repoRoot, filePath)
		if err != nil {
			return err
		}
		relDocsPath, err := filepath.Rel(absDocsRoot, filePath)
		if err != nil {
			return err
		}
		relRepoPath = filepath.ToSlash(relRepoPath)
		relDocsPath = filepath.ToSlash(relDocsPath)

		data, err := os.ReadFile(filePath)
		if err != nil {
			return err
		}
		meta, issues := parseFrontMatter(relRepoPath, string(data))
		result.Issues = append(result.Issues, issues...)
		docs[relRepoPath] = document{
			absPath:     filePath,
			relRepoPath: relRepoPath,
			relDocsPath: relDocsPath,
			meta:        meta,
		}
		return nil
	})
	if err != nil {
		return Result{}, err
	}

	result.Issues = append(result.Issues, validateMetadata(docs)...)
	result.Issues = append(result.Issues, validateDependencies(docs)...)
	result.Issues = append(result.Issues, validateREADMEStatusTable(docs)...)
	sortIssues(result.Issues)
	return result, nil
}

func parseFrontMatter(file string, data string) (metadata, []Issue) {
	var issues []Issue
	normalized := strings.ReplaceAll(data, "\r\n", "\n")
	lines := strings.Split(normalized, "\n")
	if len(lines) == 0 || lines[0] != "---" {
		return metadata{}, []Issue{{File: file, Message: "missing front matter delimiter"}}
	}

	end := -1
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "---" {
			end = i
			break
		}
	}
	if end == -1 {
		return metadata{}, []Issue{{File: file, Message: "missing closing front matter delimiter"}}
	}

	var meta metadata
	seen := map[string]bool{}
	for i := 1; i < end; i++ {
		line := strings.TrimSpace(lines[i])
		if line == "" {
			continue
		}

		key, value, ok := strings.Cut(line, ":")
		if !ok {
			issues = append(issues, Issue{File: file, Message: fmt.Sprintf("invalid front matter line %q", line)})
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if seen[key] {
			issues = append(issues, Issue{File: file, Message: fmt.Sprintf("duplicate front matter key %q", key)})
		}
		seen[key] = true

		switch key {
		case "status":
			meta.status = value
		case "owner":
			meta.owner = value
		case "last_updated":
			meta.lastUpdated = value
		case "depends_on":
			if value == "[]" {
				meta.dependsOn = nil
				continue
			}
			if value != "" {
				issues = append(issues, Issue{File: file, Message: "depends_on must be [] or a block list"})
				continue
			}
			for i+1 < end && strings.HasPrefix(lines[i+1], "  - ") {
				i++
				dependency := strings.TrimSpace(strings.TrimPrefix(lines[i], "  - "))
				meta.dependsOn = append(meta.dependsOn, dependency)
			}
			if len(meta.dependsOn) == 0 {
				issues = append(issues, Issue{File: file, Message: "depends_on block list is empty; use depends_on: []"})
			}
		}
	}

	for _, key := range []string{"status", "owner", "last_updated", "depends_on"} {
		if !seen[key] {
			issues = append(issues, Issue{File: file, Message: fmt.Sprintf("missing front matter key %q", key)})
		}
	}

	return meta, issues
}

func validateMetadata(docs map[string]document) []Issue {
	var issues []Issue
	for _, doc := range docs {
		if !validStatus(doc.relRepoPath, doc.meta.status) {
			issues = append(issues, Issue{File: doc.relRepoPath, Message: fmt.Sprintf("invalid status %q", doc.meta.status)})
		}
		if !validOwners[doc.meta.owner] {
			issues = append(issues, Issue{File: doc.relRepoPath, Message: fmt.Sprintf("invalid owner %q", doc.meta.owner)})
		}
		parsed, err := time.Parse("2006-01-02", doc.meta.lastUpdated)
		if err != nil || parsed.Format("2006-01-02") != doc.meta.lastUpdated {
			issues = append(issues, Issue{File: doc.relRepoPath, Message: fmt.Sprintf("invalid last_updated %q", doc.meta.lastUpdated)})
		}
		for _, dependency := range doc.meta.dependsOn {
			if !strings.HasPrefix(dependency, "docs/") || !strings.HasSuffix(dependency, ".md") {
				issues = append(issues, Issue{File: doc.relRepoPath, Message: fmt.Sprintf("invalid dependency path %q", dependency)})
			}
		}
	}
	return issues
}

func validStatus(file string, status string) bool {
	if strings.HasPrefix(file, "docs/04-decisions/") {
		return validADRStatuses[status]
	}
	return validDocumentStatuses[status]
}

func validateDependencies(docs map[string]document) []Issue {
	var issues []Issue
	for _, doc := range docs {
		for _, dependency := range doc.meta.dependsOn {
			if dependency == doc.relRepoPath {
				issues = append(issues, Issue{File: doc.relRepoPath, Message: "document depends on itself"})
				continue
			}
			if _, ok := docs[dependency]; !ok {
				issues = append(issues, Issue{File: doc.relRepoPath, Message: fmt.Sprintf("dependency does not exist: %s", dependency)})
			}
		}
	}
	issues = append(issues, detectDependencyCycles(docs)...)
	return issues
}

func detectDependencyCycles(docs map[string]document) []Issue {
	const (
		unvisited = 0
		visiting  = 1
		visited   = 2
	)

	state := map[string]int{}
	var stack []string
	var issues []Issue
	reported := map[string]bool{}

	var visit func(string)
	visit = func(file string) {
		switch state[file] {
		case visiting:
			cycle := appendCycle(stack, file)
			key := strings.Join(cycle, " -> ")
			if !reported[key] {
				reported[key] = true
				issues = append(issues, Issue{File: file, Message: "dependency cycle: " + key})
			}
			return
		case visited:
			return
		}

		state[file] = visiting
		stack = append(stack, file)
		doc := docs[file]
		for _, dependency := range doc.meta.dependsOn {
			if _, ok := docs[dependency]; ok {
				visit(dependency)
			}
		}
		stack = stack[:len(stack)-1]
		state[file] = visited
	}

	files := sortedDocKeys(docs)
	for _, file := range files {
		if state[file] == unvisited {
			visit(file)
		}
	}
	return issues
}

func appendCycle(stack []string, repeated string) []string {
	for i, file := range stack {
		if file == repeated {
			cycle := append([]string{}, stack[i:]...)
			cycle = append(cycle, repeated)
			return cycle
		}
	}
	return []string{repeated, repeated}
}

type readmeEntry struct {
	pattern string
	status  string
	line    int
}

func validateREADMEStatusTable(docs map[string]document) []Issue {
	readme, ok := docs["docs/README.md"]
	if !ok {
		return []Issue{{File: "docs/README.md", Message: "README is missing"}}
	}

	entries, issues := parseREADMEEntries(readme)
	if len(entries) == 0 {
		issues = append(issues, Issue{File: readme.relRepoPath, Message: "README status table has no document entries"})
		return issues
	}

	for _, entry := range entries {
		if !validDocumentStatuses[entry.status] && !validADRStatuses[entry.status] {
			issues = append(issues, Issue{File: readme.relRepoPath, Message: fmt.Sprintf("line %d has invalid README status %q", entry.line, entry.status)})
		}
		matches := docsMatchingEntry(docs, entry.pattern)
		if len(matches) == 0 {
			issues = append(issues, Issue{File: readme.relRepoPath, Message: fmt.Sprintf("line %d entry %q matches no documents", entry.line, entry.pattern)})
			continue
		}
		for _, doc := range matches {
			if doc.relRepoPath == "docs/README.md" {
				continue
			}
			if doc.meta.status != entry.status {
				issues = append(issues, Issue{File: readme.relRepoPath, Message: fmt.Sprintf("line %d status mismatch for %s: README has %s, front matter has %s", entry.line, doc.relDocsPath, entry.status, doc.meta.status)})
			}
		}
	}

	for _, doc := range docs {
		if doc.relRepoPath == "docs/README.md" {
			continue
		}
		if !coveredByREADMEEntry(doc, entries) {
			issues = append(issues, Issue{File: readme.relRepoPath, Message: fmt.Sprintf("README status table does not cover %s", doc.relDocsPath)})
		}
	}

	return issues
}

func parseREADMEEntries(readme document) ([]readmeEntry, []Issue) {
	data, err := os.ReadFile(readme.absPath)
	if err != nil {
		return nil, []Issue{{File: readme.relRepoPath, Message: err.Error()}}
	}

	var entries []readmeEntry
	lines := strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n")
	for i, line := range lines {
		if !strings.HasPrefix(strings.TrimSpace(line), "|") {
			continue
		}
		cells := splitMarkdownTableLine(line)
		if len(cells) < 3 {
			continue
		}
		docPattern, ok := extractBacktickValue(cells[0])
		if !ok || (!strings.HasSuffix(docPattern, ".md") && !strings.HasSuffix(docPattern, "*.md")) {
			continue
		}
		docPattern = strings.TrimPrefix(docPattern, "docs/")
		entries = append(entries, readmeEntry{
			pattern: docPattern,
			status:  strings.TrimSpace(cells[1]),
			line:    i + 1,
		})
	}
	return entries, nil
}

func splitMarkdownTableLine(line string) []string {
	trimmed := strings.TrimSpace(line)
	trimmed = strings.TrimPrefix(trimmed, "|")
	trimmed = strings.TrimSuffix(trimmed, "|")
	rawCells := strings.Split(trimmed, "|")
	cells := make([]string, 0, len(rawCells))
	for _, cell := range rawCells {
		cells = append(cells, strings.TrimSpace(cell))
	}
	return cells
}

func extractBacktickValue(cell string) (string, bool) {
	start := strings.Index(cell, "`")
	if start == -1 {
		return "", false
	}
	end := strings.Index(cell[start+1:], "`")
	if end == -1 {
		return "", false
	}
	return cell[start+1 : start+1+end], true
}

func docsMatchingEntry(docs map[string]document, pattern string) []document {
	var matches []document
	for _, doc := range docs {
		matched, err := path.Match(pattern, doc.relDocsPath)
		if err != nil {
			continue
		}
		if matched {
			matches = append(matches, doc)
		}
	}
	sort.Slice(matches, func(i, j int) bool {
		return matches[i].relRepoPath < matches[j].relRepoPath
	})
	return matches
}

func coveredByREADMEEntry(doc document, entries []readmeEntry) bool {
	for _, entry := range entries {
		matched, err := path.Match(entry.pattern, doc.relDocsPath)
		if err == nil && matched {
			return true
		}
	}
	return false
}

func sortedDocKeys(docs map[string]document) []string {
	keys := make([]string, 0, len(docs))
	for key := range docs {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func sortIssues(issues []Issue) {
	sort.Slice(issues, func(i, j int) bool {
		if issues[i].File == issues[j].File {
			return issues[i].Message < issues[j].Message
		}
		return issues[i].File < issues[j].File
	})
}
