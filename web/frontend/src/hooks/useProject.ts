import { useCallback, useEffect, useRef, useState } from 'react'
import { getProjects, createProject as apiCreateProject, deleteProject as apiDeleteProject, type Project } from '../api'

const STORAGE_KEY = 'aiscan-project'
export const DEFAULT_PROJECT = 'default'

export interface UseProject {
  /** Active project id — scopes the asset pool. */
  project: string
  projects: Project[]
  setProject: (id: string) => void
  createProject: (name: string) => Promise<void>
  /** Delete a project and its asset pool. The default project is never deletable. */
  deleteProject: (id: string) => Promise<void>
  refresh: () => Promise<void>
}

function initialProject(): string {
  if (typeof window === 'undefined') return DEFAULT_PROJECT
  try {
    return window.localStorage.getItem(STORAGE_KEY) || DEFAULT_PROJECT
  } catch {
    return DEFAULT_PROJECT
  }
}

function persistProject(id: string) {
  if (typeof window === 'undefined') return
  try {
    window.localStorage.setItem(STORAGE_KEY, id)
  } catch {
    /* ignore */
  }
}

/**
 * The active project scopes the asset pool: separate engagements keep separate
 * inventories instead of one global list. The current selection persists in
 * localStorage so a reload stays in the same project; the list comes from the
 * hub, which always seeds a "default" project holding the pre-project rows.
 * Its id doubles as the derived IOA space name when deploying agents, so an
 * operator picks one thing. See [[aiscan-asset-pool]].
 */
export function useProject(): UseProject {
  const [project, setProjectState] = useState<string>(initialProject)
  const [projects, setProjects] = useState<Project[]>([])

  // Monotonic request id: refresh is called concurrently (createProject + the
  // 6s asset-pool poll), so an older getProjects can resolve after a newer one.
  // Only the latest-issued request is allowed to apply its snapshot, otherwise
  // a stale list could drop a just-created project and reset the active one.
  const refreshSeqRef = useRef(0)
  const refresh = useCallback(async () => {
    const seq = ++refreshSeqRef.current
    try {
      const list = await getProjects()
      if (seq !== refreshSeqRef.current) return
      setProjects(list)
      setProjectState((current) => {
        const next = list.some((p) => p.id === current) ? current : DEFAULT_PROJECT
        if (next !== current) persistProject(next)
        return next
      })
    } catch {
      /* keep last-known list on transient errors */
    }
  }, [])

  useEffect(() => {
    refresh()
  }, [refresh])

  const setProject = useCallback((id: string) => {
    const next = id || DEFAULT_PROJECT
    setProjectState(next)
    persistProject(next)
  }, [])

  const createProject = useCallback(
    async (name: string) => {
      const p = await apiCreateProject(name)
      await refresh()
      setProject(p.id)
    },
    [refresh, setProject],
  )

  const deleteProject = useCallback(
    async (id: string) => {
      if (id === DEFAULT_PROJECT) return
      await apiDeleteProject(id)
      // If the deleted project was active, fall back to default eagerly so the
      // asset-pool poll doesn't fire a request for a project id that's now gone
      // (which the server rejects) in the gap before refresh lands.
      setProjectState((current) => {
        if (current !== id) return current
        persistProject(DEFAULT_PROJECT)
        return DEFAULT_PROJECT
      })
      await refresh()
    },
    [refresh],
  )

  return { project, projects, setProject, createProject, deleteProject, refresh }
}
