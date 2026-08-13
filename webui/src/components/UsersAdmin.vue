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
import type { Role, User } from "@/lib/types"
import { useAuth } from "@/composables/useAuth"

const { username: currentUsername } = useAuth()
const users = ref<User[]>([])
const loading = ref(false)

const roles: Role[] = ["admin", "source-manager", "contributor"]

const newUsername = ref("")
const newPassword = ref("")
const newRole = ref<Role>("contributor")
const adding = ref(false)

// Per-row role-select in-flight state, keyed by username, so one row's
// "saving" state never visually blocks the others.
const updatingRole = ref<Record<string, boolean>>({})
const deleting = ref<Record<string, boolean>>({})

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
    toast.success(`Utilisateur « ${user.username} » supprimé.`)
    await load()
  } catch (e) {
    toast.error("Échec de la suppression", { description: (e as Error).message })
  } finally {
    deleting.value[user.username] = false
  }
}

async function add() {
  const username = newUsername.value.trim()
  const password = newPassword.value
  if (!username || !password) return

  adding.value = true
  try {
    await api.createUser(username, password, newRole.value)
    toast.success(`Utilisateur « ${username} » créé.`)
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
        Comptes nommés avec un rôle (admin, source-manager, contributor). Réservé aux administrateurs.
        Le token d'écriture partagé reste disponible comme accès de secours ("token"), en dehors de
        cette liste.
      </p>
    </div>

    <Table>
      <TableHeader>
        <TableRow>
          <TableHead>Utilisateur</TableHead>
          <TableHead>Rôle</TableHead>
          <TableHead>Créé le</TableHead>
          <TableHead class="text-right">Actions</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        <TableRow v-for="user in users" :key="user.username">
          <TableCell class="font-mono text-xs">{{ user.username }}</TableCell>
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
          <TableCell class="text-right">
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
          <TableCell colspan="4" class="text-sm text-muted-foreground">Aucun utilisateur enregistré.</TableCell>
        </TableRow>
      </TableBody>
    </Table>

    <div class="space-y-3 rounded-md border p-3">
      <div class="flex items-end gap-2">
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

      <div class="flex justify-end gap-2">
        <Button :disabled="!newUsername.trim() || !newPassword || adding" @click="add">
          {{ adding ? "Création…" : "Créer" }}
        </Button>
      </div>
    </div>
  </div>
</template>
