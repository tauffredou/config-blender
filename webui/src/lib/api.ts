// Thin client for the central service's HTTP API (internal/centralserver).
// Reads are unauthenticated; writes (put/rollback) require a bearer token
// (docs/05-recipe-and-crd.md §5.3).
import type { RecipeSpec, VersionEntry, ResolveResponse, SourceConfig } from "./types"

class ApiError extends Error {}

async function request<T>(path: string, opts: RequestInit = {}, token?: string): Promise<T> {
  const headers = new Headers(opts.headers)
  if (token) headers.set("Authorization", `Bearer ${token}`)
  const res = await fetch(path, { ...opts, headers })
  if (!res.ok) {
    const text = await res.text()
    throw new ApiError(`${res.status}: ${text.trim()}`)
  }
  if (res.status === 204) return undefined as T
  const contentType = res.headers.get("content-type") ?? ""
  if (contentType.includes("application/json")) return (await res.json()) as T
  return undefined as T
}

export const api = {
  listRecipes: () => request<{ names: string[] }>("/v1/recipes"),

  getRecipe: (name: string) => request<RecipeSpec>(`/v1/recipes/${encodeURIComponent(name)}`),

  getRecipeVersion: (name: string, version: number) =>
    request<RecipeSpec>(`/v1/recipes/${encodeURIComponent(name)}/versions/${version}`),

  listVersions: (name: string) =>
    request<{ versions: VersionEntry[] }>(`/v1/recipes/${encodeURIComponent(name)}/versions`),

  putRecipe: (name: string, spec: RecipeSpec, token: string) =>
    request<void>(
      `/v1/recipes/${encodeURIComponent(name)}`,
      {
        method: "PUT",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(spec),
      },
      token,
    ),

  rollback: (name: string, version: number, token: string) =>
    request<void>(
      `/v1/recipes/${encodeURIComponent(name)}/rollback`,
      {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ version }),
      },
      token,
    ),

  resolve: (name: string) => request<ResolveResponse>(`/v1/resolve?recipe=${encodeURIComponent(name)}`),

  listSources: () => request<{ sources: SourceConfig[] }>("/v1/sources"),

  putSource: (source: SourceConfig, token: string) =>
    request<void>(
      `/v1/sources/${encodeURIComponent(source.name)}`,
      {
        method: "PUT",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(source),
      },
      token,
    ),

  deleteSource: (name: string, token: string) =>
    request<void>(`/v1/sources/${encodeURIComponent(name)}`, { method: "DELETE" }, token),
}
