// Package freeze snapshots and checks frozen file patterns.
package freeze

import (
	"bufio"
	"bytes"
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
// writes checksums under stateDir. Patterns may be empty. The loop's own
// state tree and common build/tool output directories are pruned so a broad
// pattern does not hash gigabytes of generated files.
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

// Baseline is the freeze snapshot held in memory. The sum files live in the
// workroot, so a turn can edit them; checks compare against this copy.
type Baseline struct {
	index    []byte
	sums     [][]byte
	parsed   []map[string]string
	patterns []string
}

// Load reads index and each *.sum into memory. A missing index fails closed.
// An empty index (no patterns) is a real snapshot of nothing.
func Load(stateDir string) (Baseline, error) {
	b, err := os.ReadFile(filepath.Join(stateDir, "index"))
	if err != nil {
		if os.IsNotExist(err) {
			return Baseline{}, fmt.Errorf("freeze index missing")
		}
		return Baseline{}, err
	}
	patterns, err := parsePatterns(b)
	if err != nil {
		return Baseline{}, err
	}
	base := Baseline{
		index:    b,
		patterns: patterns,
		sums:     make([][]byte, len(patterns)),
		parsed:   make([]map[string]string, len(patterns)),
	}
	for i := range patterns {
		sb, err := os.ReadFile(filepath.Join(stateDir, fmt.Sprintf("%d.sum", i+1)))
		if err != nil {
			if os.IsNotExist(err) {
				return Baseline{}, fmt.Errorf("freeze sum missing")
			}
			return Baseline{}, err
		}
		base.sums[i] = sb
		base.parsed[i] = parseSums(sb)
	}
	return base, nil
}

// Check loads the on-disk snapshot and compares the worktree to that load.
// A missing index fails closed. An empty index means nothing is frozen.
// The runner must not call this per gate: it re-reads *.sum, so a turn that
// rewrote those files would be graded against its own edit. Hold a Baseline
// from startup (or resume) and call Baseline.Check.
func Check(root, stateDir string) error {
	base, err := Load(stateDir)
	if err != nil {
		return err
	}
	return base.Check(root, stateDir)
}

// Check hashes the worktree and compares it to the loaded baseline. It reads
// index and *.sum only to see if they still match that load. A missing index
// is "freeze index missing". A changed index or *.sum is "freeze store
// modified". File drift is reported from the in-memory hashes.
func (b Baseline) Check(root, stateDir string) error {
	got, err := os.ReadFile(filepath.Join(stateDir, "index"))
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("freeze index missing")
		}
		return err
	}
	if !bytes.Equal(got, b.index) {
		return fmt.Errorf("freeze store modified")
	}
	for i, want := range b.sums {
		sb, err := os.ReadFile(filepath.Join(stateDir, fmt.Sprintf("%d.sum", i+1)))
		if err != nil {
			if os.IsNotExist(err) {
				return fmt.Errorf("freeze store modified")
			}
			return err
		}
		if !bytes.Equal(sb, want) {
			return fmt.Errorf("freeze store modified")
		}
	}

	ignoreDir := filepath.Clean(filepath.Dir(stateDir))
	var entries []string
	for i, pat := range b.patterns {
		files, err := matchFiles(root, pat, ignoreDir, stateDir)
		if err != nil {
			return err
		}
		cur, err := sumMap(root, files)
		if err != nil {
			return err
		}
		names := diffSumMaps(b.parsed[i], cur)
		if len(names) == 0 {
			continue
		}
		// Name the drifted file(s), not just the glob, so the user knows
		// which file moved. The pattern stays in parentheses for context.
		entries = append(entries, fmt.Sprintf("%s (pattern %s)", strings.Join(names, ", "), pat))
	}
	if len(entries) > 0 {
		return fmt.Errorf("freeze drift: %s", strings.Join(entries, ", "))
	}
	return nil
}

func parsePatterns(b []byte) ([]string, error) {
	var patterns []string
	sc := bufio.NewScanner(strings.NewReader(string(b)))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line != "" {
			patterns = append(patterns, line)
		}
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return patterns, nil
}

func parseSums(b []byte) map[string]string {
	m := map[string]string{}
	text := strings.TrimRight(string(b), "\n")
	if text == "" {
		return m
	}
	for _, line := range strings.Split(text, "\n") {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "  ", 2)
		if len(parts) != 2 {
			continue
		}
		m[parts[1]] = parts[0]
	}
	return m
}

func sumMap(root string, files []string) (map[string]string, error) {
	m := make(map[string]string, len(files))
	for _, file := range files {
		sum, err := hashFile(file)
		if err != nil {
			return nil, err
		}
		rel, err := filepath.Rel(root, file)
		if err != nil {
			rel = file
		}
		m[rel] = sum
	}
	return m, nil
}

func diffSumMaps(old, cur map[string]string) []string {
	var drift []string
	seen := map[string]bool{}
	for path, h := range old {
		seen[path] = true
		got, ok := cur[path]
		if !ok || got != h {
			drift = append(drift, path)
		}
	}
	for path := range cur {
		if !seen[path] {
			drift = append(drift, path)
		}
	}
	sort.Strings(drift)
	return drift
}

// buildSkipDirs are directory basenames that hold generated build/tool output.
// They are pruned unconditionally so a broad freeze pattern (e.g. "*" or
// "*.go") does not hash gigabytes of vendored or compiled files. These are
// output locations, not source directories, by widespread convention.
var buildSkipDirs = map[string]bool{
	"node_modules":  true,
	"vendor":        true,
	"dist":          true,
	"build":         true,
	"out":           true,
	"target":        true,
	"bin":           true,
	"coverage":      true,
	"__pycache__":   true,
	".turbo":        true,
	".next":         true,
	"tmp":           true,
	"venv":          true,
	".venv":         true,
	".cache":        true,
	".pytest_cache": true,
	".mypy_cache":   true,
}

func matchFiles(root, pattern, ignoreDir, stateDir string) ([]string, error) {
	// The loop's state tree is the parent of the run-state dir (ignoreDir is
	// the parent of the frozen dir: state/<id> for a run, state/.freeze-tmp
	// for a manual `loop freeze`). Prune the whole state/ tree in both cases
	// so a manual snapshot excludes existing run-state dirs just as a run
	// excludes its own — the two paths stay aligned.
	stateRoot := filepath.Clean(filepath.Dir(ignoreDir))
	pruneStateRoot := stateRoot != root && stateRoot != filepath.Clean(ignoreDir) && filepath.Base(stateRoot) == "state"
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
		// Skip generated build/tool output directories.
		if d.IsDir() && buildSkipDirs[d.Name()] {
			return filepath.SkipDir
		}
		clean := filepath.Clean(path)
		// Skip the run-state directory (parent of frozen/), the frozen dir
		// itself, and — when it is the loop's own state tree — the whole
		// state/ directory so sibling run states are excluded too.
		if d.IsDir() {
			if clean == ignoreDir || clean == filepath.Clean(stateDir) {
				return filepath.SkipDir
			}
			if pruneStateRoot && clean == stateRoot {
				return filepath.SkipDir
			}
		}
		if strings.HasPrefix(clean, ignoreDir+string(os.PathSeparator)) || clean == ignoreDir {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if pruneStateRoot && (strings.HasPrefix(clean, stateRoot+string(os.PathSeparator))) {
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
