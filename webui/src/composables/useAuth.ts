import { ref } from "vue"
import { api } from "@/lib/api"
import type { Role } from "@/lib/types"

// Module-level singleton, shared across every component that imports it —
// whether the browser currently holds a valid session cookie (issued by
// POST /v1/login for a registered human account, docs/07-open-questions.md),
// so the webui doesn't need to attach a bearer header to every write
// request itself.
const authenticated = ref(false)
const checked = ref(false)
const username = ref<string | null>(null)
const role = ref<Role | null>(null)

async function refresh() {
  try {
    const session = await api.getSession()
    authenticated.value = session.authenticated
    username.value = session.username ?? null
    role.value = session.role ?? null
  } catch {
    authenticated.value = false
    username.value = null
    role.value = null
  } finally {
    checked.value = true
  }
}

async function login(credentials: { username: string; password: string }) {
  const session = await api.login(credentials) // throws ApiError on bad credentials; caller shows it
  authenticated.value = true
  username.value = session.username ?? null
  role.value = session.role ?? null
}

async function logout() {
  try {
    await api.logout()
  } finally {
    authenticated.value = false
    username.value = null
    role.value = null
  }
}

// True when the current role is one of `roles`. Mirrors the backend's
// requireRole route table (internal/centralserver): admin is only implicitly
// a superset because the backend grants it on every check, so every
// call site here must list "admin" alongside whatever roles it accepts,
// just like the backend does — this helper does no admin special-casing.
function hasRole(...roles: Role[]): boolean {
  return role.value !== null && roles.includes(role.value)
}

// Check session state once, at module load — every component sees the
// same in-flight/resolved state via the shared refs above.
refresh()

export function useAuth() {
  return { authenticated, checked, username, role, login, logout, refresh, hasRole }
}
