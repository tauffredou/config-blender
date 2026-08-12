// Package gitauth wires the Git-source registry's stored credentials
// (internal/gitsourcedb) into a gitsource.AuthResolver. The Recipe DB is
// the single source of truth for Git credentials (docs/04-kubernetes.md
// §4.1, docs/07-open-questions.md) — there is no environment-variable
// fallback and no global credential: a source with no stored Auth fetches
// unauthenticated.
package gitauth

import (
	"fmt"

	"github.com/go-git/go-git/v5/plumbing/transport"

	"configblender/gitsource"
	"configblender/internal/gitsourcedb"
)

// FromSources returns an AuthResolver that resolves each repo URL's
// credentials from its registered gitsourcedb.GitSource. lookup is
// typically gitsourcedb.Store.LookupByRepo, so it always reflects the
// current registry — no rebuild-on-write needed. A repo URL with no
// registered source, or a registered source with no stored Auth, fetches
// unauthenticated.
func FromSources(lookup func(repoURL string) (*gitsourcedb.GitSource, bool)) gitsource.AuthResolver {
	return func(repoURL string) (transport.AuthMethod, error) {
		src, ok := lookup(repoURL)
		if !ok || src.Auth == nil {
			return nil, nil
		}
		return AuthMethod(src.Auth)
	}
}

// AuthMethod converts stored credentials into a go-git AuthMethod:
// Username+Password for HTTP(S) basic auth (Password also holds a
// personal access token), else an SSH private key from SSHKey. c may be
// nil, meaning unauthenticated access. Exported so callers that need to
// validate or exercise a credential outside of a real fetch (e.g. a
// connection test) can reuse the same conversion FromSources uses
// internally.
func AuthMethod(c *gitsourcedb.Credentials) (transport.AuthMethod, error) {
	if c == nil {
		return nil, nil
	}
	if c.Username != "" && c.Password != "" {
		return gitsource.BasicAuthMethod(c.Username, c.Password), nil
	}
	if c.SSHKey != "" {
		user := c.SSHUser
		if user == "" {
			user = "git"
		}
		auth, err := gitsource.SSHAuthMethod(user, []byte(c.SSHKey), c.SSHKeyPassphrase)
		if err != nil {
			return nil, fmt.Errorf("gitauth: %w", err)
		}
		return auth, nil
	}
	return nil, nil
}
