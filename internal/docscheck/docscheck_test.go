package docscheck

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRepositoryDocsPass(t *testing.T) {
	result, err := Check(filepath.Join("..", "..", "docs"))
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Issues) != 0 {
		t.Fatalf("repository docs should pass, got issues:\n%s", formatIssues(result.Issues))
	}
}

func TestCheckDetectsMissingFrontMatter(t *testing.T) {
	root := newDocsFixture(t)
	writeDocsREADME(t, root, map[string]string{"bad.md": "accepted"})
	writeFile(t, filepath.Join(root, "bad.md"), "# Bad\n")

	result := mustCheck(t, root)

	if !hasIssue(result.Issues, "bad.md", "missing front matter delimiter") {
		t.Fatalf("missing front matter issue not found:\n%s", formatIssues(result.Issues))
	}
}

func TestCheckDetectsBadDependency(t *testing.T) {
	root := newDocsFixture(t)
	writeDocsREADME(t, root, map[string]string{"bad.md": "accepted"})
	writeDoc(t, root, "bad.md", frontMatter("accepted", "engineering", []string{"docs/missing.md"})+"# Bad\n")

	result := mustCheck(t, root)

	if !hasIssue(result.Issues, "bad.md", "dependency does not exist: docs/missing.md") {
		t.Fatalf("bad dependency issue not found:\n%s", formatIssues(result.Issues))
	}
}

func TestCheckDetectsDependencyCycle(t *testing.T) {
	root := newDocsFixture(t)
	writeDocsREADME(t, root, map[string]string{
		"a.md": "accepted",
		"b.md": "accepted",
	})
	writeDoc(t, root, "a.md", frontMatter("accepted", "engineering", []string{"docs/b.md"})+"# A\n")
	writeDoc(t, root, "b.md", frontMatter("accepted", "engineering", []string{"docs/a.md"})+"# B\n")

	result := mustCheck(t, root)

	if !hasIssue(result.Issues, "a.md", "dependency cycle:") && !hasIssue(result.Issues, "b.md", "dependency cycle:") {
		t.Fatalf("cycle issue not found:\n%s", formatIssues(result.Issues))
	}
}

func TestCheckDetectsREADMEStatusMismatch(t *testing.T) {
	root := newDocsFixture(t)
	writeDocsREADME(t, root, map[string]string{"mismatch.md": "accepted"})
	writeDoc(t, root, "mismatch.md", frontMatter("proposed", "engineering", nil)+"# Mismatch\n")

	result := mustCheck(t, root)

	if !hasIssue(result.Issues, "README.md", "status mismatch for mismatch.md") {
		t.Fatalf("README mismatch issue not found:\n%s", formatIssues(result.Issues))
	}
}

func newDocsFixture(t *testing.T) string {
	t.Helper()
	root := filepath.Join(t.TempDir(), "docs")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	return root
}

func writeDocsREADME(t *testing.T, root string, statuses map[string]string) {
	t.Helper()
	var builder strings.Builder
	builder.WriteString(frontMatter("accepted", "architecture", nil))
	builder.WriteString("# Docs\n\n")
	builder.WriteString("| Document | Status | Description |\n")
	builder.WriteString("| --- | --- | --- |\n")
	for file, status := range statuses {
		builder.WriteString("| `")
		builder.WriteString(file)
		builder.WriteString("` | ")
		builder.WriteString(status)
		builder.WriteString(" | fixture |\n")
	}
	writeDoc(t, root, "README.md", builder.String())
}

func writeDoc(t *testing.T, root, relPath, content string) {
	t.Helper()
	writeFile(t, filepath.Join(root, filepath.FromSlash(relPath)), content)
}

func writeFile(t *testing.T, filePath, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(filePath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filePath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func frontMatter(status, owner string, dependencies []string) string {
	var builder strings.Builder
	builder.WriteString("---\n")
	builder.WriteString("status: ")
	builder.WriteString(status)
	builder.WriteString("\nowner: ")
	builder.WriteString(owner)
	builder.WriteString("\nlast_updated: 2026-06-06\n")
	if len(dependencies) == 0 {
		builder.WriteString("depends_on: []\n")
	} else {
		builder.WriteString("depends_on:\n")
		for _, dependency := range dependencies {
			builder.WriteString("  - ")
			builder.WriteString(dependency)
			builder.WriteString("\n")
		}
	}
	builder.WriteString("---\n\n")
	return builder.String()
}

func mustCheck(t *testing.T, root string) Result {
	t.Helper()
	result, err := Check(root)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func hasIssue(issues []Issue, filePart, messagePart string) bool {
	for _, issue := range issues {
		if strings.Contains(issue.File, filePart) && strings.Contains(issue.Message, messagePart) {
			return true
		}
	}
	return false
}

func formatIssues(issues []Issue) string {
	var builder strings.Builder
	for _, issue := range issues {
		builder.WriteString(issue.File)
		builder.WriteString(": ")
		builder.WriteString(issue.Message)
		builder.WriteString("\n")
	}
	return builder.String()
}
