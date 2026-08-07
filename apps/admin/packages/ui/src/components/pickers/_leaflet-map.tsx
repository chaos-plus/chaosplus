import { useEffect, useMemo, useRef, useState } from "react"
import {
  MapContainer,
  Marker,
  TileLayer,
  ZoomControl,
  useMap,
  useMapEvents,
} from "react-leaflet"
import L from "leaflet"
import "leaflet/dist/leaflet.css"
import { Search } from "lucide-react"

import { cn } from "@workspace/ui/lib/utils"
import type { MapLocation } from "@workspace/ui/components/pickers/google-map-picker"

// Use divIcon + inline SVG to avoid Leaflet's default marker images 404-ing after bundling
const PIN_ICON = L.divIcon({
  className: "",
  html: `<svg width="28" height="28" viewBox="0 0 24 24" fill="none" xmlns="http://www.w3.org/2000/svg">
    <path d="M12 2C8.13 2 5 5.13 5 9c0 5.25 7 13 7 13s7-7.75 7-13c0-3.87-3.13-7-7-7z" fill="#ef4444" stroke="#fff" stroke-width="1.5"/>
    <circle cx="12" cy="9" r="2.5" fill="#fff"/>
  </svg>`,
  iconSize: [28, 28],
  iconAnchor: [14, 28],
})

const DEFAULT_CENTER: [number, number] = [39.9042, 116.4074] // Beijing

interface NominatimResult {
  lat: string
  lon: string
  display_name: string
  name?: string
}

async function reverseGeocode(
  lat: number,
  lng: number
): Promise<string | undefined> {
  try {
    const res = await fetch(
      `https://nominatim.openstreetmap.org/reverse?format=json&lat=${lat}&lon=${lng}`,
      { headers: { Accept: "application/json" } }
    )
    const json = (await res.json()) as { display_name?: string }
    return json.display_name
  } catch {
    return undefined
  }
}

// Map click → pick a point
function ClickHandler({
  readonly,
  onPick,
}: {
  readonly: boolean
  onPick: (lat: number, lng: number) => void
}) {
  useMapEvents({
    click(e) {
      if (!readonly) onPick(e.latlng.lat, e.latlng.lng)
    },
  })
  return null
}

// When no point is picked, use the browser geolocation API to center on the user's current location (first time only, fails silently)
function Locate({ enabled }: { enabled: boolean }) {
  const map = useMap()
  const done = useRef(false)
  useEffect(() => {
    if (done.current || !enabled) return
    if (typeof navigator === "undefined" || !navigator.geolocation) return
    done.current = true
    navigator.geolocation.getCurrentPosition(
      (pos) => map.setView([pos.coords.latitude, pos.coords.longitude], 14),
      () => {},
      { enableHighAccuracy: false, timeout: 8000, maximumAge: 600000 }
    )
  }, [enabled, map])
  return null
}

// Only recenter the view when the value change comes from outside the map (search / external set).
// Don't move the view when the user clicks or drags to pick a point on the map (forcing zoom/pan would be a poor experience).
function Recenter({
  lat,
  lng,
  skipRef,
}: {
  lat: number | null
  lng: number | null
  skipRef: React.RefObject<boolean>
}) {
  const map = useMap()
  useEffect(() => {
    if (lat == null || lng == null) return
    if (skipRef.current) {
      skipRef.current = false
      return
    }
    map.setView([lat, lng], Math.max(map.getZoom(), 14))
  }, [lat, lng, map, skipRef])
  return null
}

export interface LeafletMapProps {
  value?: MapLocation | null
  onChange?: (loc: MapLocation) => void
  readonly?: boolean
  tileUrl: string
  attribution: string
  searchPlaceholder?: string
  searchLabel?: string
  geolocate?: boolean
}

export default function LeafletMap({
  value,
  onChange,
  readonly = false,
  tileUrl,
  attribution,
  searchPlaceholder,
  searchLabel,
  geolocate = true,
}: LeafletMapProps) {
  const resolvedSearchLabel =
    searchLabel ?? searchPlaceholder ?? "Location search"
  const [query, setQuery] = useState("")
  const [results, setResults] = useState<NominatimResult[]>([])
  const [open, setOpen] = useState(false)
  const debounceRef = useRef<ReturnType<typeof setTimeout> | null>(null)
  // true = this value change came from a map click/drag, so Recenter should skip it
  const skipRecenterRef = useRef(false)

  const marker = useMemo(
    () =>
      value && Number.isFinite(value.latitude)
        ? ([value.latitude, value.longitude] as [number, number])
        : null,
    [value]
  )

  function emit(lat: number, lng: number, name?: string) {
    // A point picked via map interaction should not auto-move/zoom the view
    skipRecenterRef.current = true
    const base: MapLocation = { latitude: lat, longitude: lng, name }
    onChange?.(base)
    void reverseGeocode(lat, lng).then((address) =>
      onChange?.({ ...base, address })
    )
  }

  // Search (Nominatim, free and key-less, debounced)
  useEffect(() => {
    const trimmedQuery = query.trim()
    if (trimmedQuery.length < 3) {
      return
    }
    if (debounceRef.current) clearTimeout(debounceRef.current)
    const controller = new AbortController()
    debounceRef.current = setTimeout(() => {
      void fetch(
        `https://nominatim.openstreetmap.org/search?format=json&limit=5&q=${encodeURIComponent(trimmedQuery)}`,
        { headers: { Accept: "application/json" }, signal: controller.signal }
      )
        .then((r) => r.json())
        .then((list: NominatimResult[]) => {
          setResults(Array.isArray(list) ? list : [])
          setOpen(true)
        })
        .catch((error) => {
          if ((error as { name?: string }).name !== "AbortError") {
            setResults([])
          }
        })
    }, 400)
    return () => {
      if (debounceRef.current) clearTimeout(debounceRef.current)
      controller.abort()
    }
  }, [query])

  function handleQueryChange(nextQuery: string) {
    setQuery(nextQuery)
    if (!nextQuery.trim()) {
      setResults([])
      setOpen(false)
    }
  }

  function pickResult(r: NominatimResult) {
    const lat = parseFloat(r.lat)
    const lng = parseFloat(r.lon)
    // Search result pick: the view does need to move there
    skipRecenterRef.current = false
    onChange?.({
      latitude: lat,
      longitude: lng,
      address: r.display_name,
      name: r.display_name.split(",")[0],
    })
    setQuery(r.display_name.split(",")[0] ?? "")
    setOpen(false)
  }

  return (
    <div className="relative size-full">
      {!readonly && (
        <div className="absolute top-2 left-2 z-[500] w-[min(320px,calc(100%-1rem))]">
          <div className="flex items-center gap-2 rounded-lg border border-input bg-background/95 px-3 py-2 shadow-md backdrop-blur">
            <Search className="size-4 shrink-0 text-muted-foreground" />
            <input
              value={query}
              onChange={(e) => handleQueryChange(e.target.value)}
              onFocus={() => results.length && setOpen(true)}
              placeholder={searchPlaceholder}
              aria-label={resolvedSearchLabel}
              aria-expanded={open}
              aria-controls="leaflet-map-search-results"
              className="w-full bg-transparent text-sm outline-none placeholder:text-muted-foreground"
            />
          </div>
          {open && results.length > 0 && (
            <ul
              id="leaflet-map-search-results"
              aria-label={resolvedSearchLabel}
              className="mt-1 max-h-56 overflow-auto rounded-lg border bg-popover py-1 shadow-lg"
            >
              {results.map((r, i) => (
                <li key={`${r.lat}-${r.lon}-${i}`}>
                  <button
                    type="button"
                    onClick={() => pickResult(r)}
                    className="block w-full px-3 py-2 text-left text-sm hover:bg-accent hover:text-accent-foreground"
                  >
                    {r.display_name}
                  </button>
                </li>
              ))}
            </ul>
          )}
        </div>
      )}

      <MapContainer
        center={marker ?? DEFAULT_CENTER}
        zoom={marker ? 14 : 11}
        scrollWheelZoom={!readonly}
        dragging={!readonly}
        doubleClickZoom={!readonly}
        zoomControl={false}
        className={cn("size-full", readonly && "pointer-events-none")}
      >
        {!readonly && <ZoomControl position="topright" />}
        <TileLayer url={tileUrl} attribution={attribution} />
        <ClickHandler readonly={readonly} onPick={emit} />
        <Locate enabled={geolocate && !readonly && !marker} />
        <Recenter
          lat={marker?.[0] ?? null}
          lng={marker?.[1] ?? null}
          skipRef={skipRecenterRef}
        />
        {marker && (
          <Marker
            position={marker}
            icon={PIN_ICON}
            draggable={!readonly}
            eventHandlers={{
              dragend(e) {
                const ll = (e.target as L.Marker).getLatLng()
                emit(ll.lat, ll.lng)
              },
            }}
          />
        )}
      </MapContainer>
    </div>
  )
}
