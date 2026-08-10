export type DemoCategory =
  | "automation"
  | "forms"
  | "data"
  | "feedback"
  | "overlay"
  | "layout"
  | "media"
  | "navigation"

export interface DemoMeta {
  id: string
  category: DemoCategory
}

export const demoConfig: DemoMeta[] = [
  // Forms and inputs
  { id: "button", category: "forms" },
  { id: "label", category: "forms" },
  { id: "input", category: "forms" },
  { id: "textarea", category: "forms" },
  { id: "select", category: "forms" },
  { id: "checkbox", category: "forms" },
  { id: "radioGroup", category: "forms" },
  { id: "switch", category: "forms" },
  { id: "slider", category: "forms" },
  { id: "toggle", category: "forms" },
  { id: "datePicker", category: "forms" },
  { id: "timePicker", category: "forms" },
  { id: "colorPicker", category: "forms" },
  { id: "fileUpload", category: "forms" },
  { id: "mapPicker", category: "forms" },

  // Data display
  { id: "badge", category: "data" },
  { id: "avatar", category: "data" },
  { id: "table", category: "data" },
  { id: "pagination", category: "data" },
  { id: "progress", category: "data" },
  { id: "skeleton", category: "data" },

  // Layout
  { id: "card", category: "layout" },
  { id: "separator", category: "layout" },
  { id: "tabs", category: "layout" },
  { id: "accordion", category: "layout" },
  { id: "collapsible", category: "layout" },
  { id: "scrollArea", category: "layout" },

  // Navigation and preferences
  { id: "breadcrumb", category: "navigation" },
  { id: "topNav", category: "navigation" },
  { id: "sidebar", category: "navigation" },
  { id: "languageSwitcher", category: "navigation" },
  { id: "themeSwitcher", category: "navigation" },
  { id: "themeGallery", category: "navigation" },

  // Feedback
  { id: "alert", category: "feedback" },
  { id: "toast", category: "feedback" },
  { id: "dialog", category: "feedback" },

  // Overlays
  { id: "dropdownMenu", category: "overlay" },
  { id: "popover", category: "overlay" },
  { id: "tooltip", category: "overlay" },
  { id: "sheet", category: "overlay" },

  // Media and content
  { id: "image", category: "media" },
  { id: "video", category: "media" },
  { id: "richText", category: "media" },
  { id: "code", category: "media" },

  // Configuration-driven components
  { id: "autoForm", category: "automation" },
  { id: "autoTable", category: "automation" },
]

export const defaultDemoId = demoConfig[0]?.id ?? "button"
