import { createContext, useContext, type ReactNode } from "react"

import type { ApiClient } from "@workspace/ui/lib/api-client"

const ApiContext = createContext<ApiClient | null>(null)

/**
 * Provides a configured {@link ApiClient} to the CRUD components (CommonTable, CrudForm).
 * Configure it once near the app root with a client created via `createApiClient`.
 */
export function ApiProvider({
  client,
  children,
}: {
  client: ApiClient
  children: ReactNode
}) {
  return <ApiContext.Provider value={client}>{children}</ApiContext.Provider>
}

/** Returns the configured API client; throws if used outside an ApiProvider. */
export function useApi(): ApiClient {
  const client = useContext(ApiContext)
  if (!client) {
    throw new Error("useApi must be used within an <ApiProvider>")
  }
  return client
}
