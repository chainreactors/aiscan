export function recordMediaURL(sessionId: string, eventId: string, index: number, download = false): string {
  return `/api/sessions/${encodeURIComponent(sessionId)}/media/${encodeURIComponent(eventId)}/${index}${download ? '?download=1' : ''}`
}
