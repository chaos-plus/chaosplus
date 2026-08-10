import { useState } from "react"
import { useTranslations } from "use-intl"

import {
  MapPicker,
  type MapProvider,
} from "@workspace/ui/components/pickers/map-picker"
import type { MapLocation } from "@workspace/ui/components/pickers/google-map-picker"
import { cn } from "@workspace/ui/lib/utils"

const PROVIDERS: MapProvider[] = ["osm", "mapbox", "google"]

function LocalSection({
  title,
  children,
}: {
  title: string
  children: React.ReactNode
}) {
  return (
    <div className="space-y-3">
      <h4 className="text-sm font-semibold text-muted-foreground">{title}</h4>
      {children}
    </div>
  )
}

export function MapPickerDemo() {
  const t = useTranslations("showcase.demos.mapPicker")
  const [provider, setProvider] = useState<MapProvider>("osm")
  const [location, setLocation] = useState<MapLocation | null>(null)

  const providerLabel: Record<MapProvider, string> = {
    osm: t("osm"),
    mapbox: "Mapbox",
    google: "Google",
  }

  return (
    <div className="space-y-8">
      <LocalSection title={t("searchAndPick")}>
        <p className="text-sm text-muted-foreground">{t("description")}</p>

        {/* Base-layer switcher */}
        <div className="inline-flex rounded-lg border bg-muted/40 p-1">
          {PROVIDERS.map((p) => (
            <button
              key={p}
              type="button"
              onClick={() => setProvider(p)}
              aria-pressed={provider === p}
              className={cn(
                "rounded-md px-3 py-1.5 text-sm font-medium transition-colors",
                provider === p
                  ? "bg-background text-foreground shadow-sm"
                  : "text-muted-foreground hover:text-foreground"
              )}
            >
              {providerLabel[p]}
            </button>
          ))}
        </div>

        <p className="text-xs text-muted-foreground">
          {provider === "osm" ? t("osmHint") : t("keyHint")}
        </p>

        <MapPicker
          key={provider}
          provider={provider}
          value={location}
          onChange={setLocation}
          searchPlaceholder={t("searchPlaceholder")}
          missingKeyHint={t("missingKey")}
          height={360}
        />
      </LocalSection>
    </div>
  )
}
