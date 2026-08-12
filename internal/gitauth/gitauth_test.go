package gitauth

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"testing"

	githttp "github.com/go-git/go-git/v5/plumbing/transport/http"
	"github.com/go-git/go-git/v5/plumbing/transport/ssh"

	"configblender/internal/gitsourcedb"
)

func testPEMKey(t *testing.T) []byte {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
}

func TestAuthMethod_Nil(t *testing.T) {
	auth, err := AuthMethod(nil)
	if err != nil || auth != nil {
		t.Errorf("AuthMethod(nil) = (%v, %v), want (nil, nil) (unauthenticated)", auth, err)
	}
}

func TestAuthMethod_BasicAuth(t *testing.T) {
	auth, err := AuthMethod(&gitsourcedb.Credentials{Username: "alice", Password: "s3cr3t"})
	if err != nil {
		t.Fatalf("AuthMethod: %v", err)
	}
	basic, ok := auth.(*githttp.BasicAuth)
	if !ok {
		t.Fatal("AuthMethod did not return *http.BasicAuth")
	}
	if basic.Username != "alice" || basic.Password != "s3cr3t" {
		t.Errorf("got %+v, want Username=alice Password=s3cr3t", basic)
	}
}

func TestAuthMethod_SSHKey(t *testing.T) {
	auth, err := AuthMethod(&gitsourcedb.Credentials{SSHKey: string(testPEMKey(t))})
	if err != nil {
		t.Fatalf("AuthMethod: %v", err)
	}
	keys, ok := auth.(*ssh.PublicKeys)
	if !ok {
		t.Fatal("AuthMethod did not return *ssh.PublicKeys")
	}
	if keys.User != "git" {
		t.Errorf("User = %q, want default %q", keys.User, "git")
	}
}

func TestAuthMethod_SSHKeyCustomUser(t *testing.T) {
	auth, err := AuthMethod(&gitsourcedb.Credentials{SSHKey: string(testPEMKey(t)), SSHUser: "deploy"})
	if err != nil {
		t.Fatalf("AuthMethod: %v", err)
	}
	keys, ok := auth.(*ssh.PublicKeys)
	if !ok {
		t.Fatal("AuthMethod did not return *ssh.PublicKeys")
	}
	if keys.User != "deploy" {
		t.Errorf("User = %q, want %q", keys.User, "deploy")
	}
}

func TestAuthMethod_InvalidSSHKey(t *testing.T) {
	if _, err := AuthMethod(&gitsourcedb.Credentials{SSHKey: "not a pem key"}); err == nil {
		t.Fatal("AuthMethod: expected an error for an invalid SSH key, got nil")
	}
}

func TestAuthMethod_NoCredentialsSet(t *testing.T) {
	auth, err := AuthMethod(&gitsourcedb.Credentials{})
	if err != nil || auth != nil {
		t.Errorf("AuthMethod(empty) = (%v, %v), want (nil, nil) (unauthenticated)", auth, err)
	}
}

func TestFromSources_UsesRegisteredSourceCredentials(t *testing.T) {
	resolve := FromSources(func(repoURL string) (*gitsourcedb.GitSource, bool) {
		if repoURL == "https://example.com/internal-configs.git" {
			return &gitsourcedb.GitSource{
				Name: "internal-configs",
				Repo: repoURL,
				Auth: &gitsourcedb.Credentials{Username: "specific-user", Password: "specific-token"},
			}, true
		}
		return nil, false
	})

	auth, err := resolve("https://example.com/internal-configs.git")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	basic, ok := auth.(*githttp.BasicAuth)
	if !ok {
		t.Fatal("resolve did not return *http.BasicAuth for the registered source")
	}
	if basic.Username != "specific-user" || basic.Password != "specific-token" {
		t.Errorf("got %+v, want the registered source's stored credential", basic)
	}
}

func TestFromSources_NoStoredAuthIsUnauthenticated(t *testing.T) {
	resolve := FromSources(func(repoURL string) (*gitsourcedb.GitSource, bool) {
		return &gitsourcedb.GitSource{Name: "public-repo", Repo: repoURL}, true // registered, no Auth
	})

	auth, err := resolve("https://example.com/public-repo.git")
	if err != nil || auth != nil {
		t.Errorf("resolve = (%v, %v), want (nil, nil) (unauthenticated, no global fallback)", auth, err)
	}
}

func TestFromSources_UnregisteredURLIsUnauthenticated(t *testing.T) {
	resolve := FromSources(func(repoURL string) (*gitsourcedb.GitSource, bool) { return nil, false })

	auth, err := resolve("https://example.com/anything.git")
	if err != nil || auth != nil {
		t.Errorf("resolve = (%v, %v), want (nil, nil) (unauthenticated, no global fallback)", auth, err)
	}
}

func TestFromSources_InvalidStoredCredentialSurfacesAsError(t *testing.T) {
	resolve := FromSources(func(repoURL string) (*gitsourcedb.GitSource, bool) {
		return &gitsourcedb.GitSource{
			Name: "broken",
			Repo: repoURL,
			Auth: &gitsourcedb.Credentials{SSHKey: "not a pem key"},
		}, true
	})

	if _, err := resolve("https://example.com/broken.git"); err == nil {
		t.Fatal("resolve: expected an error for an unparseable stored SSH key, got nil")
	}
}
