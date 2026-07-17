import { createContext, useContext } from 'react'
import type { Session } from './types'

export const SessionContext = createContext<Session | null>(null)
export const useSession = () => useContext(SessionContext)!
