// Package repositorylock manages process-lifetime advisory locks for selected
// repository checkouts. Locks are keyed by canonical checkout root, not by the
// workspace or the repository's configured display name.
package repositorylock

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"syscall"
	"time"
)

const (
	lockDirectory = "repository-locks"
	guardName     = ".acquire"
)

// Target identifies one selected checkout and the workspace run acquiring it.
type Target struct {
	Root  string
	RunID string
}

type metadata struct {
	Root      string `json:"root"`
	RunID     string `json:"run_id"`
	PID       int    `json:"pid"`
	CreatedAt string `json:"created_at"`
}

var lockRoot = defaultLockRoot

func defaultLockRoot() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".agent-runner"), nil
}

// AcquireAll atomically acquires every selected root or leaves none acquired
// by this attempt. The brief registry guard serializes acquisition groups;
// durable per-root files remain until their owning process exits.
func AcquireAll(targets []Target) error {
	if len(targets) == 0 {
		return nil
	}
	base, err := lockRoot()
	if err != nil {
		return fmt.Errorf("resolve repository lock directory: %w", err)
	}
	directory := filepath.Join(base, lockDirectory)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return fmt.Errorf("create repository lock directory: %w", err)
	}

	canonicalTargets := make([]Target, 0, len(targets))
	seen := make(map[string]bool, len(targets))
	for _, target := range targets {
		root, err := canonicalRoot(target.Root)
		if err != nil {
			return fmt.Errorf("canonicalize repository lock root %q: %w", target.Root, err)
		}
		if seen[root] {
			continue
		}
		seen[root] = true
		canonicalTargets = append(canonicalTargets, Target{Root: root, RunID: target.RunID})
	}
	sort.Slice(canonicalTargets, func(i, j int) bool { return canonicalTargets[i].Root < canonicalTargets[j].Root })

	guardPath := filepath.Join(directory, guardName)
	if err := acquireTransientGuard(guardPath); err != nil {
		return err
	}
	defer func() { _ = os.Remove(guardPath) }()

	acquired := make([]string, 0, len(canonicalTargets))
	for _, target := range canonicalTargets {
		path := filepath.Join(directory, lockName(target.Root))
		if err := acquireOne(path, target); err != nil {
			for _, acquiredPath := range acquired {
				_ = os.Remove(acquiredPath)
			}
			return err
		}
		acquired = append(acquired, path)
	}
	return nil
}

func canonicalRoot(root string) (string, error) {
	absolute, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	canonical, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(canonical)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", errors.New("not a directory")
	}
	return canonical, nil
}

func lockName(root string) string {
	digest := sha256.Sum256([]byte(root))
	return hex.EncodeToString(digest[:]) + ".lock"
}

func acquireTransientGuard(path string) error {
	for attempt := 0; attempt < 50; attempt++ {
		if err := createLock(path, metadata{PID: os.Getpid(), CreatedAt: time.Now().UTC().Format(time.RFC3339Nano)}); err == nil {
			return nil
		} else if !errors.Is(err, fs.ErrExist) {
			return fmt.Errorf("acquire repository lock registry: %w", err)
		}
		owner, err := readMetadata(path)
		if err == nil && processLive(owner.PID) {
			time.Sleep(10 * time.Millisecond)
			continue
		}
		if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("recover stale repository lock registry: %w", err)
		}
	}
	return errors.New("repository lock registry is busy")
}

func acquireOne(path string, target Target) error {
	owner := metadata{Root: target.Root, RunID: target.RunID, PID: os.Getpid(), CreatedAt: time.Now().UTC().Format(time.RFC3339Nano)}
	if err := createLock(path, owner); err == nil {
		return nil
	} else if !errors.Is(err, fs.ErrExist) {
		return fmt.Errorf("acquire repository lock for %s: %w", target.Root, err)
	}

	existing, err := readMetadata(path)
	if err == nil && processLive(existing.PID) {
		if existing.PID == os.Getpid() && existing.RunID == target.RunID {
			return nil
		}
		return fmt.Errorf("repository %s is locked by run %q (PID %d)", existing.Root, existing.RunID, existing.PID)
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("recover stale lock for repository %s: %w", target.Root, err)
	}
	if err := createLock(path, owner); err != nil {
		if errors.Is(err, fs.ErrExist) {
			latest, readErr := readMetadata(path)
			if readErr == nil {
				return fmt.Errorf("repository %s is locked by run %q (PID %d)", latest.Root, latest.RunID, latest.PID)
			}
		}
		return fmt.Errorf("acquire recovered repository lock for %s: %w", target.Root, err)
	}
	return nil
}

func createLock(path string, owner metadata) error {
	data, err := json.Marshal(owner)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".repository-lock-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer func() { _ = os.Remove(tmpPath) }()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Link(tmpPath, path)
}

func readMetadata(path string) (metadata, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return metadata{}, err
	}
	var owner metadata
	if err := json.Unmarshal(data, &owner); err != nil {
		return metadata{}, err
	}
	return owner, nil
}

func processLive(pid int) bool {
	if pid <= 0 {
		return false
	}
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	err = process.Signal(syscall.Signal(0))
	// Permission errors still prove that another live process owns the PID;
	// every other signal error is treated as stale so malformed/defunct owner
	// records can be recovered.
	return err == nil || errors.Is(err, syscall.EPERM)
}
