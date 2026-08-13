<script setup lang="ts">
import { computed, ref, watch } from "vue"
import { toast } from "vue-sonner"
import { Button } from "@/components/ui/button"
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import { api } from "@/lib/api"
import { diffLines, type DiffOp } from "@/lib/diff"
import type { RecipeSpec, VersionEntry } from "@/lib/types"
import { useAuth } from "@/composables/useAuth"

const props = defineProps<{
  name: string
  latestSpec: RecipeSpec
}>()

const emit = defineEmits<{
  rolledBack: []
}>()

const { hasRole } = useAuth()
const canWrite = computed(() => hasRole("admin", "contributor"))
const versions = ref<VersionEntry[]>([])
const diff = ref<DiffOp[] | null>(null)
const diffFromVersion = ref<number | null>(null)
const confirmVersion = ref<number | null>(null)

async function load() {
  diff.value = null
  try {
    const data = await api.listVersions(props.name)
    versions.value = data.versions.slice().reverse()
  } catch (e) {
    toast.error("Erreur de chargement de l'historique", { description: (e as Error).message })
  }
}
watch(() => props.name, load, { immediate: true })

async function compareToLatest(version: number) {
  try {
    const old = await api.getRecipeVersion(props.name, version)
    diff.value = diffLines(JSON.stringify(old, null, 2), JSON.stringify(props.latestSpec, null, 2))
    diffFromVersion.value = version
  } catch (e) {
    toast.error("Erreur de comparaison", { description: (e as Error).message })
  }
}

async function confirmRollback() {
  const version = confirmVersion.value
  if (version === null) return
  if (!canWrite.value) {
    toast.error("Accès refusé", { description: "Rôle admin ou contributor requis." })
    confirmVersion.value = null
    return
  }
  try {
    await api.rollback(props.name, version)
    toast.success(`Rollback vers la version ${version} effectué.`)
    confirmVersion.value = null
    emit("rolledBack")
    load()
  } catch (e) {
    toast.error("Échec du rollback", { description: (e as Error).message })
  }
}
</script>

<template>
  <div class="space-y-4">
    <Table>
      <TableHeader>
        <TableRow>
          <TableHead>Version</TableHead>
          <TableHead>Date</TableHead>
          <TableHead class="text-right">Actions</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        <TableRow v-for="v in versions" :key="v.version">
          <TableCell>{{ v.version }}</TableCell>
          <TableCell>{{ new Date(v.updatedAt).toLocaleString() }}</TableCell>
          <TableCell class="flex justify-end gap-2">
            <Button variant="secondary" size="sm" @click="compareToLatest(v.version)">
              Comparer à la dernière
            </Button>
            <Button variant="destructive" size="sm" :disabled="!canWrite" @click="confirmVersion = v.version">
              Rollback
            </Button>
          </TableCell>
        </TableRow>
      </TableBody>
    </Table>

    <div v-if="diff" class="rounded-md border">
      <div class="border-b bg-muted px-3 py-1.5 text-xs font-medium">
        Version {{ diffFromVersion }} → dernière version
      </div>
      <div class="max-h-80 overflow-auto font-mono text-xs">
        <div
          v-for="(d, i) in diff"
          :key="i"
          class="px-3 whitespace-pre"
          :class="{
            'bg-green-50 text-green-700 dark:bg-green-950 dark:text-green-400': d.t === 'add',
            'bg-red-50 text-red-700 dark:bg-red-950 dark:text-red-400': d.t === 'del',
          }"
        >{{ d.t === "add" ? "+ " : d.t === "del" ? "- " : "  " }}{{ d.line }}</div>
      </div>
    </div>

    <Dialog :open="confirmVersion !== null" @update:open="(o: boolean) => !o && (confirmVersion = null)">
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Confirmer le rollback</DialogTitle>
          <DialogDescription>
            Restaurer la version {{ confirmVersion }} de « {{ name }} » comme nouvelle version ?
            L'historique n'est jamais réécrit — ceci crée une nouvelle version avec ce contenu.
          </DialogDescription>
        </DialogHeader>
        <DialogFooter>
          <Button variant="secondary" @click="confirmVersion = null">Annuler</Button>
          <Button variant="destructive" @click="confirmRollback">Confirmer le rollback</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  </div>
</template>
