package docker

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Finsys/hawser/internal/log"
)

// FileToDelete is a deletion request from Dockhand's git sync (#966).
// The hash is the sha256 of the content Dockhand originally wrote; the file
// is only removed when the bytes on disk still match (nobody modified it).
type FileToDelete struct {
	Path   string `json:"path"`
	Sha256 string `json:"sha256"`
}

// SkippedFile reports why a requested deletion was not applied.
// Reason codes are shared with Dockhand: locally-modified, load-bearing,
// invalid-path, already-absent, apply-failed.
type SkippedFile struct {
	Path   string `json:"path"`
	Reason string `json:"reason"`
}

// Files that are never auto-deleted, regardless of the request.
var loadBearingFiles = map[string]bool{
	"docker-compose.yml":  true,
	"docker-compose.yaml": true,
	"compose.yml":         true,
	"compose.yaml":        true,
	".env":                true,
}

// isSafeRelPath rejects anything that could escape the stack directory.
func isSafeRelPath(p string) bool {
	if p == "" || filepath.IsAbs(p) || strings.Contains(p, "\\") {
		return false
	}
	for _, seg := range strings.Split(p, "/") {
		if seg == "" || seg == "." || seg == ".." {
			return false
		}
	}
	return true
}

// applyFileDeletions removes files from a stack directory. It is structurally
// incapable of touching anything outside stackDir: every path is
// containment-checked, only regular files whose content still matches the
// recorded hash are removed, and directory cleanup uses non-recursive
// os.Remove so directories holding any other content always survive.
//
// Every requested file produces exactly one entry in either deleted or
// skipped — Dockhand relies on this to detect feature support.
func applyFileDeletions(stackDir string, files []FileToDelete) (deleted []string, skipped []SkippedFile) {
	root, err := filepath.Abs(stackDir)
	if err != nil {
		for _, f := range files {
			skipped = append(skipped, SkippedFile{Path: f.Path, Reason: "apply-failed"})
		}
		return deleted, skipped
	}

	parentDirs := map[string]bool{}
	sep := string(os.PathSeparator)

	for _, f := range files {
		if !isSafeRelPath(f.Path) {
			skipped = append(skipped, SkippedFile{Path: f.Path, Reason: "invalid-path"})
			continue
		}
		if loadBearingFiles[filepath.Base(f.Path)] {
			skipped = append(skipped, SkippedFile{Path: f.Path, Reason: "load-bearing"})
			continue
		}

		abs, err := filepath.Abs(filepath.Join(root, filepath.FromSlash(f.Path)))
		if err != nil || (abs != root && !strings.HasPrefix(abs, root+sep)) || abs == root {
			skipped = append(skipped, SkippedFile{Path: f.Path, Reason: "invalid-path"})
			continue
		}

		stat, err := os.Lstat(abs)
		if err != nil {
			skipped = append(skipped, SkippedFile{Path: f.Path, Reason: "already-absent"})
			continue
		}

		// Dockhand only writes regular files. Anything else (symlink, dir,
		// device) means the user replaced it — treat as locally modified.
		if !stat.Mode().IsRegular() {
			skipped = append(skipped, SkippedFile{Path: f.Path, Reason: "locally-modified"})
			continue
		}

		content, err := os.ReadFile(abs)
		if err != nil {
			skipped = append(skipped, SkippedFile{Path: f.Path, Reason: "apply-failed"})
			continue
		}
		sum := sha256.Sum256(content)
		if hex.EncodeToString(sum[:]) != f.Sha256 {
			skipped = append(skipped, SkippedFile{Path: f.Path, Reason: "locally-modified"})
			continue
		}

		if err := os.Remove(abs); err != nil {
			skipped = append(skipped, SkippedFile{Path: f.Path, Reason: "apply-failed"})
			continue
		}
		deleted = append(deleted, f.Path)
		log.Infof("Deletion sync: removed %q — deleted from the repository", f.Path)

		// Collect parent dir chain (inside root) for empty-dir cleanup
		dir := filepath.Dir(abs)
		for dir != root && strings.HasPrefix(dir, root+sep) {
			parentDirs[dir] = true
			dir = filepath.Dir(dir)
		}
	}

	// Deepest-first; os.Remove fails harmlessly on non-empty directories
	dirs := make([]string, 0, len(parentDirs))
	for d := range parentDirs {
		dirs = append(dirs, d)
	}
	sort.Slice(dirs, func(i, j int) bool { return len(dirs[i]) > len(dirs[j]) })
	for _, d := range dirs {
		_ = os.Remove(d)
	}

	for _, s := range skipped {
		if s.Reason == "already-absent" {
			continue
		}
		log.Warnf("Deletion sync: kept %q (%s)", s.Path, s.Reason)
	}

	return deleted, skipped
}
