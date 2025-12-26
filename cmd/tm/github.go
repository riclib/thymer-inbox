package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/go-github/v66/github"
	bolt "go.etcd.io/bbolt"
)

const (
	githubBucket   = "github_issues"
	metaBucket     = "meta"
	syncIntervalKey = "last_sync"
)

// GitHubIssue represents a stored issue/PR
type GitHubIssue struct {
	ID        string    `json:"id"`        // github_owner_repo_123
	Repo      string    `json:"repo"`      // owner/repo
	Number    int       `json:"number"`
	Title     string    `json:"title"`
	Body      string    `json:"body"`
	State     string    `json:"state"`     // open, closed
	Type      string    `json:"type"`      // issue, pull_request
	URL       string    `json:"url"`
	Author    string    `json:"author"`
	Labels    []string  `json:"labels"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
	ClosedAt  *time.Time `json:"closedAt,omitempty"`
	Merged    bool      `json:"merged,omitempty"`
}

// GitHubSyncer handles syncing GitHub issues/PRs
type GitHubSyncer struct {
	client *github.Client
	db     *bolt.DB
	repos  []string
}

// NewGitHubSyncer creates a new syncer
func NewGitHubSyncer(token string, repos []string, dataDir string) (*GitHubSyncer, error) {
	client := github.NewClient(nil).WithAuthToken(token)

	// Open bbolt database
	dbPath := filepath.Join(dataDir, "github.db")
	db, err := bolt.Open(dbPath, 0600, &bolt.Options{Timeout: 1 * time.Second})
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Create buckets
	err = db.Update(func(tx *bolt.Tx) error {
		if _, err := tx.CreateBucketIfNotExists([]byte(githubBucket)); err != nil {
			return err
		}
		if _, err := tx.CreateBucketIfNotExists([]byte(metaBucket)); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to create buckets: %w", err)
	}

	return &GitHubSyncer{
		client: client,
		db:     db,
		repos:  repos,
	}, nil
}

// Close closes the database
func (s *GitHubSyncer) Close() error {
	return s.db.Close()
}

// SyncResult contains sync statistics
type SyncResult struct {
	Created   []GitHubIssue
	Updated   []GitHubIssue
	Unchanged int
	Errors    []error
}

// Sync fetches issues/PRs and returns changes
func (s *GitHubSyncer) Sync(ctx context.Context) (*SyncResult, error) {
	result := &SyncResult{
		Created: make([]GitHubIssue, 0),
		Updated: make([]GitHubIssue, 0),
		Errors:  make([]error, 0),
	}

	for _, repo := range s.repos {
		issues, err := s.syncRepo(ctx, repo)
		if err != nil {
			result.Errors = append(result.Errors, fmt.Errorf("failed to sync %s: %w", repo, err))
			continue
		}

		for _, issue := range issues {
			action, err := s.upsert(issue)
			if err != nil {
				result.Errors = append(result.Errors, err)
				continue
			}

			switch action {
			case "created":
				result.Created = append(result.Created, issue)
			case "updated":
				result.Updated = append(result.Updated, issue)
			case "unchanged":
				result.Unchanged++
			}
		}
	}

	return result, nil
}

func (s *GitHubSyncer) syncRepo(ctx context.Context, repo string) ([]GitHubIssue, error) {
	parts := strings.Split(repo, "/")
	if len(parts) != 2 {
		return nil, fmt.Errorf("invalid repo format: %s", repo)
	}
	owner, name := parts[0], parts[1]

	var issues []GitHubIssue

	// Fetch issues
	issueOpts := &github.IssueListByRepoOptions{
		State: "all",
		ListOptions: github.ListOptions{PerPage: 100},
	}

	ghIssues, _, err := s.client.Issues.ListByRepo(ctx, owner, name, issueOpts)
	if err != nil {
		return nil, fmt.Errorf("failed to list issues: %w", err)
	}

	for _, issue := range ghIssues {
		// Skip pull requests (they have PullRequestLinks)
		if issue.PullRequestLinks != nil {
			continue
		}
		issues = append(issues, s.convertIssue(repo, issue))
	}

	// Fetch PRs
	prOpts := &github.PullRequestListOptions{
		State: "all",
		ListOptions: github.ListOptions{PerPage: 100},
	}

	prs, _, err := s.client.PullRequests.List(ctx, owner, name, prOpts)
	if err != nil {
		return nil, fmt.Errorf("failed to list PRs: %w", err)
	}

	for _, pr := range prs {
		issues = append(issues, s.convertPR(repo, pr))
	}

	return issues, nil
}

func (s *GitHubSyncer) convertIssue(repo string, issue *github.Issue) GitHubIssue {
	repoSlug := strings.ReplaceAll(repo, "/", "_")
	id := fmt.Sprintf("github_%s_%d", repoSlug, issue.GetNumber())

	labels := make([]string, len(issue.Labels))
	for i, label := range issue.Labels {
		labels[i] = label.GetName()
	}

	gi := GitHubIssue{
		ID:        id,
		Repo:      repo,
		Number:    issue.GetNumber(),
		Title:     issue.GetTitle(),
		Body:      issue.GetBody(),
		State:     issue.GetState(),
		Type:      "issue",
		URL:       issue.GetHTMLURL(),
		Labels:    labels,
		CreatedAt: issue.GetCreatedAt().Time,
		UpdatedAt: issue.GetUpdatedAt().Time,
	}

	if issue.GetUser() != nil {
		gi.Author = issue.GetUser().GetLogin()
	}

	if issue.ClosedAt != nil {
		t := issue.ClosedAt.Time
		gi.ClosedAt = &t
	}

	return gi
}

func (s *GitHubSyncer) convertPR(repo string, pr *github.PullRequest) GitHubIssue {
	repoSlug := strings.ReplaceAll(repo, "/", "_")
	id := fmt.Sprintf("github_%s_%d", repoSlug, pr.GetNumber())

	labels := make([]string, len(pr.Labels))
	for i, label := range pr.Labels {
		labels[i] = label.GetName()
	}

	gi := GitHubIssue{
		ID:        id,
		Repo:      repo,
		Number:    pr.GetNumber(),
		Title:     pr.GetTitle(),
		Body:      pr.GetBody(),
		State:     pr.GetState(),
		Type:      "pull_request",
		URL:       pr.GetHTMLURL(),
		Labels:    labels,
		Merged:    pr.GetMerged(),
		CreatedAt: pr.GetCreatedAt().Time,
		UpdatedAt: pr.GetUpdatedAt().Time,
	}

	if pr.GetUser() != nil {
		gi.Author = pr.GetUser().GetLogin()
	}

	if pr.ClosedAt != nil {
		t := pr.ClosedAt.Time
		gi.ClosedAt = &t
	}

	return gi
}

func (s *GitHubSyncer) upsert(issue GitHubIssue) (string, error) {
	var action string

	err := s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(githubBucket))

		existing := b.Get([]byte(issue.ID))
		if existing == nil {
			// New issue
			data, err := json.Marshal(issue)
			if err != nil {
				return err
			}
			action = "created"
			return b.Put([]byte(issue.ID), data)
		}

		// Check if changed
		var old GitHubIssue
		if err := json.Unmarshal(existing, &old); err != nil {
			return err
		}

		if needsUpdate(old, issue) {
			data, err := json.Marshal(issue)
			if err != nil {
				return err
			}
			action = "updated"
			return b.Put([]byte(issue.ID), data)
		}

		action = "unchanged"
		return nil
	})

	return action, err
}

func needsUpdate(old, new GitHubIssue) bool {
	// State changed (open -> closed)
	if old.State != new.State {
		return true
	}
	// Title changed
	if old.Title != new.Title {
		return true
	}
	// Updated timestamp is newer
	if new.UpdatedAt.After(old.UpdatedAt) {
		return true
	}
	return false
}

// GetAll returns all stored issues
func (s *GitHubSyncer) GetAll() ([]GitHubIssue, error) {
	var issues []GitHubIssue

	err := s.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(githubBucket))
		return b.ForEach(func(k, v []byte) error {
			var issue GitHubIssue
			if err := json.Unmarshal(v, &issue); err != nil {
				return err
			}
			issues = append(issues, issue)
			return nil
		})
	})

	return issues, err
}

// StartPeriodicSync runs sync every interval and calls onChange with new/updated issues
func (s *GitHubSyncer) StartPeriodicSync(ctx context.Context, interval time.Duration, onChange func([]GitHubIssue)) {
	ticker := time.NewTicker(interval)

	go func() {
		defer ticker.Stop()

		// Initial sync
		s.doSync(onChange)

		for {
			select {
			case <-ctx.Done():
				log.Println("📡 GitHub sync stopped")
				return
			case <-ticker.C:
				s.doSync(onChange)
			}
		}
	}()

	log.Printf("📡 GitHub sync started (every %v)", interval)
}

func (s *GitHubSyncer) doSync(onChange func([]GitHubIssue)) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	result, err := s.Sync(ctx)
	if err != nil {
		log.Printf("❌ GitHub sync error: %v", err)
		return
	}

	log.Printf("📡 GitHub sync: created=%d updated=%d unchanged=%d errors=%d",
		len(result.Created), len(result.Updated), result.Unchanged, len(result.Errors))

	// Notify about changes
	if len(result.Created) > 0 || len(result.Updated) > 0 {
		changes := append(result.Created, result.Updated...)
		onChange(changes)
	}
}
