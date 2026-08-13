<script setup lang="ts">
import { computed, ref } from "vue"
import { toast } from "vue-sonner"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { Textarea } from "@/components/ui/textarea"
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table"
import { api } from "@/lib/api"
import type { Credentials, SourceConfig } from "@/lib/types"
import { useAuth } from "@/composables/useAuth"

const { hasRole } = useAuth()
const sources = ref<SourceConfig[]>([])
const loading = ref(false)

const newName = ref("")
const newRepo = ref("")
const authMode = ref<"none" | "https" | "ssh">("none")
const newUsername = ref("")
const newPassword = ref("")
const newSshKey = ref("")
const newSshUser = ref("")
const newSshKeyPassphrase = ref("")
const adding = ref(false)
const testing = ref(false)

// Built fresh from the form on every add()/test() call rather than kept as
// a ref, so a credential never lingers in state longer than the action
// that needs it (docs/05-recipe-and-crd.md §5.2bis: write-only by design —
// the form itself should hold onto it no longer than it has to either).
function currentAuth(): Credentials | undefined {
  if (authMode.value === "https" && (newUsername.value || newPassword.value)) {
    return { username: newUsername.value.trim(), password: newPassword.value }
  }
  if (authMode.value === "ssh" && newSshKey.value.trim()) {
    return {
      sshKey: newSshKey.value.trim(),
      sshUser: newSshUser.value.trim() || undefined,
      sshKeyPassphrase: newSshKeyPassphrase.value || undefined,
    }
  }
  return undefined
}

const canTest = computed(() => newRepo.value.trim().length > 0)
const canWrite = computed(() => hasRole("admin", "source-manager"))

function clearCredentialFields() {
  newUsername.value = ""
  newPassword.value = ""
  newSshKey.value = ""
  newSshUser.value = ""
  newSshKeyPassphrase.value = ""
}

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

function requireAuth(): boolean {
  if (hasRole("admin", "source-manager")) return true
  toast.error("Accès refusé", { description: "Rôle admin ou source-manager requis." })
  return false
}

async function test() {
  const repo = newRepo.value.trim()
  if (!repo || !requireAuth()) return

  testing.value = true
  try {
    const result = await api.testConnection(repo, currentAuth())
    if (result.ok) {
      toast.success("Connexion réussie", { description: repo })
    } else {
      toast.error("Échec de la connexion", { description: result.error || "raison inconnue" })
    }
  } catch (e) {
    toast.error("Échec de la connexion", { description: (e as Error).message })
  } finally {
    testing.value = false
  }
}

async function add() {
  const name = newName.value.trim()
  const repo = newRepo.value.trim()
  if (!name || !repo || !requireAuth()) return

  adding.value = true
  try {
    await api.putSource({ name, repo, auth: currentAuth() })
    toast.success(`Source « ${name} » enregistrée.`)
    newName.value = ""
    newRepo.value = ""
    authMode.value = "none"
    clearCredentialFields()
    await load()
  } catch (e) {
    toast.error("Échec de l'enregistrement", { description: (e as Error).message })
  } finally {
    adding.value = false
  }
}

async function remove(name: string) {
  if (!requireAuth()) return
  try {
    await api.deleteSource(name)
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
        jamais saisie côté Recipe. Les identifiants Git, le cas échéant, sont stockés avec la source
        (docs/04-kubernetes.md §4.1) : ils ne sont jamais renvoyés par l'API — seule leur saisie est
        possible, jamais leur relecture. Utilisez « Tester la connexion » pour vérifier un dépôt et ses
        identifiants avant de les enregistrer.
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
            <Button variant="destructive" size="sm" :disabled="!canWrite" @click="remove(src.name)">Supprimer</Button>
          </TableCell>
        </TableRow>
        <TableRow v-if="!loading && sources.length === 0">
          <TableCell colspan="3" class="text-sm text-muted-foreground">Aucune source enregistrée.</TableCell>
        </TableRow>
      </TableBody>
    </Table>

    <div class="space-y-3 rounded-md border p-3">
      <div class="flex items-end gap-2">
        <div class="flex-1 space-y-1">
          <Label class="text-xs text-muted-foreground">Nom</Label>
          <Input v-model="newName" placeholder="internal-configs" class="h-8 text-sm" />
        </div>
        <div class="flex-[2] space-y-1">
          <Label class="text-xs text-muted-foreground">Dépôt Git</Label>
          <Input
            v-model="newRepo"
            placeholder="git@github.com:org/repo.git"
            class="h-8 text-sm"
          />
        </div>
      </div>

      <div class="space-y-2">
        <Label class="text-xs text-muted-foreground">Identifiants (optionnel, dépôt privé)</Label>
        <Tabs v-model="authMode">
          <TabsList>
            <TabsTrigger value="none">Aucun</TabsTrigger>
            <TabsTrigger value="https">HTTPS</TabsTrigger>
            <TabsTrigger value="ssh">SSH</TabsTrigger>
          </TabsList>

          <TabsContent value="https">
            <div class="grid grid-cols-2 gap-2 pt-2">
              <div class="space-y-1">
                <Label class="text-xs text-muted-foreground">Utilisateur</Label>
                <Input v-model="newUsername" placeholder="x-access-token" class="h-8 text-sm" />
              </div>
              <div class="space-y-1">
                <Label class="text-xs text-muted-foreground">Mot de passe / token</Label>
                <Input v-model="newPassword" type="password" class="h-8 text-sm" autocomplete="off" />
              </div>
            </div>
          </TabsContent>

          <TabsContent value="ssh">
            <div class="space-y-2 pt-2">
              <div class="space-y-1">
                <Label class="text-xs text-muted-foreground">Clé privée (PEM)</Label>
                <Textarea
                  v-model="newSshKey"
                  rows="4"
                  class="font-mono text-xs"
                  placeholder="-----BEGIN OPENSSH PRIVATE KEY-----"
                  autocomplete="off"
                />
              </div>
              <div class="grid grid-cols-2 gap-2">
                <div class="space-y-1">
                  <Label class="text-xs text-muted-foreground">Utilisateur SSH</Label>
                  <Input v-model="newSshUser" placeholder="git" class="h-8 text-sm" />
                </div>
                <div class="space-y-1">
                  <Label class="text-xs text-muted-foreground">Passphrase</Label>
                  <Input v-model="newSshKeyPassphrase" type="password" class="h-8 text-sm" autocomplete="off" />
                </div>
              </div>
            </div>
          </TabsContent>
        </Tabs>
      </div>

      <div class="flex justify-end gap-2">
        <Button variant="outline" :disabled="!canTest || !canWrite || testing" @click="test">
          {{ testing ? "Test en cours…" : "Tester la connexion" }}
        </Button>
        <Button :disabled="!canWrite || adding" @click="add">
          {{ adding ? "Ajout…" : "Ajouter" }}
        </Button>
      </div>
    </div>
  </div>
</template>
