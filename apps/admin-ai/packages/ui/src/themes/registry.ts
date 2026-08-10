export interface Theme {
  id: string
  nameKey: string
  isCustomizable?: boolean
}

export const themes: Theme[] = [
  { id: "modern", nameKey: "modern" },
  { id: "business", nameKey: "business" },
  { id: "eyeCare", nameKey: "eyeCare" },
  { id: "pinkKawaii", nameKey: "pinkKawaii" },
  { id: "neoBrutalism", nameKey: "neoBrutalism" },
  { id: "custom", nameKey: "custom", isCustomizable: true },
]

export const themeIds = themes.map((t) => t.id)
export const defaultThemeId = "modern"

export function getThemeById(id: string): Theme | undefined {
  return themes.find((t) => t.id === id)
}
