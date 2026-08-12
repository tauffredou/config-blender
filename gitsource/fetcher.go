// Package gitsource fetches layer content from Git (CONCEPTION.md section
// 9): a Recipe stored in configblender's database references each layer's
// content by repo/path/ref rather than embedding it, so the content itself
// stays versioned and review-able in a GitOps flow.
package gitsource

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
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
// 5.1) — how they reach the resolver (the Git-source registry,
// internal/gitauth) is left to the caller. The error return exists because
// credentials are resolved per fetch, not validated once upfront — a
// malformed stored credential (e.g. an unparseable SSH key) needs a way to
// surface as a clear error rather than silently falling back to
// unauthenticated access.
type AuthResolver func(repoURL string) (transport.AuthMethod, error)

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
// the repository's in-memory cache as needed. ctx bounds the clone/fetch
// network call — the only I/O in this method — so a caller's timeout or
// cancellation (an HTTP request being aborted, a reconcile being superseded)
// actually stops an in-flight Git operation instead of running to
// completion.
func (f *Fetcher) Content(ctx context.Context, src Source) (string, error) {
	if src.Ref == "" {
		return "", fmt.Errorf("git source %s%s: ref is required", src.Repo, src.Path)
	}

	repo, err := f.repo(ctx, src.Repo)
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

func (f *Fetcher) repo(ctx context.Context, repoURL string) (*git.Repository, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	var auth transport.AuthMethod
	if f.auth != nil {
		a, err := f.auth(repoURL)
		if err != nil {
			return nil, fmt.Errorf("resolving credentials for %s: %w", repoURL, err)
		}
		auth = a
	}

	if repo, ok := f.repos[repoURL]; ok {
		err := repo.FetchContext(ctx, &git.FetchOptions{Auth: auth, Force: true})
		if err != nil && !errors.Is(err, git.NoErrAlreadyUpToDate) {
			return nil, fmt.Errorf("fetching %s: %w", repoURL, err)
		}
		return repo, nil
	}

	repo, err := git.CloneContext(ctx, memory.NewStorage(), nil, &git.CloneOptions{
		URL:  repoURL,
		Auth: auth,
	})
	if err != nil {
		return nil, fmt.Errorf("cloning %s: %w", repoURL, err)
	}
	f.repos[repoURL] = repo
	return repo, nil
}

// TestConnection checks that repoURL is reachable with auth (which may be
// nil, for unauthenticated access) without cloning anything: it lists the
// remote's refs, the cheapest operation that still exercises the same
// transport/auth handshake a real clone or fetch would (docs/05-recipe-
// and-crd.md §5.2 — the webui's Sources admin screen uses this to let an
// operator verify a source's repo URL and credentials before saving them).
func TestConnection(ctx context.Context, repoURL string, auth transport.AuthMethod) error {
	remote := git.NewRemote(memory.NewStorage(), &config.RemoteConfig{Name: "origin", URLs: []string{repoURL}})
	if _, err := remote.ListContext(ctx, &git.ListOptions{Auth: auth}); err != nil {
		return fmt.Errorf("connecting to %s: %w", repoURL, err)
	}
	return nil
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
