// Package centralclient calls the central service's HTTP API
// (internal/centralserver) to resolve a Recipe — the client side of the
// ESO+Vault-shaped split (docs/04-kubernetes.md §4.2). It implements the
// same internal/controller.RecipeResolver contract as a local
// internal/recipesource.Store, so the reconciler cannot tell the
// difference between a local and a remote resolution.
package centralclient

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"configblender/internal/centralapi"
	"configblender/internal/gitsourcedb"
	"configblender/recipe"
	"configblender/recipedb"
	"configblender/resolve"
)

type Client struct {
	baseURL string
	http    *http.Client
	token   string
}

// New builds a client for the central service at baseURL (e.g.
// "http://configblender-central.configblender.svc:8080"). httpClient may
// be nil to use http.DefaultClient.
func New(baseURL string, httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &Client{baseURL: strings.TrimRight(baseURL, "/"), http: httpClient}
}

// WithToken sets the bearer token sent on write calls (Put, Rollback) —
// required by the central service's requireToken gate
// (docs/05-recipe-and-crd.md §5.3). Reads (Get, List, Resolve...) never
// need it.
func (c *Client) WithToken(token string) *Client {
	c.token = token
	return c
}

func (c *Client) authorize(req *http.Request) {
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
}

func (c *Client) Resolve(name string) (*resolve.Result, error) {
	u := c.baseURL + centralapi.ResolvePath + "?recipe=" + url.QueryEscape(name)

	resp, err := c.http.Get(u)
	if err != nil {
		return nil, fmt.Errorf("centralclient: calling central service: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("centralclient: resolving %q: central service returned %d: %s", name, resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var payload centralapi.ResolveResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, fmt.Errorf("centralclient: decoding response for %q: %w", name, err)
	}
	return &resolve.Result{Config: payload.Config, Explain: payload.Explain}, nil
}

// Put creates a new version of spec.Name on the central service — requires
// WithToken (docs/05-recipe-and-crd.md §5.3).
func (c *Client) Put(spec *recipe.Spec) error {
	body, err := json.Marshal(spec)
	if err != nil {
		return fmt.Errorf("centralclient: encoding recipe %q: %w", spec.Name, err)
	}
	u := c.baseURL + centralapi.RecipesPath + "/" + url.PathEscape(spec.Name)
	req, err := http.NewRequest(http.MethodPut, u, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("centralclient: building request for %q: %w", spec.Name, err)
	}
	req.Header.Set("Content-Type", "application/json")
	c.authorize(req)

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("centralclient: calling central service: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("centralclient: putting %q: central service returned %d: %s", spec.Name, resp.StatusCode, strings.TrimSpace(string(respBody)))
	}
	return nil
}

// Rollback restores version as a new version of the Recipe named name —
// requires WithToken (docs/05-recipe-and-crd.md §5.3).
func (c *Client) Rollback(name string, version int) error {
	body, _ := json.Marshal(centralapi.RollbackRequest{Version: version})
	u := c.baseURL + centralapi.RecipesPath + "/" + url.PathEscape(name) + "/rollback"
	req, err := http.NewRequest(http.MethodPost, u, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("centralclient: building request for %q: %w", name, err)
	}
	req.Header.Set("Content-Type", "application/json")
	c.authorize(req)

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("centralclient: calling central service: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("centralclient: rolling back %q to version %d: central service returned %d: %s", name, version, resp.StatusCode, strings.TrimSpace(string(respBody)))
	}
	return nil
}

// Get fetches the stored Spec for name (latest version).
func (c *Client) Get(name string) (*recipe.Spec, error) {
	u := c.baseURL + centralapi.RecipesPath + "/" + url.PathEscape(name)

	resp, err := c.http.Get(u)
	if err != nil {
		return nil, fmt.Errorf("centralclient: calling central service: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("centralclient: getting %q: central service returned %d: %s", name, resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var spec recipe.Spec
	if err := json.NewDecoder(resp.Body).Decode(&spec); err != nil {
		return nil, fmt.Errorf("centralclient: decoding response for %q: %w", name, err)
	}
	return &spec, nil
}

// GetVersion fetches a specific historical version of the Spec named name
// (docs/05-recipe-and-crd.md §5.3 — Vault-KV-v2-style history).
func (c *Client) GetVersion(name string, version int) (*recipe.Spec, error) {
	u := c.baseURL + centralapi.RecipesPath + "/" + url.PathEscape(name) + "/versions/" + strconv.Itoa(version)

	resp, err := c.http.Get(u)
	if err != nil {
		return nil, fmt.Errorf("centralclient: calling central service: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("centralclient: getting %q version %d: central service returned %d: %s", name, version, resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var spec recipe.Spec
	if err := json.NewDecoder(resp.Body).Decode(&spec); err != nil {
		return nil, fmt.Errorf("centralclient: decoding response for %q version %d: %w", name, version, err)
	}
	return &spec, nil
}

// ListVersions returns the version history of the Recipe named name.
func (c *Client) ListVersions(name string) ([]recipedb.VersionInfo, error) {
	u := c.baseURL + centralapi.RecipesPath + "/" + url.PathEscape(name) + "/versions"

	resp, err := c.http.Get(u)
	if err != nil {
		return nil, fmt.Errorf("centralclient: calling central service: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("centralclient: listing versions of %q: central service returned %d: %s", name, resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var payload centralapi.ListVersionsResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, fmt.Errorf("centralclient: decoding version list for %q: %w", name, err)
	}
	versions := make([]recipedb.VersionInfo, len(payload.Versions))
	for i, v := range payload.Versions {
		versions[i] = recipedb.VersionInfo{Version: v.Version, UpdatedAt: v.UpdatedAt}
	}
	return versions, nil
}

// List returns the names of every Recipe known to the central service.
func (c *Client) List() ([]string, error) {
	resp, err := c.http.Get(c.baseURL + centralapi.RecipesPath)
	if err != nil {
		return nil, fmt.Errorf("centralclient: calling central service: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("centralclient: listing recipes: central service returned %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var payload centralapi.ListRecipesResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, fmt.Errorf("centralclient: decoding list response: %w", err)
	}
	return payload.Names, nil
}

// ListSources returns every Git source registered with the central service.
func (c *Client) ListSources() ([]gitsourcedb.GitSource, error) {
	resp, err := c.http.Get(c.baseURL + centralapi.SourcesPath)
	if err != nil {
		return nil, fmt.Errorf("centralclient: calling central service: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("centralclient: listing sources: central service returned %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var payload centralapi.ListSourcesResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, fmt.Errorf("centralclient: decoding source list: %w", err)
	}
	sources := make([]gitsourcedb.GitSource, len(payload.Sources))
	for i, src := range payload.Sources {
		sources[i] = gitsourcedb.GitSource{Name: src.Name, Repo: src.Repo}
	}
	return sources, nil
}

// PutSource registers or replaces src on the central service — requires
// WithToken (docs/05-recipe-and-crd.md §5.3).
func (c *Client) PutSource(src *gitsourcedb.GitSource) error {
	body, err := json.Marshal(centralapi.GitSource{Name: src.Name, Repo: src.Repo})
	if err != nil {
		return fmt.Errorf("centralclient: encoding source %q: %w", src.Name, err)
	}
	u := c.baseURL + centralapi.SourcesPath + "/" + url.PathEscape(src.Name)
	req, err := http.NewRequest(http.MethodPut, u, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("centralclient: building request for %q: %w", src.Name, err)
	}
	req.Header.Set("Content-Type", "application/json")
	c.authorize(req)

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("centralclient: calling central service: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("centralclient: putting source %q: central service returned %d: %s", src.Name, resp.StatusCode, strings.TrimSpace(string(respBody)))
	}
	return nil
}

// DeleteSource removes the Git source named name from the central service —
// requires WithToken (docs/05-recipe-and-crd.md §5.3).
func (c *Client) DeleteSource(name string) error {
	u := c.baseURL + centralapi.SourcesPath + "/" + url.PathEscape(name)
	req, err := http.NewRequest(http.MethodDelete, u, nil)
	if err != nil {
		return fmt.Errorf("centralclient: building request for %q: %w", name, err)
	}
	c.authorize(req)

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("centralclient: calling central service: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("centralclient: deleting source %q: central service returned %d: %s", name, resp.StatusCode, strings.TrimSpace(string(respBody)))
	}
	return nil
}
