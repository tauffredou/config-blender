// Categorical badge colors for the Explain tree's layer tags. Fixed hue
// order (blue, orange, teal, amber, pink, green, violet, red) assigned by
// each layer's position in the Recipe's own layer list, never re-sorted or
// hashed, so a given layer keeps the same color across re-resolves and the
// order stays meaningful (earlier layer = earlier hue). Caps at 8 distinct
// hues; anything beyond falls back to a neutral badge rather than repeating
// a hue ambiguously.
const HUES = ["blue", "orange", "teal", "amber", "pink", "green", "violet", "red"] as const

const BADGE_CLASSES: Record<(typeof HUES)[number], string> = {
  blue: "bg-blue-100 text-blue-700 dark:bg-blue-950 dark:text-blue-400",
  orange: "bg-orange-100 text-orange-700 dark:bg-orange-950 dark:text-orange-400",
  teal: "bg-teal-100 text-teal-700 dark:bg-teal-950 dark:text-teal-400",
  amber: "bg-amber-100 text-amber-700 dark:bg-amber-950 dark:text-amber-400",
  pink: "bg-pink-100 text-pink-700 dark:bg-pink-950 dark:text-pink-400",
  green: "bg-green-100 text-green-700 dark:bg-green-950 dark:text-green-400",
  violet: "bg-violet-100 text-violet-700 dark:bg-violet-950 dark:text-violet-400",
  red: "bg-red-100 text-red-700 dark:bg-red-950 dark:text-red-400",
}

export const FALLBACK_BADGE_CLASS = "bg-muted text-muted-foreground"

export function layerColorMap(layerOrder: string[]): Map<string, string> {
  const map = new Map<string, string>()
  for (const name of layerOrder) {
    if (map.has(name)) continue
    map.set(name, BADGE_CLASSES[HUES[map.size % HUES.length]])
  }
  return map
}
