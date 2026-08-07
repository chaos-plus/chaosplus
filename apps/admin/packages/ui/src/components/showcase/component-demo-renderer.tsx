import {
  Suspense,
  lazy,
  type ComponentType,
  type LazyExoticComponent,
} from "react"

import {
  defaultDemoId,
  demoConfig,
} from "@workspace/ui/components/showcase/component-showcase-config"
import { Skeleton } from "@workspace/ui/components/skeleton"

type DemoComponent = ComponentType

const demoLoaders: Record<string, () => Promise<{ default: DemoComponent }>> = {
  accordion: () =>
    import("@workspace/ui/components/demos/accordion").then((m) => ({
      default: m.AccordionDemo,
    })),
  alert: () =>
    import("@workspace/ui/components/demos/alert").then((m) => ({
      default: m.AlertDemo,
    })),
  autoForm: () =>
    import("@workspace/ui/components/demos/autoForm").then((m) => ({
      default: m.AutoFormDemo,
    })),
  autoTable: () =>
    import("@workspace/ui/components/demos/autoTable").then((m) => ({
      default: m.AutoTableDemo,
    })),
  avatar: () =>
    import("@workspace/ui/components/demos/avatar").then((m) => ({
      default: m.AvatarDemo,
    })),
  badge: () =>
    import("@workspace/ui/components/demos/badge").then((m) => ({
      default: m.BadgeDemo,
    })),
  breadcrumb: () =>
    import("@workspace/ui/components/demos/breadcrumb").then((m) => ({
      default: m.BreadcrumbDemo,
    })),
  button: () =>
    import("@workspace/ui/components/demos/button").then((m) => ({
      default: m.ButtonDemo,
    })),
  card: () =>
    import("@workspace/ui/components/demos/card").then((m) => ({
      default: m.CardDemo,
    })),
  checkbox: () =>
    import("@workspace/ui/components/demos/checkbox").then((m) => ({
      default: m.CheckboxDemo,
    })),
  code: () =>
    import("@workspace/ui/components/demos/code").then((m) => ({
      default: m.CodeDemo,
    })),
  collapsible: () =>
    import("@workspace/ui/components/demos/collapsible").then((m) => ({
      default: m.CollapsibleDemo,
    })),
  colorPicker: () =>
    import("@workspace/ui/components/demos/colorPicker").then((m) => ({
      default: m.ColorPickerDemo,
    })),
  datePicker: () =>
    import("@workspace/ui/components/demos/datePicker").then((m) => ({
      default: m.DatePickerDemo,
    })),
  dialog: () =>
    import("@workspace/ui/components/demos/dialog").then((m) => ({
      default: m.DialogDemo,
    })),
  dropdownMenu: () =>
    import("@workspace/ui/components/demos/dropdownMenu").then((m) => ({
      default: m.DropdownMenuDemo,
    })),
  fileUpload: () =>
    import("@workspace/ui/components/demos/fileUpload").then((m) => ({
      default: m.FileUploadDemo,
    })),
  image: () =>
    import("@workspace/ui/components/demos/image").then((m) => ({
      default: m.ImageDemo,
    })),
  input: () =>
    import("@workspace/ui/components/demos/input").then((m) => ({
      default: m.InputDemo,
    })),
  label: () =>
    import("@workspace/ui/components/demos/label").then((m) => ({
      default: m.LabelDemo,
    })),
  languageSwitcher: () =>
    import("@workspace/ui/components/demos/languageSwitcher").then((m) => ({
      default: m.LanguageSwitcherDemo,
    })),
  mapPicker: () =>
    import("@workspace/ui/components/demos/mapPicker").then((m) => ({
      default: m.MapPickerDemo,
    })),
  pagination: () =>
    import("@workspace/ui/components/demos/pagination").then((m) => ({
      default: m.PaginationDemo,
    })),
  popover: () =>
    import("@workspace/ui/components/demos/popover").then((m) => ({
      default: m.PopoverDemo,
    })),
  progress: () =>
    import("@workspace/ui/components/demos/progress").then((m) => ({
      default: m.ProgressDemo,
    })),
  radioGroup: () =>
    import("@workspace/ui/components/demos/radioGroup").then((m) => ({
      default: m.RadioGroupDemo,
    })),
  richText: () =>
    import("@workspace/ui/components/demos/richText").then((m) => ({
      default: m.RichTextDemo,
    })),
  scrollArea: () =>
    import("@workspace/ui/components/demos/scrollArea").then((m) => ({
      default: m.ScrollAreaDemo,
    })),
  select: () =>
    import("@workspace/ui/components/demos/select").then((m) => ({
      default: m.SelectDemo,
    })),
  separator: () =>
    import("@workspace/ui/components/demos/separator").then((m) => ({
      default: m.SeparatorDemo,
    })),
  sheet: () =>
    import("@workspace/ui/components/demos/sheet").then((m) => ({
      default: m.SheetDemo,
    })),
  sidebar: () =>
    import("@workspace/ui/components/demos/sidebar").then((m) => ({
      default: m.SidebarDemo,
    })),
  skeleton: () =>
    import("@workspace/ui/components/demos/skeleton").then((m) => ({
      default: m.SkeletonDemo,
    })),
  slider: () =>
    import("@workspace/ui/components/demos/slider").then((m) => ({
      default: m.SliderDemo,
    })),
  switch: () =>
    import("@workspace/ui/components/demos/switch").then((m) => ({
      default: m.SwitchDemo,
    })),
  table: () =>
    import("@workspace/ui/components/demos/table").then((m) => ({
      default: m.TableDemo,
    })),
  tabs: () =>
    import("@workspace/ui/components/demos/tabs").then((m) => ({
      default: m.TabsDemo,
    })),
  textarea: () =>
    import("@workspace/ui/components/demos/textarea").then((m) => ({
      default: m.TextareaDemo,
    })),
  themeGallery: () =>
    import("@workspace/ui/components/demos/themeGallery").then((m) => ({
      default: m.ThemeGalleryDemo,
    })),
  themeSwitcher: () =>
    import("@workspace/ui/components/demos/themeSwitcher").then((m) => ({
      default: m.ThemeSwitcherDemo,
    })),
  timePicker: () =>
    import("@workspace/ui/components/demos/timePicker").then((m) => ({
      default: m.TimePickerDemo,
    })),
  toggle: () =>
    import("@workspace/ui/components/demos/toggle").then((m) => ({
      default: m.ToggleDemo,
    })),
  tooltip: () =>
    import("@workspace/ui/components/demos/tooltip").then((m) => ({
      default: m.TooltipDemo,
    })),
  topNav: () =>
    import("@workspace/ui/components/demos/topNav").then((m) => ({
      default: m.TopNavDemo,
    })),
  toast: () =>
    import("@workspace/ui/components/demos/toast").then((m) => ({
      default: m.ToastDemo,
    })),
  video: () =>
    import("@workspace/ui/components/demos/video").then((m) => ({
      default: m.VideoDemo,
    })),
}

const lazyDemos = Object.fromEntries(
  demoConfig.map(({ id }) => [
    id,
    lazy(demoLoaders[id] ?? demoLoaders[defaultDemoId]!),
  ])
) as Record<string, LazyExoticComponent<DemoComponent>>

function DemoFallback() {
  return <Skeleton className="h-32 w-full" />
}

export function ComponentDemoRenderer({ id }: { id: string }) {
  const Demo = lazyDemos[id] ?? lazyDemos[defaultDemoId]
  return (
    <Suspense fallback={<DemoFallback />}>{Demo ? <Demo /> : null}</Suspense>
  )
}
