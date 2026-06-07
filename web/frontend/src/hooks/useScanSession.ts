import { useCallback, useEffect, useRef, useState } from 'react'
import { getScan, listScans, submitScan, subscribeScanEvents } from '../api'
import type { ScanEvent, ScanJob, ScanOptions, ScanResult } from '../api'
import { isRootPath, scanIdFromPath, setScanRoute, type RouteMode } from '../lib/scan-route'

export function useScanSession() {
  const [scans, setScans] = useState<ScanJob[]>([])
  const [activeScan, setActiveScan] = useState<ScanJob | null>(null)
  const [progressLines, setProgressLines] = useState<string[]>([])
  const [report, setReport] = useState('')
  const [result, setResult] = useState<ScanResult | null>(null)
  const [scanning, setScanning] = useState(false)
  const [error, setError] = useState('')
  const [logCollapsed, setLogCollapsed] = useState(false)
  const unsubRef = useRef<(() => void) | null>(null)
  const activationRef = useRef(0)

  const refreshScans = useCallback(async () => {
    try {
      setScans(await listScans())
    } catch {}
  }, [])

  useEffect(() => {
    refreshScans()
  }, [refreshScans])

  function closeSubscription() {
    if (unsubRef.current) {
      unsubRef.current()
      unsubRef.current = null
    }
  }

  async function submit(target: string, mode: string, options: ScanOptions) {
    const activation = ++activationRef.current
    setError('')
    setProgressLines([])
    setReport('')
    setResult(null)
    setScanning(true)
    setLogCollapsed(false)

    try {
      const job = await submitScan(target, mode, options)
      if (activation !== activationRef.current) return
      refreshScans()
      await activateScan(job, 'push', activation)
    } catch (err: any) {
      if (activation !== activationRef.current) return
      setError(err.message || 'Failed to submit scan')
      setScanning(false)
    }
  }

  function subscribeToScan(id: string) {
    closeSubscription()
    unsubRef.current = subscribeScanEvents(id, (event: ScanEvent) => {
      if (event.type === 'progress' && event.data) {
        setProgressLines((prev) => [...prev, event.data!])
        if (event.result) {
          setResult(event.result)
          setActiveScan((scan) => (scan?.id === id ? { ...scan, result: event.result } : scan))
        }
        return
      }

      if (event.type === 'status' && event.status) {
        setActiveScan((scan) =>
          scan?.id === id
            ? { ...scan, status: event.status as ScanJob['status'], updated_at: new Date().toISOString() }
            : scan,
        )
        return
      }

      if (event.type === 'complete') {
        setScanning(false)
        setLogCollapsed(true)
        setError('')
        if (event.result) setResult(event.result)
        setActiveScan((scan) =>
          scan?.id === id
            ? {
                ...scan,
                status: 'completed',
                result: event.result || scan.result,
                updated_at: new Date().toISOString(),
              }
            : scan,
        )
        refreshScans()
        if (!event.result) {
          loadResult(id)
        }
        return
      }

      if (event.type === 'error') {
        setScanning(false)
        setActiveScan((scan) =>
          scan?.id === id
            ? {
                ...scan,
                status: 'failed',
                error: event.error || 'Scan failed',
                updated_at: new Date().toISOString(),
              }
            : scan,
        )
        setError(event.error || 'Scan failed')
        refreshScans()
      }
    })
  }

  async function loadResult(id: string, activation?: number) {
    try {
      const job = await getScan(id)
      if (activation && activation !== activationRef.current) return
      if (job.result) setResult(job.result)
      if (job.report) setReport(job.report)
      setActiveScan((scan) => (scan?.id === id ? { ...scan, ...job } : scan))
    } catch {}
  }

  async function activateScan(scan: ScanJob, route: RouteMode, activation = ++activationRef.current) {
    setScanRoute(scan.id, route)
    closeSubscription()
    setActiveScan(scan)
    setError('')
    setProgressLines([])
    setResult(scan.result || null)
    setReport(scan.status === 'completed' && scan.report ? scan.report : '')
    setLogCollapsed(false)
    setScanning(scan.status === 'queued' || scan.status === 'running')

    if (scan.status === 'completed') {
      setScanning(false)
      setLogCollapsed(true)
      if (!scan.result) {
        await loadResult(scan.id, activation)
      }
    } else if (scan.status === 'queued' || scan.status === 'running') {
      subscribeToScan(scan.id)
    } else {
      setScanning(false)
      setReport('')
    }
  }

  async function loadScanById(id: string, route: RouteMode) {
    const activation = ++activationRef.current
    setError('')
    try {
      const job = await getScan(id)
      if (activation !== activationRef.current) return
      await activateScan(job, route, activation)
    } catch {
      if (activation !== activationRef.current) return
      resetActiveScan()
      setError(`Scan ${id} was not found`)
    }
  }

  function resetActiveScan() {
    activationRef.current += 1
    closeSubscription()
    setActiveScan(null)
    setProgressLines([])
    setReport('')
    setResult(null)
    setScanning(false)
    setLogCollapsed(false)
  }

  function clearActiveScan() {
    resetActiveScan()
    setError('')
  }

  useEffect(() => {
    const applyRoute = () => {
      const id = scanIdFromPath(window.location.pathname)
      if (id) {
        void loadScanById(id, 'none')
        return
      }
      if (isRootPath(window.location.pathname)) {
        clearActiveScan()
      }
    }

    applyRoute()
    window.addEventListener('popstate', applyRoute)
    return () => {
      window.removeEventListener('popstate', applyRoute)
      closeSubscription()
    }
  }, [])

  return {
    scans,
    activeScan,
    progressLines,
    report,
    result,
    scanning,
    error,
    logCollapsed,
    refreshScans,
    submit,
    selectScan: (scan: ScanJob) => activateScan(scan, 'push'),
    clearError: () => setError(''),
    toggleLog: () => setLogCollapsed((collapsed) => !collapsed),
  }
}
