<script setup lang="ts">
import { ref, watch } from "vue"
import { toast } from "vue-sonner"
import { Button } from "@/components/ui/button"
import { Textarea } from "@/components/ui/textarea"
import { api } from "@/lib/api"
import type { RecipeSpec } from "@/lib/types"
import { useToken } from "@/composables/useToken"

const props = defineProps<{
  name: string
  spec: RecipeSpec
}>()

const emit = defineEmits<{
  saved: []
}>()

const { token } = useToken()
const text = ref(JSON.stringify(props.spec, null, 2))
const saving = ref(false)

watch(
  () => props.spec,
  (spec) => {
    text.value = JSON.stringify(spec, null, 2)
  },
)

async function save() {
  let spec: RecipeSpec
  try {
    spec = JSON.parse(text.value)
  } catch (e) {
    toast.error("JSON invalide", { description: (e as Error).message })
    return
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
    <Textarea v-model="text" spellcheck="false" class="h-[26rem] font-mono text-xs" />
    <Button :disabled="saving" @click="save">
      {{ saving ? "Enregistrement…" : "Enregistrer (nouvelle version)" }}
    </Button>
  </div>
</template>
