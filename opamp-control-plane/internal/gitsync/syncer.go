// Package gitsync provides Git repository synchronization.
package gitsync

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/transport"
	"github.com/go-git/go-git/v5/plumbing/transport/http"
	"github.com/go-git/go-git/v5/plumbing/transport/ssh"
)

// SyncCallback is called after a successful sync.
type SyncCallback func(commitHash string) error

// Syncer synchronizes a Git repository.
type Syncer struct {
	repoURL      string
	branch       string
	localPath    string
	pollInterval time.Duration
	auth         transport.AuthMethod
	logger       *slog.Logger

	repo       *git.Repository
	lastCommit string
	callbacks  []SyncCallback
	mu         sync.RWMutex

	stopCh chan struct{}
	doneCh chan struct{}
}

// SyncerConfig contains configuration for the Git syncer.
type SyncerConfig struct {
	RepoURL      string
	Branch       string
	LocalPath    string
	PollInterval time.Duration
	Username     string
	Password     string
	SSHKeyPath   string
	Logger       *slog.Logger
}

// NewSyncer creates a new Git syncer.
func NewSyncer(cfg SyncerConfig) (*Syncer, error) {
	s := &Syncer{
		repoURL:      cfg.RepoURL,
		branch:       cfg.Branch,
		localPath:    cfg.LocalPath,
		pollInterval: cfg.PollInterval,
		logger:       cfg.Logger,
		stopCh:       make(chan struct{}),
		doneCh:       make(chan struct{}),
	}

	// Configure authentication
	if cfg.Username != "" && cfg.Password != "" {
		s.auth = &http.BasicAuth{
			Username: cfg.Username,
			Password: cfg.Password,
		}
	} else if cfg.SSHKeyPath != "" {
		auth, err := ssh.NewPublicKeysFromFile("git", cfg.SSHKeyPath, "")
		if err != nil {
			return nil, fmt.Errorf("failed to load SSH key: %w", err)
		}
		s.auth = auth
	}

	if s.branch == "" {
		s.branch = "main"
	}

	return s, nil
}

// OnSync registers a callback to be called after successful sync.
func (s *Syncer) OnSync(cb SyncCallback) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.callbacks = append(s.callbacks, cb)
}

// Sync performs a single sync operation.
func (s *Syncer) Sync(ctx context.Context) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var commitHash string

	if s.repo == nil {
		// Clone the repository
		s.logger.Info("cloning repository", "url", s.repoURL, "path", s.localPath)

		// Ensure parent directory exists
		if err := os.MkdirAll(s.localPath, 0755); err != nil {
			return "", fmt.Errorf("failed to create directory: %w", err)
		}

		repo, err := git.PlainCloneContext(ctx, s.localPath, false, &git.CloneOptions{
			URL:           s.repoURL,
			Auth:          s.auth,
			ReferenceName: plumbing.NewBranchReferenceName(s.branch),
			SingleBranch:  true,
			Depth:         1,
		})
		if err != nil {
			return "", fmt.Errorf("failed to clone repository: %w", err)
		}

		s.repo = repo
		commitHash, err = s.getCurrentCommit()
		if err != nil {
			return "", err
		}
	} else {
		// Pull latest changes
		worktree, err := s.repo.Worktree()
		if err != nil {
			return "", fmt.Errorf("failed to get worktree: %w", err)
		}

		err = worktree.PullContext(ctx, &git.PullOptions{
			RemoteName:    "origin",
			ReferenceName: plumbing.NewBranchReferenceName(s.branch),
			Auth:          s.auth,
			SingleBranch:  true,
		})

		if err != nil && err != git.NoErrAlreadyUpToDate {
			return "", fmt.Errorf("failed to pull: %w", err)
		}

		commitHash, err = s.getCurrentCommit()
		if err != nil {
			return "", err
		}
	}

	// Check if commit changed
	if commitHash != s.lastCommit {
		s.logger.Info("repository updated", "commit", commitHash, "previous", s.lastCommit)
		s.lastCommit = commitHash

		// Notify callbacks (outside the lock)
		go s.notifyCallbacks(commitHash)
	}

	return commitHash, nil
}

// getCurrentCommit returns the current HEAD commit hash.
func (s *Syncer) getCurrentCommit() (string, error) {
	ref, err := s.repo.Head()
	if err != nil {
		return "", fmt.Errorf("failed to get HEAD: %w", err)
	}
	return ref.Hash().String(), nil
}

// notifyCallbacks calls all registered callbacks.
func (s *Syncer) notifyCallbacks(commitHash string) {
	s.mu.RLock()
	callbacks := make([]SyncCallback, len(s.callbacks))
	copy(callbacks, s.callbacks)
	s.mu.RUnlock()

	for _, cb := range callbacks {
		if err := cb(commitHash); err != nil {
			s.logger.Error("sync callback failed", "error", err)
		}
	}
}

// Start begins periodic sync.
func (s *Syncer) Start(ctx context.Context) error {
	// Do initial sync
	if _, err := s.Sync(ctx); err != nil {
		return fmt.Errorf("initial sync failed: %w", err)
	}

	// Start periodic sync if interval is set
	if s.pollInterval > 0 {
		go s.pollLoop(ctx)
	}

	return nil
}

// pollLoop runs periodic sync operations.
func (s *Syncer) pollLoop(ctx context.Context) {
	defer close(s.doneCh)

	ticker := time.NewTicker(s.pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-s.stopCh:
			return
		case <-ticker.C:
			if _, err := s.Sync(ctx); err != nil {
				s.logger.Error("periodic sync failed", "error", err)
			}
		}
	}
}

// Stop stops the periodic sync.
func (s *Syncer) Stop() {
	close(s.stopCh)
	<-s.doneCh
}

// GetLocalPath returns the local repository path.
func (s *Syncer) GetLocalPath() string {
	return s.localPath
}

// GetLastCommit returns the last synced commit hash.
func (s *Syncer) GetLastCommit() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.lastCommit
}

// InitLocal initializes from a local directory (for testing).
func (s *Syncer) InitLocal(path string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	repo, err := git.PlainOpen(path)
	if err != nil {
		return fmt.Errorf("failed to open local repository: %w", err)
	}

	s.repo = repo
	s.localPath = path

	commit, err := s.getCurrentCommit()
	if err != nil {
		return err
	}
	s.lastCommit = commit

	return nil
}
