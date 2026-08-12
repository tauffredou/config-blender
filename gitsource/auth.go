package gitsource

import (
	"fmt"

	"github.com/go-git/go-git/v5/plumbing/transport"
	githttp "github.com/go-git/go-git/v5/plumbing/transport/http"
	"github.com/go-git/go-git/v5/plumbing/transport/ssh"
)

// BasicAuthMethod builds HTTP(S) basic-auth credentials for a Git remote —
// e.g. a GitHub/GitLab personal access token, passed as password with any
// non-empty username (GitHub accepts the token itself as username too).
func BasicAuthMethod(username, password string) transport.AuthMethod {
	return &githttp.BasicAuth{Username: username, Password: password}
}

// SSHAuthMethod builds SSH key credentials for a Git remote (git@host:...
// URLs) from a PEM-encoded private key, e.g. loaded from a mounted K8s
// Secret. passphrase may be empty for an unencrypted key.
func SSHAuthMethod(user string, privateKeyPEM []byte, passphrase string) (transport.AuthMethod, error) {
	auth, err := ssh.NewPublicKeys(user, privateKeyPEM, passphrase)
	if err != nil {
		return nil, fmt.Errorf("gitsource: parsing SSH private key: %w", err)
	}
	return auth, nil
}

// NewAuthMap returns an AuthResolver that looks up credentials by exact
// repo URL, falling back to defaultAuth (which may be nil, for
// unauthenticated access) when a repo has no specific entry. This is
// configblender's own operational credential set (distinct from
// application config/secrets, docs/04-kubernetes.md §4.1) — how byRepo and
// defaultAuth are populated (env vars, a mounted K8s Secret...) is up to
// the caller (docs/07-open-questions.md).
func NewAuthMap(byRepo map[string]transport.AuthMethod, defaultAuth transport.AuthMethod) AuthResolver {
	return func(repoURL string) transport.AuthMethod {
		if auth, ok := byRepo[repoURL]; ok {
			return auth
		}
		return defaultAuth
	}
}
