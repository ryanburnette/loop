package run

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/ryanburnette/loop/internal/config"
	"github.com/ryanburnette/loop/internal/loopdir"
	"github.com/ryanburnette/loop/internal/manifest"
	"github.com/ryanburnette/loop/internal/mend"
	"github.com/ryanburnette/loop/internal/scorecard"
)

// stallSnap is one iteration's failure signature and tree. NotOK is persisted
// so an ok iteration cannot arm the next empty signature. Seen is in-memory
// only: the file is what resume reads, and a turn can edit that file. During
// this process the previous snapshot stays the copy loaded at start or saved
// at the end of the last iteration. Stall is a cost control, not an anti-cheat.
type stallSnap struct {
	Seen  bool
	Iter  int
	NotOK bool
	Sig   string
	Tree  string
	Head  string
}

type stallFile struct {
	Iter  int    `json:"iter"`
	NotOK bool   `json:"not_ok"`
	Sig   string `json:"sig"`
	Tree  string `json:"tree"`
	Head  string `json:"head"`
}

// stallMissing is the token for a LOOP_STALL_PATHS entry that is not on disk.
// Absence has to compare equal across iterations, and it has to differ from a hash.
const stallMissing = "missing"

func (rr *runner) noteRequiredGate(step manifest.Step, ok bool, exit int, log string) {
	if !step.Required || ok {
		return
	}
	s := fmt.Sprintf("gate %s exit %d", step.Name, exit)
	log = strings.ReplaceAll(log, "\r\n", "\n")
	for _, line := range strings.Split(log, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		s += ": " + line
		break
	}
	rr.failParts = append(rr.failParts, s)
}

func (rr *runner) noteRequiredScore(step manifest.Step, readable, passed bool, marks []scorecard.Mark) {
	if !step.Required || (readable && passed) {
		return
	}
	if !readable {
		rr.failParts = append(rr.failParts, "scorecard "+step.Name+": unreadable")
		return
	}
	var ids []string
	for _, m := range marks {
		if m.Value < m.Max {
			ids = append(ids, m.ID)
		}
	}
	rr.failParts = append(rr.failParts, "scorecard "+step.Name+": "+strings.Join(ids, ","))
}

// stall decides whether this not-ok iteration repeats the previous not-ok one.
// It records the snapshot so resume does not grant two fresh iterations.
// A git status failure drops that baseline instead of comparing against it later.
func (rr *runner) stall(iter int, iterOK bool) (bool, error) {
	snap, err := rr.stallSnapshot(iter, !iterOK)
	path := filepath.Join(rr.stateDir, "stall.json")
	if err != nil {
		// The next comparison must not use a non-adjacent snapshot, including
		// the file resume would load.
		rr.prevStall = stallSnap{}
		_ = os.Remove(path)
		rr.r.Warn("stall check skipped: " + err.Error())
		return false, nil
	}
	if iter >= 2 && !iterOK && rr.prevStall.NotOK && rr.cfg.Stall != config.StallContinue && rr.prevStall.Seen &&
		snap.Sig == rr.prevStall.Sig && snap.Tree == rr.prevStall.Tree {
		rr.stallSig = snap.Sig
		rr.stallHead = snap.Head
		if err := writeStall(path, snap); err != nil {
			return false, err
		}
		if err := rr.recordResult(mend.ResultStalled, iter); err != nil {
			return false, err
		}
		rr.writeStatus(iter, "stalled")
		return true, nil
	}
	if err := writeStall(path, snap); err != nil {
		return false, err
	}
	rr.prevStall = snap
	return false, nil
}

func (rr *runner) stallSnapshot(iter int, notOK bool) (stallSnap, error) {
	head := gitHEAD(rr.workroot)
	if head == "" {
		head = "unknown"
	}
	porcelain, err := progressPorcelain(rr.workroot, rr.loopDir)
	if err != nil {
		return stallSnap{}, err
	}
	var b strings.Builder
	fmt.Fprintf(&b, "head %s\n", head)
	b.WriteString("porcelain\n")
	b.WriteString(porcelain)
	b.WriteString("paths\n")
	for _, rel := range rr.cfg.StallPaths {
		fmt.Fprintf(&b, "%s %s\n", rel, stallPathToken(rr.workroot, rel))
	}
	return stallSnap{
		Seen:  true,
		Iter:  iter,
		NotOK: notOK,
		Sig:   strings.Join(rr.failParts, "; "),
		Tree:  b.String(),
		Head:  head,
	}, nil
}

func stallPathToken(workroot, rel string) string {
	p := rel
	if !filepath.IsAbs(rel) {
		p = filepath.Join(workroot, rel)
	}
	b, err := os.ReadFile(p)
	if err != nil {
		if os.IsNotExist(err) {
			return stallMissing
		}
		return "unreadable"
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func writeStall(path string, s stallSnap) error {
	b, err := json.MarshalIndent(stallFile{
		Iter:  s.Iter,
		NotOK: s.NotOK,
		Sig:   s.Sig,
		Tree:  s.Tree,
		Head:  s.Head,
	}, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	return os.WriteFile(path, b, 0o644)
}

func loadStall(path string) (stallSnap, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return stallSnap{}, nil
		}
		return stallSnap{}, err
	}
	var f stallFile
	if err := json.Unmarshal(b, &f); err != nil {
		return stallSnap{}, err
	}
	return stallSnap{Seen: true, Iter: f.Iter, NotOK: f.NotOK, Sig: f.Sig, Tree: f.Tree, Head: f.Head}, nil
}

// dirtyWorktreePath is the first porcelain path setupBranch refuses.
// Untracked files inside the loop dir are the recipe, not the user's work.
// An empty path means the tree is clean enough to branch.
func dirtyWorktreePath(workroot, loopDir string) (string, error) {
	lines, err := gitPorcelain(workroot)
	if err != nil {
		return "", err
	}
	tolerated := recipeTolerated(workroot, loopDir)
	for _, line := range lines {
		status, path, ok := porcelainFields(line)
		if !ok {
			continue
		}
		if status == "??" && recipePath(path, tolerated) {
			continue
		}
		return path, nil
	}
	return "", nil
}

// progressPorcelain is git status --porcelain minus untracked recipe paths.
// .loop/ state changes every iteration; that is not progress. Ignored paths
// are absent here unless git is asked to list them, which it is not.
func progressPorcelain(workroot, loopDir string) (string, error) {
	lines, err := gitPorcelain(workroot)
	if err != nil {
		return "", err
	}
	tolerated := recipeTolerated(workroot, loopDir)
	var b strings.Builder
	for _, line := range lines {
		status, path, ok := porcelainFields(line)
		if !ok {
			continue
		}
		if status == "??" && recipePath(path, tolerated) {
			continue
		}
		b.WriteString(line)
		b.WriteByte('\n')
	}
	return b.String(), nil
}

func gitPorcelain(workroot string) ([]string, error) {
	out, err := exec.Command("git", "-C", workroot, "status", "--porcelain").Output()
	if err != nil {
		return nil, err
	}
	var lines []string
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimRight(line, "\r")
		if line == "" {
			continue
		}
		lines = append(lines, line)
	}
	return lines, nil
}

func porcelainFields(line string) (status, path string, ok bool) {
	if len(line) < 3 {
		return "", "", false
	}
	return line[:2], strings.TrimRight(line[3:], "/"), true
}

func recipeTolerated(workroot, loopDir string) map[string]bool {
	tolerated := map[string]bool{loopdir.DefaultDir: true}
	realWorkroot, err := filepath.EvalSymlinks(workroot)
	if err != nil || realWorkroot == "" {
		realWorkroot = workroot
	}
	realLoopDir, err := filepath.EvalSymlinks(loopDir)
	if err != nil || realLoopDir == "" {
		realLoopDir = loopDir
	}
	if rel, err := filepath.Rel(realWorkroot, realLoopDir); err == nil {
		rel = filepath.ToSlash(rel)
		if rel != "." && !strings.HasPrefix(rel, "../") {
			tolerated[rel] = true
		}
	}
	return tolerated
}

func recipePath(path string, tolerated map[string]bool) bool {
	for rel := range tolerated {
		if path == rel || strings.HasPrefix(path, rel+"/") {
			return true
		}
	}
	return false
}
