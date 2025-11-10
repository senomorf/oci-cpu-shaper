package main

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

type issue struct {
	path    string
	message string
}

func main() {
	rootFlag := flag.String("root", ".", "repository root to scan")
	flag.Parse()

	rootAbs, err := filepath.Abs(*rootFlag)
	if err != nil {
		exitWithError(fmt.Errorf("resolve root: %w", err))
	}

	agentDirs, agentFiles, err := discoverAgents(rootAbs)
	if err != nil {
		exitWithError(err)
	}

	packages, err := discoverGoPackages(rootAbs)
	if err != nil {
		exitWithError(err)
	}

	var issues []issue

	for pkgDir := range packages {
		if _, ok := findNearestAgent(pkgDir, rootAbs, agentDirs); !ok {
			rel, _ := filepath.Rel(rootAbs, pkgDir)
			issues = append(issues, issue{path: rel, message: "missing AGENTS.md; no scoped instructions found"})
		}
	}

	for _, agentPath := range agentFiles {
		agentIssues, err := validateAgent(agentPath, rootAbs)
		if err != nil {
			exitWithError(err)
		}
		issues = append(issues, agentIssues...)
	}

	if len(issues) > 0 {
		sort.Slice(issues, func(i, j int) bool {
			if issues[i].path == issues[j].path {
				return issues[i].message < issues[j].message
			}
			return issues[i].path < issues[j].path
		})
		fmt.Fprintf(os.Stderr, "AGENTS policy violations detected:\n")
		for _, is := range issues {
			if is.path == "" {
				fmt.Fprintf(os.Stderr, " - %s\n", is.message)
				continue
			}
			fmt.Fprintf(os.Stderr, " - %s: %s\n", is.path, is.message)
		}
		os.Exit(1)
	}

	fmt.Println("AGENTS policy check passed")
}

func exitWithError(err error) {
	fmt.Fprintf(os.Stderr, "AGENTS check failed: %v\n", err)
	os.Exit(1)
}

func discoverAgents(root string) (map[string]struct{}, []string, error) {
	agents := make(map[string]struct{})
	var files []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "vendor":
				if path != root {
					return fs.SkipDir
				}
			}
			return nil
		}
		if strings.EqualFold(d.Name(), "AGENTS.md") {
			dir := filepath.Dir(path)
			agents[dir] = struct{}{}
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	sort.Strings(files)
	return agents, files, nil
}

func discoverGoPackages(root string) (map[string]struct{}, error) {
	packages := make(map[string]struct{})
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "vendor":
				if path != root {
					return fs.SkipDir
				}
			}
			return nil
		}
		if filepath.Ext(d.Name()) == ".go" {
			dir := filepath.Dir(path)
			packages[dir] = struct{}{}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return packages, nil
}

func findNearestAgent(dir, root string, agents map[string]struct{}) (string, bool) {
	current := dir
	for {
		if _, ok := agents[current]; ok {
			return current, true
		}
		if current == root {
			break
		}
		parent := filepath.Dir(current)
		if parent == current {
			break
		}
		current = parent
	}
	return "", false
}

func validateAgent(path, root string) (issues []issue, err error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() {
		if cerr := file.Close(); err == nil && cerr != nil {
			err = cerr
		}
	}()

	relDir, err := filepath.Rel(root, filepath.Dir(path))
	if err != nil {
		return nil, err
	}

	expectedScope := normalizeScope(relDir)
	scopeRe := regexp.MustCompile("^##\\s+Scope:\\s+`([^`]+)`\\s*$")
	tickDirRe := regexp.MustCompile("`([^`]+/)`")

	scopeValidated := false

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		if matches := scopeRe.FindStringSubmatch(line); matches != nil {
			scopeValidated = true
			actual := normalizeScope(matches[1])
			if expectedScope != "" && expectedScope != actual {
				issues = append(issues, issue{path: relDir, message: fmt.Sprintf("scope header mismatch: expected `%s`", expectedScope)})
			}
		}

		for _, match := range tickDirRe.FindAllStringSubmatch(line, -1) {
			target := strings.TrimSuffix(match[1], "/")
			if target == "" {
				continue
			}
			full := filepath.Join(root, target)
			stat, err := os.Stat(full)
			if errors.Is(err, os.ErrNotExist) {
				issues = append(issues, issue{path: relDir, message: fmt.Sprintf("references missing directory `%s/`", match[1])})
				continue
			}
			if err != nil {
				return nil, err
			}
			if !stat.IsDir() {
				issues = append(issues, issue{path: relDir, message: fmt.Sprintf("references `%s/` but target is not a directory", match[1])})
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}

	if expectedScope != "" && !scopeValidated {
		issues = append(issues, issue{path: relDir, message: "missing `## Scope:` header"})
	}

	return issues, nil
}

func normalizeScope(scope string) string {
	scope = strings.TrimSpace(scope)
	if scope == "." || scope == "" {
		return ""
	}
	scope = strings.TrimSuffix(scope, "/")
	return scope
}
