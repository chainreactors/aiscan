import { useCallback, useEffect, useRef, useState } from 'react'
import { usePolling } from './usePolling'
import { getAssets, addAssets as apiAddAssets, deleteAsset as apiDeleteAsset, type PoolAsset } from '../api'
import type { TimelineItem } from './useChatSession'
import { toolCallAssets } from '../lib/agent-assets'

const POLL_MS = 6000

export interface UseAssetPool {
  assets: PoolAsset[]
  add: (targets: string[] | string, source?: string, label?: string) => Promise<void>
  remove: (id: string) => Promise<void>
  refresh: () => Promise<void>
}

/**
 * The shared asset pool: a deduplicated target inventory fed by three sources —
 * completed scans (server-ingested), the live agent timeline (recon), and
 * manual human entry. It polls the hub so scan-ingested assets and other
 * viewers' additions surface without a manual refresh. Pass the chat timeline
 * so agent-discovered network assets auto-upsert as the agent works.
 */
export function useAssetPool(timeline: TimelineItem[], project: string, onProjectsChanged?: () => Promise<void>): UseAssetPool {
  const [assets, setAssets] = useState<PoolAsset[]>([])
  const projectRef = useRef(project)
  // Targets already known to the pool (from the server or already pushed), so
  // the agent-timeline effect never re-POSTs the same target every render.
  const knownRef = useRef<Set<string>>(new Set())
  // Tool-call asset events already handled during this browser session. This is
  // intentionally not reset on project switch, otherwise old chat timeline items
  // would be replayed into the newly selected project.
  const processedAgentKeysRef = useRef<Set<string>>(new Set())

  const refresh = useCallback(async () => {
    try {
      const list = await getAssets(project)
      if (projectRef.current !== project) return
      setAssets(list)
      knownRef.current = new Set(list.map((a) => a.target))
      void onProjectsChanged?.()
    } catch {
      /* keep last-known pool on transient errors */
    }
  }, [project, onProjectsChanged])

  const add = useCallback(
    async (targets: string[] | string, source?: string, label?: string) => {
      const raw = Array.isArray(targets) ? targets : String(targets).split(/[\s,]+/)
      const clean = raw.map((s) => s.trim()).filter(Boolean)
      if (clean.length === 0) return
      for (const t of clean) knownRef.current.add(t)
      try {
        await apiAddAssets(clean, source, label, project)
      } finally {
        await refresh()
      }
    },
    [project, refresh],
  )

  const remove = useCallback(
    async (id: string) => {
      const gone = assets.find((a) => a.id === id)
      if (gone) knownRef.current.delete(gone.target) // let a later re-discovery re-add it
      setAssets((prev) => prev.filter((a) => a.id !== id)) // optimistic
      try {
        await apiDeleteAsset(id, project)
      } finally {
        await refresh()
      }
    },
    [assets, project, refresh],
  )

  // Switching projects swaps to a different inventory: clear the visible pool
  // and the per-project target guard. The event guard above intentionally stays
  // intact so old chat timeline items are not replayed into the new project.
  useEffect(() => {
    projectRef.current = project
    knownRef.current = new Set()
    setAssets([])
  }, [project])

  // Initial load (also re-fires on project switch since `refresh` changes).
  useEffect(() => {
    refresh()
  }, [refresh])
  // Light poll — paused while the tab is hidden.
  usePolling(refresh, POLL_MS)

  // Agent recon → pool. Whenever the live timeline surfaces a network asset we
  // haven't sent yet, upsert it (source=agent). processedAgentKeysRef tracks the
  // timeline event itself, so switching projects does not replay old agent hits
  // into the new project; on failure we roll back so the next change retries.
  useEffect(() => {
    const pendingKeys: string[] = []
    const freshSet = new Set<string>()
    for (const item of timeline) {
      if (item.kind !== 'assistant_response' || !item.assistantResponse) continue
      for (const tool of item.assistantResponse.tools) {
        for (const cand of toolCallAssets(tool.toolName, tool.toolArgs, tool.result)) {
          const key = `${item.id}\0${tool.id}\0${cand}`
          if (processedAgentKeysRef.current.has(key)) continue
          processedAgentKeysRef.current.add(key)
          pendingKeys.push(key)
          if (knownRef.current.has(cand)) continue
          knownRef.current.add(cand)
          freshSet.add(cand)
        }
      }
    }
    const fresh = [...freshSet]
    if (fresh.length === 0) return
    apiAddAssets(fresh, 'agent', undefined, project)
      .then(() => refresh())
      .catch(() => {
        for (const t of fresh) knownRef.current.delete(t)
        for (const key of pendingKeys) processedAgentKeysRef.current.delete(key)
      })
  }, [timeline, project, refresh])

  return { assets, add, remove, refresh }
}
