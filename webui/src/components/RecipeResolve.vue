<script setup lang="ts">
import { ref, watch } from "vue"
import { toast } from "vue-sonner"
import { Button } from "@/components/ui/button"
import { api } from "@/lib/api"
import type { ResolveResponse } from "@/lib/types"

const props = defineProps<{
  name: string
}>()

const result = ref<ResolveResponse | null>(null)
const loading = ref(false)

async function resolve() {
  loading.value = true
  try {
    result.value = await api.resolve(props.name)
  } catch (e) {
    toast.error("Échec de la résolution", { description: (e as Error).message })
  } finally {
    loading.value = false
  }
}

watch(
  () => props.name,
  () => {
    result.value = null
  },
)
</script>

<template>
  <div class="space-y-4">
    <Button :disabled="loading" @click="resolve">
      {{ loading ? "Résolution…" : "Résoudre" }}
    </Button>

    <div v-if="result" class="space-y-4">
      <div>
        <h3 class="mb-1.5 text-sm font-semibold">Config</h3>
        <pre class="max-h-72 overflow-auto rounded-md border bg-muted/40 p-3 text-xs">{{ JSON.stringify(result.config, null, 2) }}</pre>
      </div>
      <div>
        <h3 class="mb-1.5 text-sm font-semibold">
          Explain <span class="font-normal text-muted-foreground">(source de chaque valeur)</span>
        </h3>
        <pre class="max-h-72 overflow-auto rounded-md border bg-muted/40 p-3 text-xs">{{ JSON.stringify(result.explain, null, 2) }}</pre>
      </div>
    </div>
  </div>
</template>
