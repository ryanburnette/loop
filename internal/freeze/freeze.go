// Package freeze snapshots and checks frozen file patterns.
package freeze

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Snapshot walks root for files matching patterns (basename globs) and
// writes checksums under stateDir. Patterns may be empty.
func Snapshot(root, stateDir string, patterns []string) error {
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		return err
	}
	indexPath := filepath.Join(stateDir, "index")
	if err := os.WriteFile(indexPath, []byte(strings.Join(patterns, "\n")+"\n"), 0o644); err != nil {
		return err
	}
	// Clear old .sum files.
	entries, err := os.ReadDir(stateDir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".sum") || strings.HasSuffix(e.Name(), ".now") {
			_ = os.Remove(filepath.Join(stateDir, e.Name()))
		}
	}
	ignoreDir := filepath.Clean(filepath.Dir(stateDir)) // run state dir
	for i, pat := range patterns {
		files, err := matchFiles(root, pat, ignoreDir, stateDir)
		if err != nil {
			return err
		}
		sumPath := filepath.Join(stateDir, fmt.Sprintf("%d.sum", i+1))
		if err := writeSums(sumPath, root, files); err != nil {
			return err
		}
	}
	return nil
}

// Check re-hashes frozen patterns and reports drift.
func Check(root, stateDir string) error {
	indexPath := filepath.Join(stateDir, "index")
	b, err := os.ReadFile(indexPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	var patterns []string
	sc := bufio.NewScanner(strings.NewReader(string(b)))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line != "" {
			patterns = append(patterns, line)
		}
	}
	if err := sc.Err(); err != nil {
		return err
	}
	ignoreDir := filepath.Clean(filepath.Dir(stateDir))
	var drifts []string
	for i, pat := range patterns {
		files, err := matchFiles(root, pat, ignoreDir, stateDir)
		if err != nil {
			return err
		}
		nowPath := filepath.Join(stateDir, fmt.Sprintf("%d.now", i+1))
		if err := writeSums(nowPath, root, files); err != nil {
			return err
		}
		sumPath := filepath.Join(stateDir, fmt.Sprintf("%d.sum", i+1))
		ok, err := filesEqual(sumPath, nowPath)
		if err != nil {
			return err
		}
		if !ok {
			drifts = append(drifts, pat)
		}
	}
	if len(drifts) > 0 {
		return fmt.Errorf("freeze drift: %s", strings.Join(drifts, ", "))
	}
	return nil
}

func matchFiles(root, pattern, ignoreDir, stateDir string) ([]string, error) {
	var out []string
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		// Skip .git
		if d.IsDir() && (d.Name() == ".git" || rel == ".git") {
			return filepath.SkipDir
		}
		// Skip run state directory (parent of frozen/) and the frozen dir itself.
		clean := filepath.Clean(path)
		if d.IsDir() {
			if clean == ignoreDir || clean == filepath.Clean(stateDir) {
				return filepath.SkipDir
			}
			// Also skip any top-level path named state if ignoreDir is under it?
			// v1: -not -path "$g_state/*"
		}
		if strings.HasPrefix(clean, ignoreDir+string(os.PathSeparator)) || clean == ignoreDir {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			return nil
		}
		ok, err := filepath.Match(pattern, d.Name())
		if err != nil {
			return err
		}
		if ok {
			out = append(out, path)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(out)
	return out, nil
}

func writeSums(path, root string, files []string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	for _, file := range files {
		sum, err := hashFile(file)
		if err != nil {
			return err
		}
		// Store relative path for stable comparison.
		rel, err := filepath.Rel(root, file)
		if err != nil {
			rel = file
		}
		if _, err := fmt.Fprintf(f, "%s  %s\n", sum, rel); err != nil {
			return err
		}
	}
	return nil
}

func hashFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func filesEqual(a, b string) (bool, error) {
	ab, err := os.ReadFile(a)
	if err != nil {
		return false, err
	}
	bb, err := os.ReadFile(b)
	if err != nil {
		return false, err
	}
	return string(ab) == string(bb), nil
}
