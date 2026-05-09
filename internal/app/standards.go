package app

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"agentswitcher/internal/store"
)

type standardsFocus int

const (
	standardsFocusDirectory standardsFocus = iota
	standardsFocusFiles
)

type standardCandidate struct {
	Path     string
	Name     string
	Selected bool
}

func loadPromptStandards(selected []store.Standard) ([]promptStandard, error) {
	standards := make([]promptStandard, 0, len(selected))
	for _, item := range selected {
		content, err := os.ReadFile(item.Path)
		if err != nil {
			return nil, fmt.Errorf("read standard %s: %w", item.Path, err)
		}
		standards = append(standards, promptStandard{
			Path:    item.Path,
			Name:    filepath.Base(item.Path),
			Content: strings.TrimSpace(string(content)),
		})
	}
	return standards, nil
}

func selectedStandardPathSet(standards []store.Standard) map[string]bool {
	selected := make(map[string]bool, len(standards))
	for _, standard := range standards {
		selected[standard.Path] = true
	}
	return selected
}

func selectedStandardPaths(selected map[string]bool) []string {
	paths := make([]string, 0, len(selected))
	for path, enabled := range selected {
		if enabled {
			paths = append(paths, path)
		}
	}
	sort.Strings(paths)
	return paths
}

func defaultStandardsDirectory(projectDir string, standards []store.Standard) string {
	if len(standards) == 0 {
		return projectDir
	}
	dir := filepath.Dir(standards[0].Path)
	if dir == "." || dir == "" {
		return projectDir
	}
	return dir
}

func listStandardCandidates(dir string, selected map[string]bool) ([]standardCandidate, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read standards directory %s: %w", dir, err)
	}

	var candidates []standardCandidate
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if !isMarkdownFile(entry.Name()) {
			continue
		}

		fullPath := filepath.Join(dir, entry.Name())
		candidates = append(candidates, standardCandidate{
			Path:     fullPath,
			Name:     entry.Name(),
			Selected: selected[fullPath],
		})
	}

	sort.Slice(candidates, func(i, j int) bool {
		return strings.ToLower(candidates[i].Name) < strings.ToLower(candidates[j].Name)
	})

	return candidates, nil
}

func isMarkdownFile(name string) bool {
	lower := strings.ToLower(name)
	return strings.HasSuffix(lower, ".md") || strings.HasSuffix(lower, ".markdown") || strings.HasSuffix(lower, ".mdx")
}

func expandDirectoryInput(input, projectDir string) (string, error) {
	value := strings.TrimSpace(input)
	if value == "" {
		return projectDir, nil
	}

	if strings.HasPrefix(value, "~") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolve home directory: %w", err)
		}
		value = strings.TrimPrefix(value, "~")
		value = strings.TrimPrefix(value, string(filepath.Separator))
		value = filepath.Join(home, value)
	}

	if !filepath.IsAbs(value) {
		value = filepath.Join(projectDir, value)
	}

	return filepath.Clean(value), nil
}

func autocompleteDirectory(input, projectDir string) ([]string, error) {
	expanded, err := expandDirectoryInput(input, projectDir)
	if err != nil {
		return nil, err
	}

	parent := expanded
	prefix := ""
	if !strings.HasSuffix(strings.TrimSpace(input), string(filepath.Separator)) {
		parent = filepath.Dir(expanded)
		prefix = filepath.Base(expanded)
	}

	entries, err := os.ReadDir(parent)
	if err != nil {
		return nil, fmt.Errorf("read parent directory %s: %w", parent, err)
	}

	var matches []string
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		if prefix != "" && !strings.HasPrefix(strings.ToLower(entry.Name()), strings.ToLower(prefix)) {
			continue
		}
		matches = append(matches, filepath.Join(parent, entry.Name())+string(filepath.Separator))
	}

	sort.Strings(matches)
	return matches, nil
}

func relativeDirectoryDisplay(path, projectDir string) string {
	if rel, err := filepath.Rel(projectDir, path); err == nil && !strings.HasPrefix(rel, "..") {
		if rel == "." {
			return "." + string(filepath.Separator)
		}
		return rel
	}
	return path
}

func visibleListWindow(length, selectedIndex, maxRows int) (int, int) {
	if maxRows <= 0 || length <= maxRows {
		return 0, length
	}

	start := selectedIndex - maxRows/2
	if start < 0 {
		start = 0
	}
	end := start + maxRows
	if end > length {
		end = length
		start = end - maxRows
	}
	return start, end
}
