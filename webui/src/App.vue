<script setup lang="ts">
import { ref, computed } from "vue"
import { toast } from "vue-sonner"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Button } from "@/components/ui/button"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { Toaster } from "@/components/ui/sonner"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import { api } from "@/lib/api"
import type { RecipeSpec } from "@/lib/types"
import { useAuth } from "@/composables/useAuth"
import RecipeSidebar from "@/components/RecipeSidebar.vue"
import RecipeEditor from "@/components/RecipeEditor.vue"
import RecipeHistory from "@/components/RecipeHistory.vue"
import RecipeResolve from "@/components/RecipeResolve.vue"
import SourcesAdmin from "@/components/SourcesAdmin.vue"
import UsersAdmin from "@/components/UsersAdmin.vue"

const { authenticated, checked, username, role, hasRole, login, logout } = useAuth()
const loginUsername = ref("")
const loginPassword = ref("")
const loggingIn = ref(false)

async function submitLogin() {
  if (!loginUsername.value || !loginPassword.value) return
  loggingIn.value = true
  try {
    await login({ username: loginUsername.value, password: loginPassword.value })
    loginUsername.value = ""
    loginPassword.value = ""
  } catch (e) {
    toast.error("Échec de la connexion", { description: (e as Error).message })
  } finally {
    loggingIn.value = false
  }
}

const recipes = ref<string[]>([])
const currentName = ref<string | null>(null)
const currentSpec = ref<RecipeSpec | null>(null)
const activeTab = ref("edit")
const isNew = ref(false)
const sourcesActive = ref(false)
const usersActive = ref(false)

const newRecipeOpen = ref(false)
const newRecipeName = ref("")

const hasSelection = computed(() => currentName.value !== null && currentSpec.value !== null)

async function loadRecipes() {
  try {
    const data = await api.listRecipes()
    recipes.value = data.names
  } catch (e) {
    toast.error("Erreur de chargement des Recipes", { description: (e as Error).message })
  }
}
loadRecipes()

function openSources() {
  sourcesActive.value = true
  usersActive.value = false
}

function openUsers() {
  usersActive.value = true
  sourcesActive.value = false
}

async function selectRecipe(name: string) {
  sourcesActive.value = false
  usersActive.value = false
  isNew.value = false
  currentName.value = name
  activeTab.value = "edit"
  try {
    currentSpec.value = await api.getRecipe(name)
  } catch (e) {
    toast.error(`Erreur de chargement de « ${name} »`, { description: (e as Error).message })
    currentSpec.value = null
  }
}

function openCreateDialog() {
  newRecipeName.value = ""
  newRecipeOpen.value = true
}

function confirmCreateRecipe() {
  const name = newRecipeName.value.trim()
  if (!name) return
  if (recipes.value.includes(name)) {
    toast.error(`« ${name} » existe déjà`, { description: "Choisissez un autre nom, ou sélectionnez-la dans la liste." })
    return
  }
  isNew.value = true
  sourcesActive.value = false
  usersActive.value = false
  currentName.value = name
  currentSpec.value = { name, layers: [], mergePolicy: [] }
  activeTab.value = "edit"
  newRecipeOpen.value = false
}

async function onSaved() {
  isNew.value = false
  await loadRecipes()
  if (currentName.value) await selectRecipe(currentName.value)
}

async function onRolledBack() {
  if (currentName.value) await selectRecipe(currentName.value)
}
</script>

<template>
  <Toaster position="top-right" />

  <div v-if="!checked" class="flex min-h-screen items-center justify-center text-sm text-muted-foreground">
    Chargement…
  </div>

  <div v-else-if="!authenticated" class="flex min-h-screen items-center justify-center bg-neutral-950 p-6">
    <div class="w-full max-w-sm space-y-5 rounded-lg border border-neutral-800 bg-neutral-900 p-6 text-white">
      <div>
        <h1 class="text-lg font-semibold">configblender</h1>
        <p class="mt-1 text-xs text-neutral-400">Connectez-vous pour continuer.</p>
      </div>

      <div class="space-y-3">
        <div class="space-y-1.5">
          <Label for="login-username" class="text-xs text-neutral-300">Utilisateur</Label>
          <Input
            id="login-username"
            v-model="loginUsername"
            placeholder="nom d'utilisateur"
            class="bg-neutral-800 text-white border-neutral-700"
            @keyup.enter="submitLogin"
          />
        </div>
        <div class="space-y-1.5">
          <Label for="login-password" class="text-xs text-neutral-300">Mot de passe</Label>
          <Input
            id="login-password"
            v-model="loginPassword"
            type="password"
            placeholder="mot de passe"
            class="bg-neutral-800 text-white border-neutral-700"
            @keyup.enter="submitLogin"
          />
        </div>
        <Button class="w-full" :disabled="!loginUsername || !loginPassword || loggingIn" @click="submitLogin">
          {{ loggingIn ? "Connexion…" : "Se connecter" }}
        </Button>
      </div>
    </div>
  </div>

  <div v-else class="flex min-h-screen flex-col">
    <header class="flex items-center justify-between border-b bg-neutral-900 px-5 py-3 text-white">
      <h1 class="text-base font-semibold">configblender</h1>
      <div class="flex items-center gap-2">
        <span class="text-xs text-neutral-300">{{ username }} ({{ role }})</span>
        <Button variant="outline" size="sm" class="h-7 border-neutral-700 bg-neutral-800 text-white hover:bg-neutral-700" @click="logout">
          Déconnexion
        </Button>
      </div>
    </header>

    <div class="flex flex-1">
      <RecipeSidebar
        :recipes="recipes"
        :current="currentName"
        :sources-active="sourcesActive"
        :users-active="usersActive"
        :can-manage-users="hasRole('admin')"
        @select="selectRecipe"
        @create="openCreateDialog"
        @open-sources="openSources"
        @open-users="openUsers"
      />

      <main class="flex-1 p-6">
        <SourcesAdmin v-if="sourcesActive" />
        <UsersAdmin v-else-if="usersActive" />

        <template v-else>
          <p v-if="!hasSelection" class="text-sm text-muted-foreground">
            Sélectionnez une Recipe à gauche, ou créez-en une nouvelle.
          </p>

          <div v-else class="space-y-4">
            <h2 class="text-lg font-semibold">
              {{ currentName }}
              <span v-if="isNew" class="ml-2 text-xs font-normal text-muted-foreground">(non enregistrée)</span>
            </h2>

            <Tabs v-model="activeTab">
              <TabsList>
                <TabsTrigger value="edit">Éditer</TabsTrigger>
                <TabsTrigger value="history" :disabled="isNew">Historique</TabsTrigger>
                <TabsTrigger value="resolve" :disabled="isNew">Resolve / Explain</TabsTrigger>
              </TabsList>

              <TabsContent value="edit">
                <RecipeEditor :name="currentName!" :spec="currentSpec!" @saved="onSaved" />
              </TabsContent>

              <TabsContent value="history">
                <RecipeHistory :name="currentName!" :latest-spec="currentSpec!" @rolled-back="onRolledBack" />
              </TabsContent>

              <TabsContent value="resolve">
                <RecipeResolve :name="currentName!" :layers="currentSpec!.layers.map((l) => l.name)" />
              </TabsContent>
            </Tabs>
          </div>
        </template>
      </main>
    </div>

    <Dialog v-model:open="newRecipeOpen">
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Nouvelle Recipe</DialogTitle>
          <DialogDescription>
            Choisissez un nom. Elle n'est créée qu'après le premier enregistrement.
          </DialogDescription>
        </DialogHeader>
        <div class="space-y-2">
          <Label for="new-recipe-name">Nom</Label>
          <Input
            id="new-recipe-name"
            v-model="newRecipeName"
            placeholder="my-app-recipe"
            @keyup.enter="confirmCreateRecipe"
          />
        </div>
        <DialogFooter>
          <Button variant="secondary" @click="newRecipeOpen = false">Annuler</Button>
          <Button @click="confirmCreateRecipe">Créer</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  </div>
</template>
