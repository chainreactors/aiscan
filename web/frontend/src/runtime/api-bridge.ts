import type { AOPClient } from '@cyber/aop'
import type { AuthService } from './auth'
import type { ConnectionService } from './connection'

let active: ConnectionService | undefined
let authentication: AuthService | undefined
let admitting = false

export function bindAuth(auth: AuthService) {
  if (authentication && authentication !== auth) throw new Error('Another browser runtime owns authentication')
  authentication = auth
  return () => { if (authentication === auth) authentication = undefined }
}
export function requireAuth() {
  if (!authentication) throw new Error('Start the application runtime before using authentication')
  return authentication
}
export function bindConnection(connection: ConnectionService) {
  if (active && active !== connection) throw new Error('Another Hub owns the legacy API bridge')
  active = connection
  admitting = true
  return () => { if (active === connection) { active = undefined; admitting = false } }
}
export function stopAdmission(owner: ConnectionService | undefined) {
  if (owner && active === owner) admitting = false
}
export function requireConnection() {
  if (!active || !admitting) throw new Error('No authenticated Hub connection')
  return active
}

// Compatibility facade only. No socket, subscription or connection lives here.
// Methods remain writable for existing transport fixtures.
export const aopClient = {
  get connected() { return admitting && !!active?.aop.connected },
  request: ((...args) => requireConnection().aop.request(...args)) as AOPClient['request'],
  send: ((...args) => requireConnection().aop.send(...args)) as AOPClient['send'],
  subscribe: ((...args) => requireConnection().aop.subscribe(...args)) as AOPClient['subscribe'],
  onConnectionChange(listener: (connected: boolean) => void) { return requireConnection().aop.onConnectionChange(listener) },
}
