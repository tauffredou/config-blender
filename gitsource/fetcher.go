// Package gitsource fetches layer content from Git (CONCEPTION.md section
// 9): a Recipe stored in configblender's database references each layer's
// content by repo/path/ref rather than embedding it, so the content itself
// stays versioned and review-able in a GitOps flow.
package gitsource

import (
	"errors"
	"fmt"
	"sync"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/transport"
	"github.com/go-git/go-git/v5/storage/memory"
)

// Source locates one layer's content in a Git repository.
type Source struct {
	// Repo is the repository URL (or a local path, mainly for testing).
	Repo string `yaml:"repo" json:"repo"`
	// Path is the file's path within the repository.
	Path string `yaml:"path" json:"path"`
	// Ref is a branch, tag, or commit SHA. Required — there is no implicit
	// default branch, so a Recipe is explicit about what it resolves to.
	Ref string `yaml:"ref" json:"ref"`
}

// AuthResolver returns the credentials to use for a given repo URL, or nil
// for no authentication. Repo credentials are configblender's own
// operational secret (distinct from application config/secrets, section
// 5.1) — how they reach the resolver (env, mounted K8s Secret...) is left
// to the caller.
type AuthResolver func(repoURL string) transport.AuthMethod

// Fetcher retrieves layer content from Git, keeping one in-memory clone per
// repository and updating it with a fetch on every call — cheap enough at
// the refreshInterval cadence (section 5.5) and avoids any on-disk cache to
// manage (fits a read-only-rootfs controller pod, section 5.4).
type Fetcher struct {
	mu    sync.Mutex
	repos map[string]*git.Repository
	auth  AuthResolver
}

func NewFetcher(auth AuthResolver) *Fetcher {
	return &Fetcher{
		repos: make(map[string]*git.Repository),
		auth:  auth,
	}
}

// Content returns the file content of src at its ref, cloning or updating
// the repository's in-memory cache as needed.
func (f *Fetcher) Content(src Source) (string, error) {
	if src.Ref == "" {
		return "", fmt.Errorf("git source %s%s: ref is required", src.Repo, src.Path)
	}

	repo, err := f.repo(src.Repo)
	if err != nil {
		return "", err
	}

	hash, err := resolveRevision(repo, src.Ref)
	if err != nil {
		return "", fmt.Errorf("git source %s@%s: %w", src.Repo, src.Ref, err)
	}

	commit, err := repo.CommitObject(*hash)
	if err != nil {
		return "", fmt.Errorf("git source %s@%s: loading commit: %w", src.Repo, src.Ref, err)
	}

	file, err := commit.File(src.Path)
	if err != nil {
		return "", fmt.Errorf("git source %s@%s: reading %q: %w", src.Repo, src.Ref, src.Path, err)
	}

	content, err := file.Contents()
	if err != nil {
		return "", fmt.Errorf("git source %s@%s: reading %q: %w", src.Repo, src.Ref, src.Path, err)
	}
	return content, nil
}

func (f *Fetcher) repo(repoURL string) (*git.Repository, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	var auth transport.AuthMethod
	if f.auth != nil {
		auth = f.auth(repoURL)
	}

	if repo, ok := f.repos[repoURL]; ok {
		err := repo.Fetch(&git.FetchOptions{Auth: auth, Force: true})
		if err != nil && !errors.Is(err, git.NoErrAlreadyUpToDate) {
			return nil, fmt.Errorf("fetching %s: %w", repoURL, err)
		}
		return repo, nil
	}

	repo, err := git.Clone(memory.NewStorage(), nil, &git.CloneOptions{
		URL:  repoURL,
		Auth: auth,
	})
	if err != nil {
		return nil, fmt.Errorf("cloning %s: %w", repoURL, err)
	}
	f.repos[repoURL] = repo
	return repo, nil
}

// resolveRevision prefers the remote-tracking ref for a branch name: a
// Fetch updates refs/remotes/origin/* but leaves the local refs/heads/*
// snapshot from clone time untouched (standard git behavior), so resolving
// the local ref first would silently serve stale content after an update.
// The raw ref is tried as a fallback for tags, HEAD, and commit SHAs, which
// have no remote-tracking counterpart.
func resolveRevision(repo *git.Repository, ref string) (*plumbing.Hash, error) {
	if h, err := repo.ResolveRevision(plumbing.Revision("refs/remotes/origin/" + ref)); err == nil {
		return h, nil
	}
	if h, err := repo.ResolveRevision(plumbing.Revision(ref)); err == nil {
		return h, nil
	}
	return nil, fmt.Errorf("revision %q not found", ref)
}
