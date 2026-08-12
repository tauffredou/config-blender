<script setup lang="ts">
import { computed, ref } from "vue"
import { ChevronDownIcon, ChevronRightIcon } from "@lucide/vue"
import { Badge } from "@/components/ui/badge"
import { FALLBACK_BADGE_CLASS } from "@/lib/layerColor"
import type { GitSource } from "@/lib/types"

interface Provenance {
  layer: string
  source?: GitSource
}

const props = defineProps<{
  name: string
  configValue: unknown
  explainValue: unknown
  layerBadgeClass: Map<string, string>
}>()

defineOptions({ name: "ExplainNode" })

function isPlainObject(v: unknown): v is Record<string, unknown> {
  return typeof v === "object" && v !== null && !Array.isArray(v)
}

const isObject = computed(() => isPlainObject(props.configValue))
const isList = computed(() => Array.isArray(props.configValue))

const childKeys = computed(() =>
  isObject.value ? Object.keys(props.configValue as Record<string, unknown>).sort() : [],
)
const explainObj = computed<Record<string, unknown>>(() =>
  isPlainObject(props.explainValue) ? (props.explainValue as Record<string, unknown>) : {},
)

const provenance = computed<Provenance | null>(() => {
  if (isObject.value) return null
  const e = props.explainValue
  if (isPlainObject(e) && typeof e.layer === "string") return e as unknown as Provenance
  return null
})

const badgeClass = computed(() => {
  if (!provenance.value) return FALLBACK_BADGE_CLASS
  return props.layerBadgeClass.get(provenance.value.layer) ?? FALLBACK_BADGE_CLASS
})

const sourceTitle = computed(() => {
  const p = provenance.value
  if (!p) return undefined
  if (!p.source) return `couche : ${p.layer}`
  return `couche : ${p.layer}\n${p.source.repo}\n${p.source.path} @ ${p.source.ref}`
})

function formatScalar(v: unknown): string {
  if (v === null || v === undefined) return "null"
  if (typeof v === "string") return v
  return JSON.stringify(v)
}

const open = ref(true)
</script>

<template>
  <div>
    <template v-if="isObject">
      <div class="flex items-center gap-1 py-0.5">
        <button
          type="button"
          class="text-muted-foreground hover:text-foreground"
          @click="open = !open"
        >
          <ChevronDownIcon v-if="open" class="size-3.5" />
          <ChevronRightIcon v-else class="size-3.5" />
        </button>
        <span class="font-mono text-xs font-medium">{{ name }}</span>
      </div>

      <div v-if="open" class="ml-2.5 space-y-0.5 border-l pl-3">
        <ExplainNode
          v-for="key in childKeys"
          :key="key"
          :name="key"
          :config-value="(configValue as Record<string, unknown>)[key]"
          :explain-value="explainObj[key]"
          :layer-badge-class="layerBadgeClass"
        />
      </div>
    </template>

    <div v-else-if="isList" class="py-0.5 pl-[18px]">
      <div class="flex flex-wrap items-baseline gap-x-2 gap-y-0.5 font-mono text-xs">
        <span class="text-muted-foreground">{{ name }}:</span>
        <Badge v-if="provenance" variant="outline" :class="badgeClass" :title="sourceTitle">
          {{ provenance.layer }}
        </Badge>
      </div>
      <ul class="ml-2.5 border-l pl-3">
        <li v-for="(item, i) in (configValue as unknown[])" :key="i" class="font-mono text-xs">
          - {{ formatScalar(item) }}
        </li>
      </ul>
    </div>

    <div v-else class="flex flex-wrap items-baseline gap-x-2 gap-y-0.5 py-0.5 pl-[18px] font-mono text-xs">
      <span class="text-muted-foreground">{{ name }}:</span>
      <span>{{ formatScalar(configValue) }}</span>
      <Badge v-if="provenance" variant="outline" :class="badgeClass" :title="sourceTitle">
        {{ provenance.layer }}
      </Badge>
    </div>
  </div>
</template>
