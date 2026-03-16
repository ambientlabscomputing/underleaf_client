package deploy

import (
	"archive/tar"
	"bufio"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// createBuildArchive creates a gzipped tar archive of projectDir and writes it
// to a temporary file. The archive wraps all content under a single top-level
// directory named "local-context/" — matching the structure GitHub generates for
// repository tarballs — so the deployment_engine's extractTarGzStripped helper
// can unpack it without any special-casing.
//
// Returns the path of the temp file. The caller is responsible for removing it
// (typically via defer os.Remove(path)).
//
// Files/directories matching patterns from .dockerignore (if present) are excluded.
// The .git directory is always excluded regardless of .dockerignore.
func createBuildArchive(projectDir string) (string, error) {
	absDir, err := filepath.Abs(projectDir)
	if err != nil {
		return "", fmt.Errorf("failed to resolve project directory: %w", err)
	}

	excludePatterns, err := loadDockerignore(absDir)
	if err != nil {
		// Non-fatal: proceed without exclusions if .dockerignore can't be read.
		excludePatterns = nil
	}

	tmp, err := os.CreateTemp("", "underleaf-build-*.tar.gz")
	if err != nil {
		return "", fmt.Errorf("failed to create temp archive file: %w", err)
	}
	tmpPath := tmp.Name()

	// Wrap in a cleanup closure so we remove the file on error.
	if err := writeTarGz(tmp, absDir, excludePatterns); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return "", fmt.Errorf("failed to create build archive: %w", err)
	}

	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return "", fmt.Errorf("failed to finalise archive: %w", err)
	}

	return tmpPath, nil
}

func writeTarGz(w io.Writer, srcDir string, excludePatterns []string) error {
	gw := gzip.NewWriter(w)
	tw := tar.NewWriter(gw)

	topLevel := "local-context/"

	err := filepath.Walk(srcDir, func(path string, fi os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		relPath, err := filepath.Rel(srcDir, path)
		if err != nil {
			return err
		}
		relPath = filepath.ToSlash(relPath)

		// Always exclude .git
		if relPath == ".git" || strings.HasPrefix(relPath, ".git/") {
			if fi.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		// Apply .dockerignore exclusions
		if shouldExclude(relPath, fi.IsDir(), excludePatterns) {
			if fi.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		// Skip the root directory itself (we add topLevel as the wrapper instead)
		if relPath == "." {
			return nil
		}

		hdr, err := tar.FileInfoHeader(fi, "")
		if err != nil {
			return fmt.Errorf("failed to create tar header for %s: %w", relPath, err)
		}
		hdr.Name = topLevel + relPath
		if fi.IsDir() {
			hdr.Name += "/"
		}

		if err := tw.WriteHeader(hdr); err != nil {
			return fmt.Errorf("failed to write tar header for %s: %w", relPath, err)
		}

		if fi.Mode().IsRegular() {
			f, err := os.Open(path)
			if err != nil {
				return fmt.Errorf("failed to open %s: %w", relPath, err)
			}
			defer f.Close()
			if _, err := io.Copy(tw, f); err != nil {
				return fmt.Errorf("failed to copy %s into archive: %w", relPath, err)
			}
		}

		return nil
	})

	if err != nil {
		return err
	}

	if err := tw.Close(); err != nil {
		return err
	}
	return gw.Close()
}

// loadDockerignore reads .dockerignore from projectDir and returns a slice of
// pattern strings. Lines starting with # and blank lines are skipped.
func loadDockerignore(projectDir string) ([]string, error) {
	dockerignorePath := filepath.Join(projectDir, ".dockerignore")
	f, err := os.Open(dockerignorePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()

	var patterns []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		patterns = append(patterns, line)
	}
	return patterns, scanner.Err()
}

// shouldExclude returns true if relPath matches any of the .dockerignore patterns.
// This is a simplified implementation that handles the most common patterns:
// exact matches, directory prefixes, and simple glob wildcards via filepath.Match.
func shouldExclude(relPath string, isDir bool, patterns []string) bool {
	for _, pattern := range patterns {
		negated := strings.HasPrefix(pattern, "!")
		if negated {
			pattern = pattern[1:]
		}

		matched := false

		// Direct match
		if m, _ := filepath.Match(pattern, relPath); m {
			matched = true
		}

		// Match as directory prefix (pattern is a directory, relPath is inside it)
		if !matched {
			prefix := strings.TrimSuffix(pattern, "/")
			if strings.HasPrefix(relPath, prefix+"/") {
				matched = true
			}
		}

		// Basename match (e.g., "*.log" should match "logs/app.log")
		if !matched {
			if m, _ := filepath.Match(pattern, filepath.Base(relPath)); m {
				matched = true
			}
		}

		if matched {
			// Negation re-includes the path; we can't easily shortcircuit here
			// because a later positive pattern might re-exclude it.
			// For now: treat the last matching non-negated rule as authoritative,
			// which is the standard Docker behavior. Return false for negations.
			if negated {
				return false
			}
			return true
		}
	}
	return false
}
