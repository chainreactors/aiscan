const scanRouteBase = 'scans'

export type RouteMode = 'none' | 'push' | 'replace'

export function scanIdFromPath(pathname: string) {
  const segments = pathname.split('/').filter(Boolean)
  if (segments.length < 2 || segments[0] !== scanRouteBase) {
    return null
  }
  try {
    return decodeURIComponent(segments[1])
  } catch {
    return segments[1]
  }
}

export function isRootPath(pathname: string) {
  return pathname === '' || pathname === '/'
}

export function scanRoutePath(id: string) {
  return `/${scanRouteBase}/${encodeURIComponent(id)}`
}

export function setScanRoute(id: string, mode: RouteMode) {
  setBrowserRoute(scanRoutePath(id), mode)
}

function setBrowserRoute(path: string, mode: RouteMode) {
  if (mode === 'none') {
    return
  }
  const current = `${window.location.pathname}${window.location.search}${window.location.hash}`
  if (current === path) {
    return
  }
  if (mode === 'replace') {
    window.history.replaceState({}, '', path)
  } else {
    window.history.pushState({}, '', path)
  }
}
