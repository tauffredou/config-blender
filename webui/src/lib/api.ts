// Thin client for the central service's HTTP API (internal/centralserver).
// Reads are unauthenticated; writes (put/rollback/source put/source
// delete/test-connection) require the write token — the browser proves it
// once via login() (POST /v1/login), which sets a session cookie; fetch
// sends it automatically on every same-origin request after that (default
// `credentials: "same-origin"`), so no call here attaches an
// Authorization header itself (docs/07-open-questions.md).
import type { RecipeSpec, VersionEntry, ResolveResponse, SourceConfig, Credentials, Role, User } from "./types"

class ApiError extends Error {}

async function request<T>(path: string, opts: RequestInit = {}): Promise<T> {
  const res = await fetch(path, opts)
  if (!res.ok) {
    const text = await res.text()
    throw new ApiError(`${res.status}: ${text.trim()}`)
  }
  if (res.status === 204) return undefined as T
  const contentType = res.headers.get("content-type") ?? ""
  if (contentType.includes("application/json")) return (await res.json()) as T
  return undefined as T
}

function requestJSON<T>(path: string, method: string, body: unknown): Promise<T> {
  return request<T>(path, {
    method,
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body),
  })
}

export const api = {
  listRecipes: () => request<{ names: string[] }>("/v1/recipes"),

  getRecipe: (name: string) => request<RecipeSpec>(`/v1/recipes/${encodeURIComponent(name)}`),

  getRecipeVersion: (name: string, version: number) =>
    request<RecipeSpec>(`/v1/recipes/${encodeURIComponent(name)}/versions/${version}`),

  listVersions: (name: string) =>
    request<{ versions: VersionEntry[] }>(`/v1/recipes/${encodeURIComponent(name)}/versions`),

  putRecipe: (name: string, spec: RecipeSpec) =>
    requestJSON<void>(`/v1/recipes/${encodeURIComponent(name)}`, "PUT", spec),

  rollback: (name: string, version: number) =>
    requestJSON<void>(`/v1/recipes/${encodeURIComponent(name)}/rollback`, "POST", { version }),

  resolve: (name: string) => request<ResolveResponse>(`/v1/resolve?recipe=${encodeURIComponent(name)}`),

  listSources: () => request<{ sources: SourceConfig[] }>("/v1/sources"),

  putSource: (source: SourceConfig) =>
    requestJSON<void>(`/v1/sources/${encodeURIComponent(source.name)}`, "PUT", source),

  deleteSource: (name: string) => request<void>(`/v1/sources/${encodeURIComponent(name)}`, { method: "DELETE" }),

  // testConnection checks a repo/credential pair — saved or not — without
  // registering it as a source. A failed connection is a normal outcome
  // (ok: false, error: "..."), not a thrown ApiError: only a genuine
  // request-level failure (not logged in, network error) throws.
  testConnection: (repo: string, auth: Credentials | undefined) =>
    requestJSON<{ ok: boolean; error?: string }>("/v1/sources/test", "POST", { repo, auth }),

  // login authenticates a registered human account, resolving to a session
  // cookie. A service account never logs in here — it authenticates with
  // its API key directly (Authorization: Bearer) on every request instead.
  login: (credentials: { username: string; password: string }) =>
    requestJSON<{ authenticated: boolean; username?: string; role?: Role }>("/v1/login", "POST", credentials),

  logout: () => request<void>("/v1/logout", { method: "POST" }),

  getSession: () => request<{ authenticated: boolean; username?: string; role?: Role }>("/v1/session"),

  listUsers: () => request<{ users: User[] }>("/v1/users"),

  createUser: (username: string, password: string, role: Role) =>
    requestJSON<void>("/v1/users", "POST", { username, password, role }),

  updateUser: (username: string, changes: { role?: Role; password?: string }) =>
    requestJSON<void>(`/v1/users/${encodeURIComponent(username)}`, "PUT", changes),

  deleteUser: (username: string) => request<void>(`/v1/users/${encodeURIComponent(username)}`, { method: "DELETE" }),

  // createServiceAccount registers a machine account (no password — an API
  // key instead) and returns it once; it is never retrievable again, only
  // rotated (rotateServiceAccountKey).
  createServiceAccount: (username: string, role: Role) =>
    requestJSON<{ username: string; role: Role; apiKey: string }>("/v1/service-accounts", "POST", { username, role }),

  rotateServiceAccountKey: (username: string) =>
    requestJSON<{ username: string; role: Role; apiKey: string }>(
      `/v1/service-accounts/${encodeURIComponent(username)}/rotate`,
      "POST",
      undefined,
    ),
}
