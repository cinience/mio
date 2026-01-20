package config

import (
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	git "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
)

// MaybeBootstrapModels clones model artifacts when auto-download is enabled and
// the target directory is empty.
func MaybeBootstrapModels(cfg *Config) error {
	if cfg == nil {
		return nil
	}
	boot := cfg.ModelBootstrap
	if !boot.AutoDownload {
		return nil
	}
	repo := strings.TrimSpace(boot.GitRepo)
	if repo == "" {
		return fmt.Errorf("models.git_repo is required")
	}
	target := strings.TrimSpace(boot.TargetDir)
	if target == "" {
		target = "./models"
	}
	info, err := os.Stat(target)
	if err == nil {
		if !info.IsDir() {
			return fmt.Errorf("models target %s is not a directory", target)
		}
		empty, err := dirIsEmpty(target)
		if err != nil {
			return err
		}
		if !empty {
			return nil
		}
		if err := os.RemoveAll(target); err != nil {
			return fmt.Errorf("remove empty models directory %s: %w", target, err)
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return fmt.Errorf("create models parent directory: %w", err)
	}
	cloneOpts := &git.CloneOptions{
		URL:      repo,
		Progress: io.Discard,
	}
	if ref := strings.TrimSpace(boot.GitRef); ref != "" {
		cloneOpts.ReferenceName = plumbing.NewBranchReferenceName(ref)
		cloneOpts.SingleBranch = true
		cloneOpts.Depth = 1
	}
	log.Printf("bootstrapping models from %s into %s", repo, target)
	repository, err := git.PlainClone(target, false, cloneOpts)
	if err != nil {
		if errors.Is(err, git.ErrRepositoryAlreadyExists) {
			return nil
		}
		return fmt.Errorf("clone models repository: %w", err)
	}
	if wt, err := repository.Worktree(); err == nil {
		if err := wt.Checkout(&git.CheckoutOptions{Force: true}); err != nil {
			log.Printf("warning: git checkout failed: %v", err)
		}
	}
	if err := runGitLFSPull(target); err != nil {
		log.Printf("warning: git lfs pull failed: %v", err)
	}
	return nil
}

func dirIsEmpty(path string) (bool, error) {
	entries, err := os.ReadDir(path)
	if err != nil {
		return false, err
	}
	return len(entries) == 0, nil
}

func runGitLFSPull(dir string) error {
	if _, err := exec.LookPath("git"); err != nil {
		return fmt.Errorf("git binary not found: %w", err)
	}
	cmd := exec.Command("git", "lfs", "install", "--local")
	cmd.Dir = dir
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("git lfs install: %w", err)
	}
	cmd = exec.Command("git", "lfs", "pull")
	cmd.Dir = dir
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("git lfs pull: %w", err)
	}
	return nil
}
