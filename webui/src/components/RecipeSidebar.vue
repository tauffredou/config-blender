<script setup lang="ts">
import { Button } from "@/components/ui/button"

defineProps<{
  recipes: string[]
  current: string | null
  sourcesActive: boolean
}>()

const emit = defineEmits<{
  select: [name: string]
  create: []
  openSources: []
}>()
</script>

<template>
  <aside class="w-60 shrink-0 border-r bg-background p-3">
    <Button class="w-full mb-3" @click="emit('create')">+ Nouvelle Recipe</Button>
    <ul class="space-y-0.5">
      <li
        v-for="name in recipes"
        :key="name"
        class="cursor-pointer rounded-md px-2.5 py-1.5 text-sm hover:bg-muted"
        :class="{ 'bg-muted font-medium': name === current && !sourcesActive }"
        @click="emit('select', name)"
      >
        {{ name }}
      </li>
      <li v-if="recipes.length === 0" class="px-2.5 py-1.5 text-sm text-muted-foreground">
        Aucune Recipe pour l'instant.
      </li>
    </ul>

    <div class="my-3 border-t" />

    <button
      type="button"
      class="w-full cursor-pointer rounded-md px-2.5 py-1.5 text-left text-sm hover:bg-muted"
      :class="{ 'bg-muted font-medium': sourcesActive }"
      @click="emit('openSources')"
    >
      ⚙ Sources Git
    </button>
  </aside>
</template>
