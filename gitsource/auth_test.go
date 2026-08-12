package gitsource

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"testing"

	"github.com/go-git/go-git/v5/plumbing/transport"
	githttp "github.com/go-git/go-git/v5/plumbing/transport/http"
)

func TestBasicAuthMethod(t *testing.T) {
	auth := BasicAuthMethod("x-access-token", "s3cr3t")
	basic, ok := auth.(*githttp.BasicAuth)
	if !ok {
		t.Fatalf("BasicAuthMethod returned %T, want *http.BasicAuth", auth)
	}
	if basic.Username != "x-access-token" || basic.Password != "s3cr3t" {
		t.Errorf("got %+v, want Username=x-access-token Password=s3cr3t", basic)
	}
}

func TestSSHAuthMethod(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	pemBytes := pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(key),
	})

	auth, err := SSHAuthMethod("git", pemBytes, "")
	if err != nil {
		t.Fatalf("SSHAuthMethod: %v", err)
	}
	if auth.Name() != "ssh-public-keys" {
		t.Errorf("auth.Name() = %q, want %q", auth.Name(), "ssh-public-keys")
	}
}

func TestSSHAuthMethod_InvalidKey(t *testing.T) {
	if _, err := SSHAuthMethod("git", []byte("not a pem key"), ""); err == nil {
		t.Fatal("SSHAuthMethod: expected an error for an invalid PEM key, got nil")
	}
}

func TestNewAuthMap(t *testing.T) {
	specific := BasicAuthMethod("specific-user", "specific-pass")
	fallback := BasicAuthMethod("fallback-user", "fallback-pass")

	resolve := NewAuthMap(map[string]transport.AuthMethod{
		"https://example.com/private.git": specific,
	}, fallback)

	if got := resolve("https://example.com/private.git"); got != specific {
		t.Errorf("resolve(private) = %v, want the specific credential", got)
	}
	if got := resolve("https://example.com/other.git"); got != fallback {
		t.Errorf("resolve(other) = %v, want the fallback credential", got)
	}
}

func TestNewAuthMap_NilFallback(t *testing.T) {
	resolve := NewAuthMap(nil, nil)
	if got := resolve("https://example.com/public.git"); got != nil {
		t.Errorf("resolve(public) = %v, want nil (unauthenticated)", got)
	}
}

// Confirms the resolver is actually consulted with the repo URL during a
// real Fetcher call, not just constructed and ignored.
func TestFetcher_ConsultsAuthResolverWithRepoURL(t *testing.T) {
	repoDir := newTestRepo(t, "layer.yaml", "env: dev\n")

	var gotURL string
	resolver := AuthResolver(func(repoURL string) transport.AuthMethod {
		gotURL = repoURL
		return nil // local path repo needs no auth; we only check it was asked
	})

	f := NewFetcher(resolver)
	if _, err := f.Content(Source{Repo: repoDir, Path: "layer.yaml", Ref: "master"}); err != nil {
		t.Fatalf("Content: %v", err)
	}
	if gotURL != repoDir {
		t.Errorf("AuthResolver called with %q, want %q", gotURL, repoDir)
	}
}
