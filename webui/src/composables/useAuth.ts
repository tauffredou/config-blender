import { ref } from "vue"
import { api } from "@/lib/api"

// Module-level singleton, shared across every component that imports it —
// whether the browser currently holds a valid session cookie (issued by
// POST /v1/login, docs/07-open-questions.md: session auth over the same
// shared write token, so the webui doesn't need to attach a bearer header
// to every write request itself).
const authenticated = ref(false)
const checked = ref(false)

async function refresh() {
  try {
    const session = await api.getSession()
    authenticated.value = session.authenticated
  } catch {
    authenticated.value = false
  } finally {
    checked.value = true
  }
}

async function login(token: string) {
  await api.login(token) // throws ApiError on a wrong token; caller shows it
  authenticated.value = true
}

async function logout() {
  try {
    await api.logout()
  } finally {
    authenticated.value = false
  }
}

// Check session state once, at module load — every component sees the
// same in-flight/resolved state via the shared refs above.
refresh()

export function useAuth() {
  return { authenticated, checked, login, logout, refresh }
}
