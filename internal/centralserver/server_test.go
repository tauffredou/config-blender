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
	if loginResp.StatusCode != http.StatusOK {
		t.Fatalf("login: status = %d, want %d", loginResp.StatusCode, http.StatusOK)
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

// loginAsUser creates username/password/role via the admin bearer token,
// then logs client in as that user, returning the SessionResponse from the
// login call.
func loginAsUser(t *testing.T, srv *httptest.Server, client *http.Client, username, password, role string) centralapi.SessionResponse {
	t.Helper()
	createUser(t, srv, username, password, role)

	body, _ := json.Marshal(centralapi.LoginRequest{Username: username, Password: password})
	resp, err := client.Post(srv.URL+centralapi.LoginPath, "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST login: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("login as %q: status = %d, want %d", username, resp.StatusCode, http.StatusOK)
	}
	var s centralapi.SessionResponse
	if err := json.NewDecoder(resp.Body).Decode(&s); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return s
}

func createUser(t *testing.T, srv *httptest.Server, username, password, role string) {
	t.Helper()
	body, _ := json.Marshal(centralapi.CreateUserRequest{Username: username, Password: password, Role: role})
	req, err := http.NewRequest(http.MethodPost, srv.URL+centralapi.UsersPath, bytes.NewReader(body))
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+testWriteToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("POST users: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("POST users(%q): status = %d, want %d", username, resp.StatusCode, http.StatusNoContent)
	}
}

func TestCreateUser_InvalidRole_Returns400(t *testing.T) {
	srv := newTestServer(t)

	body, _ := json.Marshal(centralapi.CreateUserRequest{Username: "nobody", Password: "pw", Role: "superuser"})
	req, _ := http.NewRequest(http.MethodPost, srv.URL+centralapi.UsersPath, bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+testWriteToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("POST users: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("create user with invalid role: status = %d, want %d", resp.StatusCode, http.StatusBadRequest)
	}
}

// A contributor can write Recipes but must be forbidden from writing Git
// sources or managing users — the three roles gate three disjoint (except
// for admin) write surfaces.
func TestRoleContributor_CanWriteRecipes_ButNotSourcesOrUsers(t *testing.T) {
	srv := newTestServer(t)
	client := clientWithCookies(t, srv)
	loginAsUser(t, srv, client, "carol", "pw12345", "contributor")

	specBody, _ := json.Marshal(map[string]any{"layers": []any{}})
	req, _ := http.NewRequest(http.MethodPut, srv.URL+centralapi.RecipesPath+"/demo", bytes.NewReader(specBody))
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("PUT recipe: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Errorf("contributor PUT recipe: status = %d, want %d", resp.StatusCode, http.StatusNoContent)
	}

	srcBody, _ := json.Marshal(centralapi.GitSource{Repo: "https://example.invalid/config.git"})
	req, _ = http.NewRequest(http.MethodPut, srv.URL+centralapi.SourcesPath+"/blocked", bytes.NewReader(srcBody))
	resp, err = client.Do(req)
	if err != nil {
		t.Fatalf("PUT source: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("contributor PUT source: status = %d, want %d", resp.StatusCode, http.StatusForbidden)
	}

	resp, err = client.Get(srv.URL + centralapi.UsersPath)
	if err != nil {
		t.Fatalf("GET users: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("contributor GET users: status = %d, want %d", resp.StatusCode, http.StatusForbidden)
	}
}

// A source-manager can write Git sources but must be forbidden from writing
// Recipes or managing users.
func TestRoleSourceManager_CanWriteSources_ButNotRecipesOrUsers(t *testing.T) {
	srv := newTestServer(t)
	client := clientWithCookies(t, srv)
	loginAsUser(t, srv, client, "sam", "pw12345", "source-manager")

	srcBody, _ := json.Marshal(centralapi.GitSource{Repo: "https://example.invalid/config.git"})
	req, _ := http.NewRequest(http.MethodPut, srv.URL+centralapi.SourcesPath+"/allowed", bytes.NewReader(srcBody))
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("PUT source: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Errorf("source-manager PUT source: status = %d, want %d", resp.StatusCode, http.StatusNoContent)
	}

	specBody, _ := json.Marshal(map[string]any{"layers": []any{}})
	req, _ = http.NewRequest(http.MethodPut, srv.URL+centralapi.RecipesPath+"/demo", bytes.NewReader(specBody))
	resp, err = client.Do(req)
	if err != nil {
		t.Fatalf("PUT recipe: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("source-manager PUT recipe: status = %d, want %d", resp.StatusCode, http.StatusForbidden)
	}

	req, _ = http.NewRequest(http.MethodPost, srv.URL+centralapi.UsersPath, bytes.NewReader([]byte(`{"username":"x","password":"y","role":"contributor"}`)))
	resp, err = client.Do(req)
	if err != nil {
		t.Fatalf("POST users: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("source-manager POST users: status = %d, want %d", resp.StatusCode, http.StatusForbidden)
	}
}

// An admin can do everything a contributor and a source-manager can, plus
// manage users — admin is a strict superset, not a fourth disjoint role.
func TestRoleAdmin_CanManageUsers(t *testing.T) {
	srv := newTestServer(t)
	client := clientWithCookies(t, srv)
	loginAsUser(t, srv, client, "root", "pw12345", "admin")

	resp, err := client.Get(srv.URL + centralapi.UsersPath)
	if err != nil {
		t.Fatalf("GET users: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("admin GET users: status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	var listed centralapi.ListUsersResponse
	if err := json.NewDecoder(resp.Body).Decode(&listed); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(listed.Users) != 1 || listed.Users[0].Username != "root" {
		t.Errorf("ListUsers = %+v, want just [root]", listed.Users)
	}
}

// A caller with no session and no bearer token gets 401, not 403 — that
// distinction (identity unknown vs. identity known but insufficient) is
// requireRole's contract.
func TestUsersEndpoint_Unauthenticated_Returns401(t *testing.T) {
	srv := newTestServer(t)

	resp, err := http.Get(srv.URL + centralapi.UsersPath)
	if err != nil {
		t.Fatalf("GET users: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("unauthenticated GET users: status = %d, want %d", resp.StatusCode, http.StatusUnauthorized)
	}
}

// An admin must not be able to delete the account they're currently
// authenticated as — otherwise a lone admin could lock themselves out.
func TestDeleteUser_SelfDeletionGuard(t *testing.T) {
	srv := newTestServer(t)
	client := clientWithCookies(t, srv)
	loginAsUser(t, srv, client, "root", "pw12345", "admin")

	req, _ := http.NewRequest(http.MethodDelete, srv.URL+centralapi.UsersPath+"/root", nil)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("DELETE users/root: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("self-deletion: status = %d, want %d", resp.StatusCode, http.StatusBadRequest)
	}

	if s := getSession(t, client, srv); !s.Authenticated {
		t.Error("session should still be valid after a rejected self-deletion attempt")
	}
}

// An admin deleting a different account must succeed, and that account's
// session (any request re-authenticating as them) must stop working
// immediately rather than riding out its TTL — this is the live-lookup
// behavior authenticate relies on instead of trusting a role/identity baked
// into the cookie at login time.
func TestDeleteUser_OtherAccount_RevokesTheirSessionImmediately(t *testing.T) {
	srv := newTestServer(t)
	adminClient := clientWithCookies(t, srv)
	loginAsUser(t, srv, adminClient, "root", "pw12345", "admin")

	victimClient := clientWithCookies(t, srv)
	loginAsUser(t, srv, victimClient, "victim", "pw12345", "contributor")
	if s := getSession(t, victimClient, srv); !s.Authenticated {
		t.Fatal("victim should be authenticated before deletion")
	}

	req, _ := http.NewRequest(http.MethodDelete, srv.URL+centralapi.UsersPath+"/victim", nil)
	resp, err := adminClient.Do(req)
	if err != nil {
		t.Fatalf("DELETE users/victim: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("DELETE users/victim: status = %d, want %d", resp.StatusCode, http.StatusNoContent)
	}

	if s := getSession(t, victimClient, srv); s.Authenticated {
		t.Error("victim's session cookie should stop authenticating immediately after account deletion")
	}
}

// Changing a user's role must take effect on their very next request, not
// wait out the session's multi-day TTL — role is looked up live, not
// trusted from the cookie.
func TestUpdateUser_RoleChange_TakesEffectImmediately(t *testing.T) {
	srv := newTestServer(t)
	adminClient := clientWithCookies(t, srv)
	loginAsUser(t, srv, adminClient, "root", "pw12345", "admin")

	targetClient := clientWithCookies(t, srv)
	loginAsUser(t, srv, targetClient, "grower", "pw12345", "contributor")

	specBody, _ := json.Marshal(map[string]any{"layers": []any{}})
	req, _ := http.NewRequest(http.MethodPut, srv.URL+centralapi.RecipesPath+"/demo", bytes.NewReader(specBody))
	resp, err := targetClient.Do(req)
	if err != nil {
		t.Fatalf("PUT recipe: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("contributor PUT recipe before promotion: status = %d, want %d", resp.StatusCode, http.StatusNoContent)
	}

	updateBody, _ := json.Marshal(centralapi.UpdateUserRequest{Role: "source-manager"})
	req, _ = http.NewRequest(http.MethodPut, srv.URL+centralapi.UsersPath+"/grower", bytes.NewReader(updateBody))
	resp, err = adminClient.Do(req)
	if err != nil {
		t.Fatalf("PUT users/grower: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("role update: status = %d, want %d", resp.StatusCode, http.StatusNoContent)
	}

	req, _ = http.NewRequest(http.MethodPut, srv.URL+centralapi.RecipesPath+"/demo2", bytes.NewReader(specBody))
	resp, err = targetClient.Do(req)
	if err != nil {
		t.Fatalf("PUT recipe: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("PUT recipe after demotion to source-manager: status = %d, want %d", resp.StatusCode, http.StatusForbidden)
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
