package centralserver_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"configblender/internal/centralapi"
	"configblender/internal/centralserver"
	"configblender/internal/recipesource"
)

const testWriteToken = "test-token"

func newTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "recipes.db")
	store, err := recipesource.Open(dbPath)
	if err != nil {
		t.Fatalf("recipesource.Open: %v", err)
	}
	t.Cleanup(func() { store.Close() })

	srv := httptest.NewServer(centralserver.New(store, testWriteToken).Handler())
	t.Cleanup(srv.Close)
	return srv
}

func putSource(t *testing.T, srv *httptest.Server, name string, src centralapi.GitSource) {
	t.Helper()
	body, err := json.Marshal(src)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	req, err := http.NewRequest(http.MethodPut, srv.URL+centralapi.SourcesPath+"/"+name, bytes.NewReader(body))
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+testWriteToken)
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("PUT source: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("PUT source: status = %d, want %d", resp.StatusCode, http.StatusNoContent)
	}
}

// A stored credential must never come back over the API, in List or Get —
// the whole point of storing credentials in the DB rather than the
// environment is that the API/webui boundary stays the same as before
// (docs/04-kubernetes.md §4.1): write-only.
func TestPutSource_CredentialsNeverReturnedByList(t *testing.T) {
	srv := newTestServer(t)

	putSource(t, srv, "internal-configs", centralapi.GitSource{
		Repo: "https://example.invalid/config.git",
		Auth: &centralapi.Credentials{Username: "x-access-token", Password: "s3cr3t"},
	})

	resp, err := http.Get(srv.URL + centralapi.SourcesPath)
	if err != nil {
		t.Fatalf("GET sources: %v", err)
	}
	defer resp.Body.Close()

	var listed centralapi.ListSourcesResponse
	if err := json.NewDecoder(resp.Body).Decode(&listed); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(listed.Sources) != 1 {
		t.Fatalf("ListSources = %+v, want 1 source", listed.Sources)
	}
	if listed.Sources[0].Auth != nil {
		t.Errorf("ListSources[0].Auth = %+v, want nil (credentials must be write-only)", listed.Sources[0].Auth)
	}

	// The raw response body must not contain the secret value either —
	// belt and suspenders beyond the typed decode above.
	resp2, err := http.Get(srv.URL + centralapi.SourcesPath)
	if err != nil {
		t.Fatalf("GET sources: %v", err)
	}
	defer resp2.Body.Close()
	raw := new(bytes.Buffer)
	if _, err := raw.ReadFrom(resp2.Body); err != nil {
		t.Fatalf("read body: %v", err)
	}
	if bytes.Contains(raw.Bytes(), []byte("s3cr3t")) {
		t.Errorf("ListSources response body contains the stored secret: %s", raw.String())
	}
}

func TestPutSource_RequiresToken(t *testing.T) {
	srv := newTestServer(t)

	body, _ := json.Marshal(centralapi.GitSource{Repo: "https://example.invalid/config.git"})
	req, err := http.NewRequest(http.MethodPut, srv.URL+centralapi.SourcesPath+"/no-token", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("PUT: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("PUT without token: status = %d, want %d", resp.StatusCode, http.StatusUnauthorized)
	}
}

func TestTestConnection_UnreachableRepoReturnsOkFalse(t *testing.T) {
	srv := newTestServer(t)

	body, _ := json.Marshal(centralapi.TestConnectionRequest{Repo: filepath.Join(t.TempDir(), "does-not-exist")})
	req, err := http.NewRequest(http.MethodPost, srv.URL+centralapi.TestConnectionPath, bytes.NewReader(body))
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+testWriteToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("POST test-connection: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d (a failed connection is not an HTTP error)", resp.StatusCode, http.StatusOK)
	}

	var result centralapi.TestConnectionResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if result.Ok {
		t.Error("TestConnectionResponse.Ok = true, want false for an unreachable repo")
	}
	if result.Error == "" {
		t.Error("TestConnectionResponse.Error is empty, want a reason")
	}
}

// clientWithCookies returns an httptest client with a cookie jar, since
// session auth relies on the browser (or here, the test client) storing
// and resending the cookie POST LoginPath sets — srv.Client() alone has no
// jar, so cookies wouldn't persist across requests.
func clientWithCookies(t *testing.T, srv *httptest.Server) *http.Client {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookiejar.New: %v", err)
	}
	c := *srv.Client()
	c.Jar = jar
	return &c
}

func getSession(t *testing.T, client *http.Client, srv *httptest.Server) centralapi.SessionResponse {
	t.Helper()
	resp, err := client.Get(srv.URL + centralapi.SessionPath)
	if err != nil {
		t.Fatalf("GET session: %v", err)
	}
	defer resp.Body.Close()
	var s centralapi.SessionResponse
	if err := json.NewDecoder(resp.Body).Decode(&s); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return s
}

func TestSession_UnauthenticatedByDefault(t *testing.T) {
	srv := newTestServer(t)
	client := clientWithCookies(t, srv)

	if s := getSession(t, client, srv); s.Authenticated {
		t.Error("Session.Authenticated = true before any login, want false")
	}
}

func TestLogin_WrongToken_Returns401AndNoSession(t *testing.T) {
	srv := newTestServer(t)
	client := clientWithCookies(t, srv)

	body, _ := json.Marshal(centralapi.LoginRequest{Token: "wrong-token"})
	resp, err := client.Post(srv.URL+centralapi.LoginPath, "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST login: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("login with wrong token: status = %d, want %d", resp.StatusCode, http.StatusUnauthorized)
	}
	if s := getSession(t, client, srv); s.Authenticated {
		t.Error("Session.Authenticated = true after a failed login, want false")
	}
}

// The core promise of this feature: after logging in once, a write
// endpoint that used to require a manually-attached Authorization header
// now succeeds off the session cookie alone.
func TestLogin_ThenWriteEndpointSucceedsWithoutBearerHeader(t *testing.T) {
	srv := newTestServer(t)
	client := clientWithCookies(t, srv)

	body, _ := json.Marshal(centralapi.LoginRequest{Token: testWriteToken})
	loginResp, err := client.Post(srv.URL+centralapi.LoginPath, "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST login: %v", err)
	}
	defer loginResp.Body.Close()
	if loginResp.StatusCode != http.StatusNoContent {
		t.Fatalf("login: status = %d, want %d", loginResp.StatusCode, http.StatusNoContent)
	}

	if s := getSession(t, client, srv); !s.Authenticated {
		t.Fatal("Session.Authenticated = false after a successful login, want true")
	}

	srcBody, _ := json.Marshal(centralapi.GitSource{Repo: "https://example.invalid/config.git"})
	req, err := http.NewRequest(http.MethodPut, srv.URL+centralapi.SourcesPath+"/via-session", bytes.NewReader(srcBody))
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	// Deliberately no Authorization header — the cookie alone must satisfy
	// requireToken.
	putResp, err := client.Do(req)
	if err != nil {
		t.Fatalf("PUT source: %v", err)
	}
	defer putResp.Body.Close()
	if putResp.StatusCode != http.StatusNoContent {
		t.Errorf("PUT source with session cookie only: status = %d, want %d", putResp.StatusCode, http.StatusNoContent)
	}
}

func TestLogout_ClearsSession(t *testing.T) {
	srv := newTestServer(t)
	client := clientWithCookies(t, srv)

	body, _ := json.Marshal(centralapi.LoginRequest{Token: testWriteToken})
	loginResp, err := client.Post(srv.URL+centralapi.LoginPath, "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST login: %v", err)
	}
	loginResp.Body.Close()
	if s := getSession(t, client, srv); !s.Authenticated {
		t.Fatal("expected to be authenticated after login")
	}

	logoutResp, err := client.Post(srv.URL+centralapi.LogoutPath, "application/json", nil)
	if err != nil {
		t.Fatalf("POST logout: %v", err)
	}
	logoutResp.Body.Close()

	if s := getSession(t, client, srv); s.Authenticated {
		t.Error("Session.Authenticated = true after logout, want false")
	}
}
