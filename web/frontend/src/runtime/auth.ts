import { Store } from './store'

export type AuthSnapshot = {
  state: 'checking' | 'authenticated' | 'unauthenticated'
  generation: number
}
export class AuthError extends Error {
  constructor(message: string, readonly status: number) { super(message) }
}

export class AuthService extends Store<AuthSnapshot> {
  private controller?: AbortController
  private stopped = false
  private loggingOut?: Promise<void>
  afterInvalidate: () => Promise<void> = async () => {}
  constructor() { super({ state: 'checking', generation: 0 }) }

  private begin(checking = true) {
    if (this.stopped) throw new Error('Authentication service is disposed')
    this.controller?.abort()
    this.controller = new AbortController()
    const generation = this.getSnapshot().generation + 1
    this.set({ state: checking ? 'checking' : this.getSnapshot().state, generation })
    return { generation, signal: this.controller.signal }
  }
  async check() {
    await this.loggingOut
    const { generation, signal } = this.begin()
    try {
      const response = await fetch('/api/auth/session', { cache: 'no-store', signal })
      const authenticated = response.ok && (await response.json()).authenticated === true
      if (this.isCurrent(generation)) this.set({ generation, state: authenticated ? 'authenticated' : 'unauthenticated' })
      return authenticated && this.isCurrent(generation)
    } catch {
      if (this.isCurrent(generation)) this.set({ generation, state: 'unauthenticated' })
      return false
    }
  }
  async login(token: string) {
    await this.loggingOut
    // Re-authentication must revoke the old Hub while the new cookie is pending.
    // Keep the login form mounted for ordinary unauthenticated submissions.
    const { generation, signal } = this.begin(this.getSnapshot().state !== 'unauthenticated')
    try {
      const response = await fetch('/api/auth/login', {
        method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ token }), signal,
      })
      if (!response.ok) throw new AuthError('Login failed', response.status)
      if (this.isCurrent(generation)) this.set({ generation, state: 'authenticated' })
    } catch (error) {
      if (this.isCurrent(generation)) this.set({ generation, state: 'unauthenticated' })
      throw error
    }
  }
  isCurrent = (generation: number) => !this.stopped && this.getSnapshot().generation === generation
  invalidate(generation = this.getSnapshot().generation) {
    if (!this.isCurrent(generation)) return
    this.controller?.abort()
    this.set({ state: 'unauthenticated', generation: generation + 1 })
  }
  logout(): Promise<void> {
    return this.loggingOut ??= (async () => {
      this.invalidate()
      const cleanup = this.afterInvalidate()
      try { await fetch('/api/auth/logout', { method: 'POST' }) }
      finally { await cleanup }
    })().finally(() => { this.loggingOut = undefined })
  }
  dispose(): Promise<void> {
    this.stopped = true
    this.controller?.abort()
    // A replacement browser must not race an old logout response's Set-Cookie.
    return this.loggingOut?.catch(() => {}) ?? Promise.resolve()
  }
}
