import { useState, useEffect, useCallback, useRef } from 'react'
import { Settings, Shield } from 'lucide-react'
import Sidebar from './components/Sidebar'
import ScanForm from './components/ScanForm'
import ScanView from './components/ScanView'
import LLMConfigPanel from './components/LLMConfigPanel'
import { submitScan, listScans, getScan, subscribeScanEvents, getReport, getStatus } from './api'
import type { ScanJob, ScanEvent, ScanOptions, ServerStatus, StructuredResult } from './api'
import { Button } from './components/ui/button'

export default function App() {
  const [scans, setScans] = useState<ScanJob[]>([])
  const [activeScan, setActiveScan] = useState<ScanJob | null>(null)
  const [progressLines, setProgressLines] = useState<string[]>([])
  const [report, setReport] = useState('')
  const [result, setResult] = useState<StructuredResult | null>(null)
  const [scanning, setScanning] = useState(false)
  const [error, setError] = useState('')
  const [analysisAvailable, setAnalysisAvailable] = useState(true)
  const [serverStatus, setServerStatus] = useState<ServerStatus | null>(null)
  const [llmConfigOpen, setLLMConfigOpen] = useState(false)
  const [sidebarOpen, setSidebarOpen] = useState(false)
  const [logCollapsed, setLogCollapsed] = useState(false)
  const unsubRef = useRef<(() => void) | null>(null)

  const refreshScans = useCallback(async () => {
    try {
      const list = await listScans()
      setScans(list || [])
    } catch {}
  }, [])

  useEffect(() => {
    refreshScans()
  }, [refreshScans])

  const refreshStatus = useCallback(async () => {
    try {
      const status = await getStatus()
      setServerStatus(status)
      setAnalysisAvailable(status.llm_available)
    } catch {
      setAnalysisAvailable(true)
    }
  }, [])

  useEffect(() => {
    refreshStatus()
  }, [refreshStatus])

  const handleSubmit = async (target: string, mode: string, options: ScanOptions) => {
    setError('')
    setProgressLines([])
    setReport('')
    setResult(null)
    setScanning(true)
    setLogCollapsed(false)

    try {
      const job = await submitScan(target, mode, options)
      setActiveScan(job)
      refreshScans()
      subscribeToScan(job.id)
    } catch (err: any) {
      setError(err.message || 'Failed to submit scan')
      setScanning(false)
    }
  }

  const subscribeToScan = (id: string) => {
    if (unsubRef.current) {
      unsubRef.current()
    }

    const unsub = subscribeScanEvents(id, (event: ScanEvent) => {
      if (event.type === 'progress' && event.data) {
        setProgressLines((prev) => [...prev, event.data!])
        if (event.result) {
          setResult(event.result)
          setActiveScan((scan) => (scan?.id === id ? { ...scan, result: event.result } : scan))
        }
      } else if (event.type === 'status' && event.status) {
        setActiveScan((scan) =>
          scan?.id === id
            ? { ...scan, status: event.status as ScanJob['status'], updated_at: new Date().toISOString() }
            : scan,
        )
      } else if (event.type === 'complete') {
        setScanning(false)
        setLogCollapsed(true)
        setError('')
        if (event.result) {
          setResult(event.result)
        }
        setActiveScan((scan) =>
          scan?.id === id
            ? { ...scan, status: 'completed', updated_at: new Date().toISOString() }
            : scan,
        )
        refreshScans()
        if (!event.result) {
          loadResult(id)
        }
        loadReport(id)
      } else if (event.type === 'error') {
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
    unsubRef.current = unsub
  }

  const loadReport = async (id: string) => {
    try {
      const r = await getReport(id)
      setReport(r)
    } catch {
      const job = await getScan(id)
      if (job.report) {
        setReport(job.report)
      }
    }
  }

  const loadResult = async (id: string) => {
    try {
      const job = await getScan(id)
      if (job.result) {
        setResult(job.result)
      }
    } catch {}
  }

  const handleSelectScan = async (scan: ScanJob) => {
    setActiveScan(scan)
    setError('')
    setProgressLines([])
    setResult(scan.result || null)
    setLogCollapsed(false)

    if (scan.status === 'completed') {
      setLogCollapsed(true)
      if (!scan.result) {
        await loadResult(scan.id)
      }
      if (scan.report) {
        setReport(scan.report)
      } else {
        await loadReport(scan.id)
      }
    } else if (scan.status === 'running') {
      setScanning(true)
      subscribeToScan(scan.id)
    } else {
      setReport('')
    }
  }

  return (
    <div className="min-h-screen flex">
      <Sidebar
        open={sidebarOpen}
        onToggle={() => setSidebarOpen(!sidebarOpen)}
        scans={scans}
        activeId={activeScan?.id}
        onSelectScan={handleSelectScan}
      />

      <main className="flex-1 flex flex-col min-w-0">
        {/* Header with form */}
        <div className="flex flex-wrap items-center gap-3 border-b border-border bg-card/30 p-4 backdrop-blur-sm">
          <div className="min-w-0 flex-1">
            <ScanForm onSubmit={handleSubmit} disabled={scanning} analysisAvailable={analysisAvailable} />
          </div>
          <Button
            type="button"
            variant="outline"
            onClick={() => setLLMConfigOpen(true)}
            className="h-10 shrink-0"
          >
            <Settings className="h-4 w-4" />
            <span className="hidden sm:inline">LLM</span>
          </Button>
        </div>

        {/* Error */}
        {error && (
          <div className="mx-4 mt-3 px-3 py-2 bg-destructive/10 border border-destructive/30 rounded-md text-destructive text-sm animate-fade-in">
            {error}
          </div>
        )}

        {/* Content */}
        {activeScan ? (
          <div className="flex-1 p-4 overflow-auto">
            <ScanView
              scan={activeScan}
              lines={progressLines}
              report={report}
              result={result}
              logCollapsed={logCollapsed}
              onToggleLog={() => setLogCollapsed(!logCollapsed)}
            />
          </div>
        ) : (
          <div className="flex-1 flex items-center justify-center">
            <div className="text-center space-y-3">
              <Shield className="w-16 h-16 mx-auto text-muted-foreground/10" strokeWidth={1} />
              <p className="text-muted-foreground text-sm">Enter a target to start scanning</p>
              <p className="text-muted-foreground/50 text-xs">
                Security scanning with structured reports
              </p>
            </div>
          </div>
        )}
      </main>
      <LLMConfigPanel
        open={llmConfigOpen}
        status={serverStatus}
        onClose={() => setLLMConfigOpen(false)}
        onSaved={refreshStatus}
      />
    </div>
  )
}
