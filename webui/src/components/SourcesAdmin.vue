<script setup lang="ts">
import { ref } from "vue"
import { toast } from "vue-sonner"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table"
import { api } from "@/lib/api"
import type { SourceConfig } from "@/lib/types"
import { useToken } from "@/composables/useToken"

const { token } = useToken()
const sources = ref<SourceConfig[]>([])
const loading = ref(false)

const newName = ref("")
const newRepo = ref("")
const adding = ref(false)

async function load() {
  loading.value = true
  try {
    const data = await api.listSources()
    sources.value = data.sources
  } catch (e) {
    toast.error("Erreur de chargement des sources", { description: (e as Error).message })
  } finally {
    loading.value = false
  }
}
load()

async function add() {
  const name = newName.value.trim()
  const repo = newRepo.value.trim()
  if (!name || !repo) return
  if (!token.value) {
    toast.error("Token d'écriture requis", { description: "Renseignez-le en haut à droite." })
    return
  }

  adding.value = true
  try {
    await api.putSource({ name, repo }, token.value)
    toast.success(`Source « ${name} » enregistrée.`)
    newName.value = ""
    newRepo.value = ""
    await load()
  } catch (e) {
    toast.error("Échec de l'enregistrement", { description: (e as Error).message })
  } finally {
    adding.value = false
  }
}

async function remove(name: string) {
  if (!token.value) {
    toast.error("Token d'écriture requis", { description: "Renseignez-le en haut à droite." })
    return
  }
  try {
    await api.deleteSource(name, token.value)
    toast.success(`Source « ${name} » supprimée.`)
    await load()
  } catch (e) {
    toast.error("Échec de la suppression", { description: (e as Error).message })
  }
}
</script>

<template>
  <div class="space-y-4">
    <div>
      <h2 class="text-lg font-semibold">Sources Git</h2>
      <p class="text-sm text-muted-foreground">
        Dépôts préconfigurés, référencés par nom depuis le builder de couches — l'URL du dépôt n'est
        jamais saisie côté Recipe, et aucun identifiant n'est stocké ici : les identifiants Git sont
        résolus depuis l'environnement, par source (GIT_TOKEN_&lt;NOM&gt;, etc.).
      </p>
    </div>

    <Table>
      <TableHeader>
        <TableRow>
          <TableHead>Nom</TableHead>
          <TableHead>Dépôt</TableHead>
          <TableHead class="text-right">Actions</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        <TableRow v-for="src in sources" :key="src.name">
          <TableCell class="font-mono text-xs">{{ src.name }}</TableCell>
          <TableCell class="font-mono text-xs text-muted-foreground">{{ src.repo }}</TableCell>
          <TableCell class="text-right">
            <Button variant="destructive" size="sm" @click="remove(src.name)">Supprimer</Button>
          </TableCell>
        </TableRow>
        <TableRow v-if="!loading && sources.length === 0">
          <TableCell colspan="3" class="text-sm text-muted-foreground">Aucune source enregistrée.</TableCell>
        </TableRow>
      </TableBody>
    </Table>

    <div class="flex items-end gap-2 rounded-md border p-3">
      <div class="flex-1 space-y-1">
        <Label class="text-xs text-muted-foreground">Nom</Label>
        <Input v-model="newName" placeholder="internal-configs" class="h-8 text-sm" @keyup.enter="add" />
      </div>
      <div class="flex-[2] space-y-1">
        <Label class="text-xs text-muted-foreground">Dépôt Git</Label>
        <Input
          v-model="newRepo"
          placeholder="git@github.com:org/repo.git"
          class="h-8 text-sm"
          @keyup.enter="add"
        />
      </div>
      <Button :disabled="adding" @click="add">
        {{ adding ? "Ajout…" : "Ajouter" }}
      </Button>
    </div>
  </div>
</template>
