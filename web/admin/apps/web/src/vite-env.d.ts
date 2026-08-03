/// <reference types="vite/client" />

interface ImportMetaEnv {
  /** Browser API base path. Defaults to /api. */
  readonly VITE_API_URL?: string
  /** Local backend used by the Vite /api proxy. */
  readonly VITE_API_PROXY_TARGET?: string
  /** Google Maps JS API key for the map picker component (was NEXT_PUBLIC_GOOGLE_MAPS_API_KEY). */
  readonly VITE_GOOGLE_MAPS_API_KEY?: string
  /** Mapbox access token for the map picker component (was NEXT_PUBLIC_MAPBOX_TOKEN). */
  readonly VITE_MAPBOX_TOKEN?: string
}

interface ImportMeta {
  readonly env: ImportMetaEnv
}
