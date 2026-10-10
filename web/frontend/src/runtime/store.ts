export type Disposer = () => void
export interface Observable<T> {
  getSnapshot: () => T
  subscribe: (listener: () => void) => Disposer
}

// Snapshots keep their identity until a write; React can safely read them twice.
export class Store<T> implements Observable<T> {
  private listeners = new Set<() => void>()
  constructor(private value: T) {}
  getSnapshot = () => this.value
  subscribe = (listener: () => void) => {
    this.listeners.add(listener)
    return () => { this.listeners.delete(listener) }
  }
  set(value: T) {
    if (Object.is(this.value, value)) return
    this.value = value
    for (const listener of [...this.listeners]) listener()
  }
}
