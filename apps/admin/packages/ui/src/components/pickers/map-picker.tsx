import {
  Suspense,
  lazy,
  useCallback,
  useSyncExternalStore,
  useState,
} from "react"
import { MapPin } from "lucide-react"

import { cn } from "@workspace/ui/lib/utils"
import {
  GoogleMapPicker,
  type MapLocation,
} from "@workspace/ui/components/pickers/google-map-picker"

export type MapProvider = "osm" | "mapbox" | "google"

// Leaflet is client-only; lazy-load it (its module accesses window at the top level)
const LeafletMap = lazy(
  () => import("@workspace/ui/components/pickers/_leaflet-map")
)

export interface MapPickerProps {
  /** Map base layer: osm (default, zero-config) / mapbox (needs token) / google (needs key) */
  provider?: MapProvider
  value?: MapLocation | null
  onChange?: (location: MapLocation) => void
  readonly?: boolean
  height?: number | string
  searchPlaceholder?: string
  searchLabel?: string
  /** Hint shown when the token/key is missing (i18n string passed in by the caller) */
  missingKeyHint?: string
  /** Auto-locate to the user's current position when no point is picked (osm/mapbox only), defaults to false */
  geolocate?: boolean
  /** mapbox token; defaults to reading VITE_MAPBOX_TOKEN */
  mapboxToken?: string
  /** google key; defaults to reading VITE_GOOGLE_MAPS_API_KEY */
  googleApiKey?: string
  className?: string
}

function MissingKey({
  hint,
  envName,
  height,
}: {
  hint?: string
  envName: string
  height: string
}) {
  return (
    <div
      style={{ height }}
      className="flex flex-col items-center justify-center gap-2 rounded-xl border border-dashed bg-muted/30 p-6 text-center"
    >
      <MapPin className="size-8 text-muted-foreground/50" />
      <p className="max-w-sm text-sm text-muted-foreground">{hint}</p>
      <code className="rounded bg-muted px-2 py-1 text-xs">{envName}</code>
    </div>
  )
}

function LocationCard({ loc }: { loc: MapLocation }) {
  return (
    <div className="flex items-start gap-2 rounded-lg border bg-muted/30 px-3 py-2 text-sm">
      <MapPin className="mt-0.5 size-4 shrink-0 text-primary" />
      <div className="min-w-0">
        {loc.name && <div className="font-medium">{loc.name}</div>}
        {loc.address && (
          <div className="text-muted-foreground">{loc.address}</div>
        )}
        <div className="font-mono text-xs text-muted-foreground">
          {loc.latitude.toFixed(6)}, {loc.longitude.toFixed(6)}
        </div>
      </div>
    </div>
  )
}

export function MapPicker({
  provider = "osm",
  value,
  onChange,
  readonly = false,
  height = 360,
  searchPlaceholder,
  searchLabel,
  missingKeyHint,
  geolocate = false,
  mapboxToken,
  googleApiKey,
  className,
}: MapPickerProps) {
  const [selected, setSelected] = useState<MapLocation | null>(value ?? null)
  const current = value !== undefined ? value : selected

  // Leaflet touches browser APIs, so defer loading until hydration.
  const mounted = useSyncExternalStore(
    () => () => undefined,
    () => true,
    () => false
  )

  const handleChange = useCallback(
    (loc: MapLocation) => {
      setSelected(loc)
      onChange?.(loc)
    },
    [onChange]
  )

  const heightCss = typeof height === "number" ? `${height}px` : height

  // Google uses its dedicated component (with its own placeholder/card)
  if (provider === "google") {
    return (
      <GoogleMapPicker
        value={current}
        onChange={handleChange}
        readonly={readonly}
        height={height}
        searchPlaceholder={searchPlaceholder}
        searchLabel={searchLabel}
        missingKeyHint={missingKeyHint}
        apiKey={googleApiKey}
        className={className}
      />
    )
  }

  // Mapbox requires a token
  const token = mapboxToken ?? import.meta.env.VITE_MAPBOX_TOKEN

  if (provider === "mapbox" && !token) {
    return (
      <div className={className}>
        <MissingKey
          hint={missingKeyHint}
          envName="VITE_MAPBOX_TOKEN"
          height={heightCss}
        />
      </div>
    )
  }

  const tile =
    provider === "mapbox"
      ? {
          url: `https://api.mapbox.com/styles/v1/mapbox/streets-v12/tiles/256/{z}/{x}/{y}@2x?access_token=${token}`,
          attribution:
            '&copy; <a href="https://www.mapbox.com/">Mapbox</a> &copy; OpenStreetMap',
        }
      : {
          url: "https://{s}.tile.openstreetmap.org/{z}/{x}/{y}.png",
          attribution: "&copy; OpenStreetMap contributors",
        }

  return (
    <div className={cn("space-y-2", className)}>
      <div
        style={{ height: heightCss }}
        className="relative isolate overflow-hidden rounded-xl border"
      >
        {mounted ? (
          <Suspense
            fallback={<div className="size-full animate-pulse bg-muted" />}
          >
            <LeafletMap
              value={current}
              onChange={handleChange}
              readonly={readonly}
              tileUrl={tile.url}
              attribution={tile.attribution}
              searchPlaceholder={searchPlaceholder}
              searchLabel={searchLabel}
              geolocate={geolocate}
            />
          </Suspense>
        ) : (
          <div className="size-full animate-pulse bg-muted" />
        )}
      </div>
      {current && <LocationCard loc={current} />}
    </div>
  )
}
