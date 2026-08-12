<script setup lang="ts">
import { ref, watch } from "vue"
import { toast } from "vue-sonner"
import { ArrowDown, ArrowUp, Plus, Trash2 } from "@lucide/vue"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Card, CardContent } from "@/components/ui/card"
import { Textarea } from "@/components/ui/textarea"
import { api } from "@/lib/api"
import type { LayerSpec, RecipeSpec } from "@/lib/types"
import { useToken } from "@/composables/useToken"

const props = defineProps<{
  name: string
  spec: RecipeSpec
}>()

const emit = defineEmits<{
  saved: []
}>()

const { token } = useToken()
const mode = ref<"builder" | "json">("builder")
const specDraft = ref<RecipeSpec>(cloneSpec(props.spec))
const jsonText = ref(JSON.stringify(specDraft.value, null, 2))
const saving = ref(false)

function cloneSpec(spec: RecipeSpec): RecipeSpec {
  // JSON round-trip rather than structuredClone: spec is a reactive Vue
  // prop (a Proxy), which structuredClone refuses to clone.
  return JSON.parse(JSON.stringify({ ...spec, layers: spec.layers ?? [] }))
}

watch(
  () => props.spec,
  (spec) => {
    specDraft.value = cloneSpec(spec)
    jsonText.value = JSON.stringify(specDraft.value, null, 2)
  },
)

function switchMode(next: "builder" | "json") {
  if (next === mode.value) return
  if (next === "json") {
    jsonText.value = JSON.stringify(specDraft.value, null, 2)
  } else {
    try {
      specDraft.value = cloneSpec(JSON.parse(jsonText.value))
    } catch (e) {
      toast.error("JSON invalide", { description: (e as Error).message })
      return
    }
  }
  mode.value = next
}

function addLayer() {
  const layer: LayerSpec = {
    name: `layer-${specDraft.value.layers.length + 1}`,
    type: "static",
    source: { repo: "", path: "", ref: "" },
  }
  specDraft.value.layers.push(layer)
}

function removeLayer(index: number) {
  specDraft.value.layers.splice(index, 1)
}

function moveLayer(index: number, direction: -1 | 1) {
  const target = index + direction
  if (target < 0 || target >= specDraft.value.layers.length) return
  const layers = specDraft.value.layers
  ;[layers[index], layers[target]] = [layers[target], layers[index]]
}

async function save() {
  let spec: RecipeSpec
  if (mode.value === "json") {
    try {
      spec = JSON.parse(jsonText.value)
    } catch (e) {
      toast.error("JSON invalide", { description: (e as Error).message })
      return
    }
  } else {
    spec = specDraft.value
  }
  if (!token.value) {
    toast.error("Token d'écriture requis", { description: "Renseignez-le en haut à droite." })
    return
  }

  saving.value = true
  try {
    await api.putRecipe(props.name, spec, token.value)
    toast.success(`Nouvelle version de « ${props.name} » enregistrée.`)
    emit("saved")
  } catch (e) {
    toast.error("Échec de l'enregistrement", { description: (e as Error).message })
  } finally {
    saving.value = false
  }
}
</script>

<template>
  <div class="space-y-3">
    <div class="flex gap-1">
      <Button :variant="mode === 'builder' ? 'secondary' : 'ghost'" size="sm" @click="switchMode('builder')">
        Builder
      </Button>
      <Button :variant="mode === 'json' ? 'secondary' : 'ghost'" size="sm" @click="switchMode('json')">
        JSON avancé
      </Button>
    </div>

    <div v-if="mode === 'builder'" class="space-y-3">
      <p class="text-xs text-muted-foreground">
        Couches appliquées dans l'ordre — chaque couche suivante fusionne par-dessus les précédentes.
      </p>

      <Card v-for="(layer, i) in specDraft.layers" :key="i" class="py-3">
        <CardContent class="space-y-2.5 px-3">
          <div class="flex items-start justify-between gap-2">
            <div class="grid flex-1 grid-cols-2 gap-2">
              <div class="space-y-1">
                <Label class="text-xs text-muted-foreground">Nom</Label>
                <Input v-model="layer.name" placeholder="base" class="h-7 text-xs" />
              </div>
              <div class="space-y-1">
                <Label class="text-xs text-muted-foreground">Type</Label>
                <select
                  v-model="layer.type"
                  class="dark:bg-input/30 border-input focus-visible:border-ring focus-visible:ring-ring/50 h-7 w-full rounded-lg border bg-transparent px-2 text-xs outline-none focus-visible:ring-3"
                >
                  <option value="static">Statique (YAML/JSON)</option>
                  <option value="dynamic">Dynamique (Starlark)</option>
                </select>
              </div>
            </div>
            <div class="flex gap-0.5 pt-5">
              <Button variant="ghost" size="icon" class="size-7" :disabled="i === 0" @click="moveLayer(i, -1)">
                <ArrowUp class="size-3.5" />
              </Button>
              <Button
                variant="ghost"
                size="icon"
                class="size-7"
                :disabled="i === specDraft.layers.length - 1"
                @click="moveLayer(i, 1)"
              >
                <ArrowDown class="size-3.5" />
              </Button>
              <Button variant="ghost" size="icon" class="size-7 text-destructive" @click="removeLayer(i)">
                <Trash2 class="size-3.5" />
              </Button>
            </div>
          </div>

          <div class="grid grid-cols-3 gap-2">
            <div class="space-y-1">
              <Label class="text-xs text-muted-foreground">Repo Git</Label>
              <Input v-model="layer.source.repo" placeholder="git@github.com:org/repo.git" class="h-7 text-xs" />
            </div>
            <div class="space-y-1">
              <Label class="text-xs text-muted-foreground">Chemin</Label>
              <Input
                v-model="layer.source.path"
                :placeholder="layer.type === 'dynamic' ? 'compute.star' : 'base.yaml'"
                class="h-7 text-xs"
              />
            </div>
            <div class="space-y-1">
              <Label class="text-xs text-muted-foreground">Ref</Label>
              <Input v-model="layer.source.ref" placeholder="main" class="h-7 text-xs" />
            </div>
          </div>
        </CardContent>
      </Card>

      <Button variant="secondary" size="sm" @click="addLayer">
        <Plus class="size-3.5" />
        Ajouter une couche
      </Button>

      <p v-if="specDraft.layers.length > 1" class="text-xs text-muted-foreground">
        `mergePolicy` (stratégie de fusion des listes) reste éditable en JSON avancé.
      </p>
    </div>

    <Textarea v-else v-model="jsonText" spellcheck="false" class="h-[26rem] font-mono text-xs" />

    <Button :disabled="saving" @click="save">
      {{ saving ? "Enregistrement…" : "Enregistrer (nouvelle version)" }}
    </Button>
  </div>
</template>
