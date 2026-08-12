import { ref, watch } from "vue"

const STORAGE_KEY = "cb-token"

// Shared across every component that imports it (module-level singleton) —
// the write token gating PUT/rollback (docs/05-recipe-and-crd.md §5.3).
const token = ref(localStorage.getItem(STORAGE_KEY) ?? "")
watch(token, (v) => localStorage.setItem(STORAGE_KEY, v))

export function useToken() {
  return { token }
}
