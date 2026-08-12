<script setup lang="ts">
import { computed, ref, watch } from "vue"
import { toast } from "vue-sonner"
import { Button } from "@/components/ui/button"
import { Badge } from "@/components/ui/badge"
import { api } from "@/lib/api"
import { layerColorMap } from "@/lib/layerColor"
import ExplainNode from "@/components/ExplainNode.vue"
import type { ResolveResponse } from "@/lib/types"

const props = defineProps<{
  name: string
  layers: string[]
}>()

const result = ref<ResolveResponse | null>(null)
const loading = ref(false)
const showRaw = ref(false)

const layerBadgeClass = computed(() => layerColorMap(props.layers))
const configKeys = computed(() => (result.value ? Object.keys(result.value.config).sort() : []))

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
      <div class="flex items-center justify-between">
        <h3 class="text-sm font-semibold">
          Config <span class="font-normal text-muted-foreground">(survolez une couche pour sa source Git)</span>
        </h3>
        <div class="flex items-center gap-1.5">
          <Badge
            v-for="layer in layers"
            :key="layer"
            variant="outline"
            :class="layerBadgeClass.get(layer)"
          >
            {{ layer }}
          </Badge>
        </div>
      </div>

      <div class="max-h-96 overflow-auto rounded-md border p-3">
        <ExplainNode
          v-for="key in configKeys"
          :key="key"
          :name="key"
          :config-value="result.config[key]"
          :explain-value="result.explain?.[key]"
          :layer-badge-class="layerBadgeClass"
        />
      </div>

      <Button variant="ghost" size="sm" class="text-xs text-muted-foreground" @click="showRaw = !showRaw">
        {{ showRaw ? "Masquer le JSON brut" : "Voir le JSON brut" }}
      </Button>
      <div v-if="showRaw" class="space-y-4">
        <div>
          <h4 class="mb-1.5 text-sm font-semibold">Config</h4>
          <pre class="max-h-72 overflow-auto rounded-md border bg-muted/40 p-3 text-xs">{{ JSON.stringify(result.config, null, 2) }}</pre>
        </div>
        <div>
          <h4 class="mb-1.5 text-sm font-semibold">Explain</h4>
          <pre class="max-h-72 overflow-auto rounded-md border bg-muted/40 p-3 text-xs">{{ JSON.stringify(result.explain, null, 2) }}</pre>
        </div>
      </div>
    </div>
  </div>
</template>
