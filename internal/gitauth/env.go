// Package gitauth wires environment variables into a gitsource.AuthResolver,
// so Git credentials reach configblender the same way any other app
// consumes a mounted K8s Secret (env vars or a mounted file) — configblender
// needs no Secret-reading code of its own (docs/04-kubernetes.md §4.1:
// it stays a plain consumer of ambient credentials, not a secrets manager).
package gitauth

import (
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/go-git/go-git/v5/plumbing/transport"

	"configblender/gitsource"
)

// FromEnv builds an AuthResolver from environment variables. Recognized
// variables (first match wins):
//
//   - GIT_USERNAME + (GIT_PASSWORD or GIT_TOKEN) -> HTTP basic auth
//   - GIT_SSH_KEY (PEM content) or GIT_SSH_KEY_FILE (path to a mounted key)
//     -> SSH key auth; optional GIT_SSH_USER (default "git"),
//     GIT_SSH_KEY_PASSPHRASE
//
// If none are set, FromEnv returns a resolver that always returns nil
// (unauthenticated access — the correct default for public repos).
//
// The same credential is used for every repo. For per-repo credentials,
// see FromSources.
func FromEnv() (gitsource.AuthResolver, error) {
	auth, err := authFromEnv(os.Getenv)
	if err != nil {
		return nil, err
	}
	return constantAuth(auth), nil
}

// FromSources returns an AuthResolver that resolves credentials per
// registered Git source rather than one global credential
// (docs/07-open-questions.md: "per-repo credentials rather than a single
// global set"). For a repo URL, it asks lookupName (backed by
// internal/gitsourcedb.Store.LookupByRepo, so it always reflects the
// current registry — no rebuild-on-write needed) which registered source
// owns that URL, then resolves that source's own credentials from env vars
// suffixed by its sanitized name (e.g. GIT_TOKEN_MY_REPO for a source
// named "my-repo") — same variable names as FromEnv, just suffixed. Falls
// back to the single global credential (FromEnv's behavior) when no
// per-source override is set, or when the URL belongs to no registered
// source at all.
func FromSources(lookupName func(repoURL string) (name string, ok bool)) (gitsource.AuthResolver, error) {
	fallback, err := authFromEnv(os.Getenv)
	if err != nil {
		return nil, err
	}
	return func(repoURL string) transport.AuthMethod {
		if name, ok := lookupName(repoURL); ok {
			if auth, err := authFromEnv(suffixedGetenv(name)); err == nil && auth != nil {
				return auth
			}
		}
		return fallback
	}, nil
}

// authFromEnv is FromEnv's parsing logic, parameterized over how a
// variable's value is looked up so FromSources can reuse it with a
// per-source suffix instead of duplicating the parsing.
func authFromEnv(get func(string) string) (transport.AuthMethod, error) {
	if user, pass := get("GIT_USERNAME"), firstNonEmpty(get("GIT_PASSWORD"), get("GIT_TOKEN")); user != "" && pass != "" {
		return gitsource.BasicAuthMethod(user, pass), nil
	}

	if keyPEM, err := sshKeyFrom(get); err != nil {
		return nil, err
	} else if keyPEM != nil {
		user := get("GIT_SSH_USER")
		if user == "" {
			user = "git"
		}
		return gitsource.SSHAuthMethod(user, keyPEM, get("GIT_SSH_KEY_PASSPHRASE"))
	}

	return nil, nil
}

// suffixedGetenv returns a getenv function that looks up "<KEY>_<SUFFIX>"
// where SUFFIX is name sanitized into a valid env-var fragment.
func suffixedGetenv(name string) func(string) string {
	suffix := "_" + envSafe(name)
	return func(key string) string { return os.Getenv(key + suffix) }
}

var envUnsafeChars = regexp.MustCompile(`[^A-Z0-9_]+`)

// envSafe uppercases name and replaces anything that isn't a valid env-var
// character with "_", so a source name like "internal-configs" becomes the
// variable fragment "INTERNAL_CONFIGS".
func envSafe(name string) string {
	return envUnsafeChars.ReplaceAllString(strings.ToUpper(name), "_")
}

func sshKeyFrom(get func(string) string) ([]byte, error) {
	if key := get("GIT_SSH_KEY"); key != "" {
		return []byte(key), nil
	}
	if path := get("GIT_SSH_KEY_FILE"); path != "" {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("gitauth: reading GIT_SSH_KEY_FILE: %w", err)
		}
		return data, nil
	}
	return nil, nil
}

func constantAuth(auth transport.AuthMethod) gitsource.AuthResolver {
	return func(string) transport.AuthMethod { return auth }
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
