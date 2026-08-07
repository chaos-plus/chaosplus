import { Suspense, lazy, useEffect, useState } from "react"
import { useTranslations } from "use-intl"

import { cn } from "@workspace/ui/lib/utils"

import { useApi } from "../api-provider"
import { Input } from "../input"
import type { MapLocation } from "@workspace/ui/components/pickers/google-map-picker"
import { SimpleSelect } from "../select"
import type { FieldRendererProps } from "./types"

// MapPicker (and its own lazy leaflet chunk) is only needed when a form actually
// renders a `location` field. Statically importing it here would pull the whole
// pickers/demos module graph into every CrudForm bundle (auto-form.tsx registers
// this renderer eagerly for all forms). Deferred the same way map-picker.tsx
// defers leaflet itself.
const MapPicker = lazy(() =>
  import("@workspace/ui/components/pickers/map-picker").then((m) => ({
    default: m.MapPicker,
  }))
)

interface LocationValue {
  latitude?: number | string
  longitude?: number | string
  regionId?: number | string
  address?: string
}

interface RegionOption {
  id: number
  name: string
  parentId: number
}

function toLocation(value: unknown): LocationValue {
  if (value && typeof value === "object") return value as LocationValue
  return {}
}

/**
 * LocationField (= the legacy LOCATION CommonFormLocation): a 3-level region cascade
 * (province → city → county) + a map picker (lat/lng/address) + an address input. Emits
 * { latitude, longitude, regionId, address }.
 */
export function LocationFieldRenderer(props: FieldRendererProps) {
  const { value, onChange, disabled, error, errorId, labelId } = props
  const t = useTranslations("form")
  const api = useApi()
  const loc = toLocation(value)

  const [provinces, setProvinces] = useState<RegionOption[]>([])
  const [cities, setCities] = useState<RegionOption[]>([])
  const [counties, setCounties] = useState<RegionOption[]>([])
  const [sel, setSel] = useState<{
    province: string
    city: string
    county: string
  }>({
    province: "",
    city: "",
    county: "",
  })

  const loadChildren = async (parentId: number): Promise<RegionOption[]> => {
    try {
      const res = await api.list<RegionOption>("/regions", {
        limit: 999,
        filter: { parentId },
      })
      return res.list.map((r) => ({
        id: Number(r.id),
        name: String(r.name),
        parentId: Number(r.parentId),
      }))
    } catch {
      return []
    }
  }

  // load provinces once
  useEffect(() => {
    void loadChildren(0).then(setProvinces)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  // resolve the saved regionId up its ancestor chain on first load (edit/detail)
  useEffect(() => {
    const rid = Number(loc.regionId)
    if (!rid) return
    let cancelled = false
    ;(async () => {
      try {
        const county = (await api.get<RegionOption>(`/regions/${rid}`)).data
        if (!county) return
        const city = county.parentId
          ? (await api.get<RegionOption>(`/regions/${county.parentId}`)).data
          : undefined
        const province = city?.parentId
          ? (await api.get<RegionOption>(`/regions/${city.parentId}`)).data
          : undefined
        if (cancelled) return
        if (province) setCities(await loadChildren(Number(province.id)))
        if (city) setCounties(await loadChildren(Number(city.id)))
        setSel({
          province: province ? String(province.id) : "",
          city: city ? String(city.id) : "",
          county: String(county.id),
        })
      } catch {
        /* ignore */
      }
    })()
    return () => {
      cancelled = true
    }
    // run once on mount
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  const emit = (patch: Partial<LocationValue>) => onChange({ ...loc, ...patch })

  const onProvince = async (v: string) => {
    setSel({ province: v, city: "", county: "" })
    setCities(await loadChildren(Number(v)))
    setCounties([])
    emit({ regionId: Number(v) || 0 })
  }
  const onCity = async (v: string) => {
    setSel((s) => ({ ...s, city: v, county: "" }))
    setCounties(await loadChildren(Number(v)))
    emit({ regionId: Number(v) || 0 })
  }
  const onCounty = (v: string) => {
    setSel((s) => ({ ...s, county: v }))
    emit({ regionId: Number(v) || 0 })
  }

  const mapValue: MapLocation | null =
    loc.latitude != null &&
    loc.longitude != null &&
    loc.latitude !== "" &&
    loc.longitude !== ""
      ? {
          latitude: Number(loc.latitude),
          longitude: Number(loc.longitude),
          address: loc.address ?? "",
        }
      : null

  const regionSelect = (
    placeholder: string,
    options: RegionOption[],
    val: string,
    onSel: (v: string) => void
  ) => (
    <SimpleSelect
      options={options.map((o) => ({ value: String(o.id), label: o.name }))}
      value={val}
      onValueChange={onSel}
      disabled={disabled}
      placeholder={placeholder}
      className="h-8 flex-1"
    />
  )

  return (
    <div
      className={cn(
        "w-full space-y-2",
        error && "rounded-md ring-1 ring-destructive"
      )}
      aria-labelledby={labelId}
    >
      <div className="flex gap-2">
        {regionSelect(t("province"), provinces, sel.province, onProvince)}
        {regionSelect(t("city"), cities, sel.city, onCity)}
        {regionSelect(t("county"), counties, sel.county, onCounty)}
      </div>
      <Input
        placeholder={t("address")}
        value={loc.address ?? ""}
        onChange={(e) => emit({ address: e.target.value })}
        disabled={disabled}
      />
      <div className="flex gap-2">
        <Input
          placeholder={t("latitude")}
          value={loc.latitude == null ? "" : String(loc.latitude)}
          onChange={(e) => emit({ latitude: e.target.value })}
          disabled={disabled}
        />
        <Input
          placeholder={t("longitude")}
          value={loc.longitude == null ? "" : String(loc.longitude)}
          onChange={(e) => emit({ longitude: e.target.value })}
          disabled={disabled}
        />
      </div>
      {!disabled ? (
        <Suspense
          fallback={
            <div className="h-[360px] w-full animate-pulse rounded-xl border bg-muted/30" />
          }
        >
          <MapPicker
            value={mapValue}
            onChange={(m) =>
              emit({
                latitude: m.latitude,
                longitude: m.longitude,
                address: m.address ?? loc.address,
              })
            }
          />
        </Suspense>
      ) : null}
      {error ? (
        <p id={errorId} className="mt-1 text-xs text-destructive">
          {error}
        </p>
      ) : null}
    </div>
  )
}
