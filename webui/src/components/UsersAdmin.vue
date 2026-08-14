<script setup lang="ts">
import { computed, ref } from "vue"
import { toast } from "vue-sonner"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table"
import { api } from "@/lib/api"
import type { Role, User } from "@/lib/types"
import { useAuth } from "@/composables/useAuth"

const { username: currentUsername } = useAuth()
const users = ref<User[]>([])
const loading = ref(false)

const roles: Role[] = ["admin", "source-manager", "contributor", "read"]

const newKind = ref<"human" | "service">("human")
const newUsername = ref("")
const newPassword = ref("")
const newRole = ref<Role>("contributor")
const adding = ref(false)

// Per-row role-select in-flight state, keyed by username, so one row's
// "saving" state never visually blocks the others.
const updatingRole = ref<Record<string, boolean>>({})
const deleting = ref<Record<string, boolean>>({})
const rotating = ref<Record<string, boolean>>({})

// A freshly generated API key is shown exactly once (create or rotate) —
// the backend never returns it again, so this dialog is the only chance to
// copy it.
const revealedKey = ref<{ username: string; apiKey: string } | null>(null)
const revealOpen = computed({
  get: () => revealedKey.value !== null,
  set: (open: boolean) => {
    if (!open) revealedKey.value = null
  },
})

async function copyKey() {
  if (!revealedKey.value) return
  try {
    await navigator.clipboard.writeText(revealedKey.value.apiKey)
    toast.success("Clé copiée dans le presse-papiers.")
  } catch (e) {
    toast.error("Échec de la copie", { description: (e as Error).message })
  }
}

async function load() {
  loading.value = true
  try {
    const data = await api.listUsers()
    users.value = data.users
  } catch (e) {
    toast.error("Erreur de chargement des utilisateurs", { description: (e as Error).message })
  } finally {
    loading.value = false
  }
}
load()

async function changeRole(user: User, role: Role) {
  if (role === user.role) return
  const previous = user.role
  updatingRole.value[user.username] = true
  try {
    await api.updateUser(user.username, { role })
    user.role = role
    toast.success(`Rôle de « ${user.username} » mis à jour.`)
  } catch (e) {
    user.role = previous
    toast.error("Échec de la mise à jour du rôle", { description: (e as Error).message })
  } finally {
    updatingRole.value[user.username] = false
  }
}

async function remove(user: User) {
  if (user.username === currentUsername.value) return
  deleting.value[user.username] = true
  try {
    await api.deleteUser(user.username)
    toast.success(`Compte « ${user.username} » supprimé.`)
    await load()
  } catch (e) {
    toast.error("Échec de la suppression", { description: (e as Error).message })
  } finally {
    deleting.value[user.username] = false
  }
}

async function rotate(user: User) {
  rotating.value[user.username] = true
  try {
    const result = await api.rotateServiceAccountKey(user.username)
    revealedKey.value = { username: user.username, apiKey: result.apiKey }
    toast.success(`Clé de « ${user.username} » régénérée — l'ancienne est immédiatement invalide.`)
  } catch (e) {
    toast.error("Échec de la rotation", { description: (e as Error).message })
  } finally {
    rotating.value[user.username] = false
  }
}

async function add() {
  const username = newUsername.value.trim()
  if (!username) return

  adding.value = true
  try {
    if (newKind.value === "service") {
      const result = await api.createServiceAccount(username, newRole.value)
      revealedKey.value = { username, apiKey: result.apiKey }
    } else {
      if (!newPassword.value) return
      await api.createUser(username, newPassword.value, newRole.value)
      toast.success(`Utilisateur « ${username} » créé.`)
    }
    newUsername.value = ""
    newPassword.value = ""
    newRole.value = "contributor"
    await load()
  } catch (e) {
    toast.error("Échec de la création", { description: (e as Error).message })
  } finally {
    adding.value = false
  }
}
</script>

<template>
  <div class="space-y-4">
    <div>
      <h2 class="text-lg font-semibold">Utilisateurs</h2>
      <p class="text-sm text-muted-foreground">
        Comptes nommés avec un rôle (admin, source-manager, contributor, read). Réservé aux
        administrateurs. Un compte humain se connecte par mot de passe (session) ; un compte de
        service s'authentifie avec une clé d'API envoyée en <code>Authorization: Bearer</code> à
        chaque requête, sans session — même modèle de rôles pour les deux. Le token d'écriture
        partagé reste disponible comme accès de secours ("token"), en dehors de cette liste.
      </p>
    </div>

    <Table>
      <TableHeader>
        <TableRow>
          <TableHead>Compte</TableHead>
          <TableHead>Type</TableHead>
          <TableHead>Rôle</TableHead>
          <TableHead>Créé le</TableHead>
          <TableHead class="text-right">Actions</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        <TableRow v-for="user in users" :key="user.username">
          <TableCell class="font-mono text-xs">{{ user.username }}</TableCell>
          <TableCell class="text-xs text-muted-foreground">
            {{ user.kind === "service" ? "service" : "humain" }}
          </TableCell>
          <TableCell>
            <select
              :value="user.role"
              :disabled="updatingRole[user.username]"
              class="dark:bg-input/30 border-input focus-visible:border-ring focus-visible:ring-ring/50 h-7 rounded-lg border bg-transparent px-2 text-xs outline-none focus-visible:ring-3"
              @change="changeRole(user, ($event.target as HTMLSelectElement).value as Role)"
            >
              <option v-for="r in roles" :key="r" :value="r">{{ r }}</option>
            </select>
          </TableCell>
          <TableCell class="text-xs text-muted-foreground">{{ new Date(user.createdAt).toLocaleString() }}</TableCell>
          <TableCell class="text-right space-x-2">
            <Button
              v-if="user.kind === 'service'"
              variant="outline"
              size="sm"
              :disabled="rotating[user.username]"
              @click="rotate(user)"
            >
              {{ rotating[user.username] ? "…" : "Régénérer la clé" }}
            </Button>
            <Button
              variant="destructive"
              size="sm"
              :disabled="user.username === currentUsername || deleting[user.username]"
              :title="user.username === currentUsername ? 'Impossible de supprimer votre propre compte.' : undefined"
              @click="remove(user)"
            >
              Supprimer
            </Button>
          </TableCell>
        </TableRow>
        <TableRow v-if="!loading && users.length === 0">
          <TableCell colspan="5" class="text-sm text-muted-foreground">Aucun compte enregistré.</TableCell>
        </TableRow>
      </TableBody>
    </Table>

    <div class="space-y-3 rounded-md border p-3">
      <Tabs v-model="newKind">
        <TabsList>
          <TabsTrigger value="human">Compte humain</TabsTrigger>
          <TabsTrigger value="service">Compte de service</TabsTrigger>
        </TabsList>

        <TabsContent value="human">
          <div class="flex items-end gap-2 pt-2">
            <div class="flex-1 space-y-1">
              <Label class="text-xs text-muted-foreground">Utilisateur</Label>
              <Input v-model="newUsername" placeholder="alice" class="h-8 text-sm" />
            </div>
            <div class="flex-1 space-y-1">
              <Label class="text-xs text-muted-foreground">Mot de passe</Label>
              <Input v-model="newPassword" type="password" class="h-8 text-sm" autocomplete="off" />
            </div>
            <div class="flex-1 space-y-1">
              <Label class="text-xs text-muted-foreground">Rôle</Label>
              <select
                v-model="newRole"
                class="dark:bg-input/30 border-input focus-visible:border-ring focus-visible:ring-ring/50 h-8 w-full rounded-lg border bg-transparent px-2 text-sm outline-none focus-visible:ring-3"
              >
                <option v-for="r in roles" :key="r" :value="r">{{ r }}</option>
              </select>
            </div>
          </div>
        </TabsContent>

        <TabsContent value="service">
          <div class="flex items-end gap-2 pt-2">
            <div class="flex-1 space-y-1">
              <Label class="text-xs text-muted-foreground">Nom du compte de service</Label>
              <Input v-model="newUsername" placeholder="ci-bot" class="h-8 text-sm" />
            </div>
            <div class="flex-1 space-y-1">
              <Label class="text-xs text-muted-foreground">Rôle</Label>
              <select
                v-model="newRole"
                class="dark:bg-input/30 border-input focus-visible:border-ring focus-visible:ring-ring/50 h-8 w-full rounded-lg border bg-transparent px-2 text-sm outline-none focus-visible:ring-3"
              >
                <option v-for="r in roles" :key="r" :value="r">{{ r }}</option>
              </select>
            </div>
          </div>
          <p class="pt-2 text-xs text-muted-foreground">
            Aucun mot de passe : une clé d'API est générée à la création et affichée une seule fois.
          </p>
        </TabsContent>
      </Tabs>

      <div class="flex justify-end gap-2">
        <Button
          :disabled="!newUsername.trim() || (newKind === 'human' && !newPassword) || adding"
          @click="add"
        >
          {{ adding ? "Création…" : "Créer" }}
        </Button>
      </div>
    </div>

    <Dialog v-model:open="revealOpen">
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Clé d'API pour « {{ revealedKey?.username }} »</DialogTitle>
          <DialogDescription>
            Copiez-la maintenant : elle ne sera plus jamais affichée. En cas de perte, régénérez une
            nouvelle clé depuis cette page (l'ancienne sera immédiatement invalide).
          </DialogDescription>
        </DialogHeader>
        <Input :model-value="revealedKey?.apiKey" readonly class="font-mono text-xs" @focus="($event.target as HTMLInputElement).select()" />
        <DialogFooter>
          <Button variant="secondary" @click="revealOpen = false">Fermer</Button>
          <Button @click="copyKey">Copier</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  </div>
</template>
