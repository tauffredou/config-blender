package gitauth

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"os"
	"path/filepath"
	"testing"

	githttp "github.com/go-git/go-git/v5/plumbing/transport/http"
	"github.com/go-git/go-git/v5/plumbing/transport/ssh"
)

func testPEMKey(t *testing.T) []byte {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
}

func TestFromEnv_NoCredentials(t *testing.T) {
	resolve, err := FromEnv()
	if err != nil {
		t.Fatalf("FromEnv: %v", err)
	}
	if got := resolve("https://example.com/repo.git"); got != nil {
		t.Errorf("resolve = %v, want nil (unauthenticated)", got)
	}
}

func TestFromEnv_BasicAuthViaPassword(t *testing.T) {
	t.Setenv("GIT_USERNAME", "alice")
	t.Setenv("GIT_PASSWORD", "s3cr3t")

	resolve, err := FromEnv()
	if err != nil {
		t.Fatalf("FromEnv: %v", err)
	}
	auth, ok := resolve("https://example.com/repo.git").(*githttp.BasicAuth)
	if !ok {
		t.Fatal("resolve did not return *http.BasicAuth")
	}
	if auth.Username != "alice" || auth.Password != "s3cr3t" {
		t.Errorf("got %+v, want Username=alice Password=s3cr3t", auth)
	}
}

func TestFromEnv_BasicAuthViaToken(t *testing.T) {
	t.Setenv("GIT_USERNAME", "x-access-token")
	t.Setenv("GIT_TOKEN", "ghp_example")

	resolve, err := FromEnv()
	if err != nil {
		t.Fatalf("FromEnv: %v", err)
	}
	auth, ok := resolve("https://example.com/repo.git").(*githttp.BasicAuth)
	if !ok {
		t.Fatal("resolve did not return *http.BasicAuth")
	}
	if auth.Password != "ghp_example" {
		t.Errorf("Password = %q, want %q", auth.Password, "ghp_example")
	}
}

func TestFromEnv_SSHKeyInline(t *testing.T) {
	t.Setenv("GIT_SSH_KEY", string(testPEMKey(t)))

	resolve, err := FromEnv()
	if err != nil {
		t.Fatalf("FromEnv: %v", err)
	}
	auth, ok := resolve("git@example.com:org/repo.git").(*ssh.PublicKeys)
	if !ok {
		t.Fatal("resolve did not return *ssh.PublicKeys")
	}
	if auth.User != "git" {
		t.Errorf("User = %q, want default %q", auth.User, "git")
	}
}

func TestFromEnv_SSHKeyFile(t *testing.T) {
	keyPath := filepath.Join(t.TempDir(), "id_rsa")
	if err := os.WriteFile(keyPath, testPEMKey(t), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	t.Setenv("GIT_SSH_KEY_FILE", keyPath)
	t.Setenv("GIT_SSH_USER", "deploy")

	resolve, err := FromEnv()
	if err != nil {
		t.Fatalf("FromEnv: %v", err)
	}
	auth, ok := resolve("git@example.com:org/repo.git").(*ssh.PublicKeys)
	if !ok {
		t.Fatal("resolve did not return *ssh.PublicKeys")
	}
	if auth.User != "deploy" {
		t.Errorf("User = %q, want %q", auth.User, "deploy")
	}
}

func TestFromEnv_InvalidSSHKey(t *testing.T) {
	t.Setenv("GIT_SSH_KEY", "not a pem key")

	if _, err := FromEnv(); err == nil {
		t.Fatal("FromEnv: expected an error for an invalid SSH key, got nil")
	}
}

func TestFromSources_PerSourceOverridesGlobal(t *testing.T) {
	t.Setenv("GIT_USERNAME", "global-user")
	t.Setenv("GIT_TOKEN", "global-token")
	t.Setenv("GIT_USERNAME_INTERNAL_CONFIGS", "specific-user")
	t.Setenv("GIT_TOKEN_INTERNAL_CONFIGS", "specific-token")

	resolve, err := FromSources(func(repoURL string) (string, bool) {
		if repoURL == "https://example.com/internal-configs.git" {
			return "internal-configs", true
		}
		return "", false
	})
	if err != nil {
		t.Fatalf("FromSources: %v", err)
	}

	auth, ok := resolve("https://example.com/internal-configs.git").(*githttp.BasicAuth)
	if !ok {
		t.Fatal("resolve did not return *http.BasicAuth for the registered source")
	}
	if auth.Username != "specific-user" || auth.Password != "specific-token" {
		t.Errorf("got %+v, want the per-source credential", auth)
	}
}

func TestFromSources_FallsBackToGlobalWhenNoOverride(t *testing.T) {
	t.Setenv("GIT_USERNAME", "global-user")
	t.Setenv("GIT_TOKEN", "global-token")

	resolve, err := FromSources(func(repoURL string) (string, bool) {
		return "internal-configs", true // registered, but no per-source env vars set
	})
	if err != nil {
		t.Fatalf("FromSources: %v", err)
	}

	auth, ok := resolve("https://example.com/internal-configs.git").(*githttp.BasicAuth)
	if !ok {
		t.Fatal("resolve did not return *http.BasicAuth from the global fallback")
	}
	if auth.Username != "global-user" || auth.Password != "global-token" {
		t.Errorf("got %+v, want the global credential", auth)
	}
}

func TestFromSources_FallsBackToGlobalWhenURLUnregistered(t *testing.T) {
	t.Setenv("GIT_USERNAME", "global-user")
	t.Setenv("GIT_TOKEN", "global-token")

	resolve, err := FromSources(func(repoURL string) (string, bool) { return "", false })
	if err != nil {
		t.Fatalf("FromSources: %v", err)
	}

	auth, ok := resolve("https://example.com/anything.git").(*githttp.BasicAuth)
	if !ok {
		t.Fatal("resolve did not return *http.BasicAuth from the global fallback")
	}
	if auth.Username != "global-user" {
		t.Errorf("got %+v, want the global credential", auth)
	}
}

func TestEnvSafe(t *testing.T) {
	cases := map[string]string{
		"internal-configs": "INTERNAL_CONFIGS",
		"already_safe":     "ALREADY_SAFE",
		"weird!name.here":  "WEIRD_NAME_HERE",
	}
	for in, want := range cases {
		if got := envSafe(in); got != want {
			t.Errorf("envSafe(%q) = %q, want %q", in, got, want)
		}
	}
}
