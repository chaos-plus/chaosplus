/// <reference types="google.maps" />
import { useCallback, useEffect, useRef, useState } from "react"
import {
  APIProvider,
  Map,
  Marker,
  useMap,
  useMapsLibrary,
} from "@vis.gl/react-google-maps"
import { MapPin, Search } from "lucide-react"

import { cn } from "@workspace/ui/lib/utils"

// ─── types ───────────────────────────────────────────────────────────────────

export interface MapLocation {
  latitude: number
  longitude: number
  /** Address obtained via reverse geocoding */
  address?: string
  /** Name of the selected place (from search results) */
  name?: string
}

export interface GoogleMapPickerProps {
  value?: MapLocation | null
  onChange?: (location: MapLocation) => void
  /** Defaults to reading VITE_GOOGLE_MAPS_API_KEY; can also be passed explicitly */
  apiKey?: string
  readonly?: boolean
  /** Map center when there is no selected value; defaults to Beijing */
  defaultCenter?: { lat: number; lng: number }
  height?: number | string
  /** Search box placeholder (i18n string passed in by the caller) */
  searchPlaceholder?: string
  searchLabel?: string
  /** Placeholder hint shown when no API key is configured (i18n string passed in by the caller) */
  missingKeyHint?: string
  className?: string
}

const DEFAULT_CENTER = { lat: 39.9042, lng: 116.4074 } // Beijing

// ─── search box (Places Autocomplete) ────────────────────────────────────────

function SearchBox({
  placeholder,
  label,
  onPlace,
}: {
  placeholder?: string
  label?: string
  onPlace: (loc: MapLocation) => void
}) {
  const places = useMapsLibrary("places")
  const inputRef = useRef<HTMLInputElement>(null)

  useEffect(() => {
    if (!places || !inputRef.current) return
    const autocomplete = new places.Autocomplete(inputRef.current, {
      fields: ["geometry", "formatted_address", "name"],
    })
    const listener = autocomplete.addListener("place_changed", () => {
      const place = autocomplete.getPlace()
      const loc = place.geometry?.location
      if (!loc) return
      onPlace({
        latitude: loc.lat(),
        longitude: loc.lng(),
        address: place.formatted_address ?? undefined,
        name: place.name ?? undefined,
      })
    })
    return () => listener.remove()
  }, [places, onPlace])

  return (
    <div className="absolute top-2 left-2 z-10 flex w-[min(320px,calc(100%-1rem))] items-center gap-2 rounded-lg border border-input bg-background/95 px-3 py-2 shadow-md backdrop-blur">
      <Search className="size-4 shrink-0 text-muted-foreground" />
      <input
        ref={inputRef}
        type="text"
        placeholder={placeholder}
        aria-label={label ?? placeholder ?? "Location search"}
        className="w-full bg-transparent text-sm outline-none placeholder:text-muted-foreground"
      />
    </div>
  )
}

// ─── inner map (recenters + reverse-geocodes) ────────────────────────────────

function MapInner({
  value,
  onChange,
  readonly,
  searchPlaceholder,
  searchLabel,
}: {
  value?: MapLocation | null
  onChange?: (loc: MapLocation) => void
  readonly?: boolean
  searchPlaceholder?: string
  searchLabel?: string
}) {
  const map = useMap()
  const geocodingLib = useMapsLibrary("geocoding")
  type GeocoderInstance = InstanceType<
    NonNullable<typeof geocodingLib>["Geocoder"]
  >
  const geocoderRef = useRef<GeocoderInstance | null>(null)

  useEffect(() => {
    if (geocodingLib) geocoderRef.current = new geocodingLib.Geocoder()
  }, [geocodingLib])

  // After picking a point, reverse-geocode to fill in the address
  const resolve = useCallback(
    (lat: number, lng: number, name?: string) => {
      const base: MapLocation = { latitude: lat, longitude: lng, name }
      if (!geocoderRef.current) {
        onChange?.(base)
        return
      }
      geocoderRef.current
        .geocode({ location: { lat, lng } })
        .then(({ results }) => {
          onChange?.({ ...base, address: results?.[0]?.formatted_address })
        })
        .catch(() => onChange?.(base))
    },
    [onChange]
  )

  const handlePlace = useCallback(
    (loc: MapLocation) => {
      onChange?.(loc)
      map?.panTo({ lat: loc.latitude, lng: loc.longitude })
      map?.setZoom(16)
    },
    [map, onChange]
  )

  const marker =
    value && Number.isFinite(value.latitude)
      ? { lat: value.latitude, lng: value.longitude }
      : null

  return (
    <>
      {!readonly && (
        <SearchBox
          placeholder={searchPlaceholder}
          label={searchLabel ?? searchPlaceholder}
          onPlace={handlePlace}
        />
      )}
      <Map
        defaultCenter={marker ?? DEFAULT_CENTER}
        defaultZoom={marker ? 16 : 11}
        gestureHandling={readonly ? "none" : "greedy"}
        disableDefaultUI={false}
        clickableIcons={!readonly}
        onClick={(e) => {
          if (readonly) return
          const ll = e.detail.latLng
          if (ll) resolve(ll.lat, ll.lng)
        }}
        className="size-full"
      >
        {marker && (
          <Marker
            position={marker}
            draggable={!readonly}
            onDragEnd={(e) => {
              if (readonly || !e.latLng) return
              resolve(e.latLng.lat(), e.latLng.lng())
            }}
          />
        )}
      </Map>
    </>
  )
}

// ─── placeholder when no API key ─────────────────────────────────────────────

function MissingKey({
  hint,
  height,
}: {
  hint?: string
  height: string | number
}) {
  return (
    <div
      style={{ height }}
      className="flex flex-col items-center justify-center gap-2 rounded-xl border border-dashed bg-muted/30 p-6 text-center"
    >
      <MapPin className="size-8 text-muted-foreground/50" />
      <p className="max-w-sm text-sm text-muted-foreground">
        {hint ?? "Set VITE_GOOGLE_MAPS_API_KEY to enable the map."}
      </p>
      <code className="rounded bg-muted px-2 py-1 text-xs">
        VITE_GOOGLE_MAPS_API_KEY
      </code>
    </div>
  )
}

// ─── main component ──────────────────────────────────────────────────────────

export function GoogleMapPicker({
  value,
  onChange,
  apiKey,
  readonly = false,
  height = 360,
  searchPlaceholder,
  searchLabel,
  missingKeyHint,
  className,
}: GoogleMapPickerProps) {
  const [selected, setSelected] = useState<MapLocation | null>(value ?? null)

  const resolvedKey = apiKey ?? import.meta.env.VITE_GOOGLE_MAPS_API_KEY

  const handleChange = useCallback(
    (loc: MapLocation) => {
      setSelected(loc)
      onChange?.(loc)
    },
    [onChange]
  )

  const heightCss = typeof height === "number" ? `${height}px` : height
  const current = value !== undefined ? value : selected

  if (!resolvedKey) {
    return (
      <div className={className}>
        <MissingKey hint={missingKeyHint} height={heightCss} />
      </div>
    )
  }

  return (
    <div className={cn("space-y-2", className)}>
      <div
        style={{ height: heightCss }}
        className="relative isolate overflow-hidden rounded-xl border"
      >
        <APIProvider apiKey={resolvedKey}>
          <MapInner
            value={current}
            onChange={handleChange}
            readonly={readonly}
            searchPlaceholder={searchPlaceholder}
            searchLabel={searchLabel}
          />
        </APIProvider>
      </div>

      {current && (
        <div className="flex items-start gap-2 rounded-lg border bg-muted/30 px-3 py-2 text-sm">
          <MapPin className="mt-0.5 size-4 shrink-0 text-primary" />
          <div className="min-w-0">
            {current.name && <div className="font-medium">{current.name}</div>}
            {current.address && (
              <div className="text-muted-foreground">{current.address}</div>
            )}
            <div className="font-mono text-xs text-muted-foreground">
              {current.latitude.toFixed(6)}, {current.longitude.toFixed(6)}
            </div>
          </div>
        </div>
      )}
    </div>
  )
}
