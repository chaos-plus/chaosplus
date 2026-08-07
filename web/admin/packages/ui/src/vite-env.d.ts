// Ambient declarations for packages/ui, typechecked standalone via its own tsc --noEmit
// (it does not inherit apps/web's src/vite-env.d.ts). Declares only the Vite env vars and
// asset side-effect imports actually used by this package's components.

interface ImportMetaEnv {
  /** Google Maps JS API key for the map picker component. */
  readonly VITE_GOOGLE_MAPS_API_KEY?: string
  /** Mapbox access token for the map picker component. */
  readonly VITE_MAPBOX_TOKEN?: string
}

interface ImportMeta {
  readonly env: ImportMetaEnv
}

declare module "*.css" {
  const content: string
  export default content
}
