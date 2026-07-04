import { useEffect, useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { X, FileText, Shield, TableProperties } from 'lucide-react'
import { Tabs, TabsList, TabsTrigger, TabsContent, Button } from '@aspect/ui'
import { fetchScanReport, type ScanResult } from '../api'
import { buildFindings } from '../lib/scan-result'
import { MarkdownContent } from '@aspect/markdown'
import AssetResultView from './AssetResultView'
import FindingsPanel from './FindingsPanel'

interface Props {
  scanID: string
  result: ScanResult | null
  report?: string
  onClose: () => void
}

type Tab = 'assets' | 'findings' | 'report'

export default function DetailPanel({ scanID, result, report, onClose }: Props) {
  const { t, i18n } = useTranslation('findings')
  const findingsCount = useMemo(() => {
    if (!result) return 0
    return buildFindings(result).length
  }, [result])

  const [tab, setTab] = useState<Tab>(findingsCount > 0 ? 'findings' : 'assets')
  // The findings tab is unmounted when a scan has no findings. This panel is
  // reused (no key remount) when retargeted at another scan, so a stale
  // tab==='findings' against a 0-findings scan would leave the controlled Tabs
  // with no matching content and render blank — fall back to 'assets'.
  const activeTab: Tab = tab === 'findings' && findingsCount === 0 ? 'assets' : tab

  // The report is (re)rendered server-side per language, so fetch it on open and
  // whenever the UI locale flips — that gives zh users the zh report instead of
  // the stored en one. The `report` prop, if ever passed, seeds the initial
  // value; a 404 (report not ready) resolves to '' and shows the placeholder.
  const [reportMd, setReportMd] = useState(report ?? '')
  useEffect(() => {
    let cancelled = false
    const lang = (i18n.resolvedLanguage || i18n.language || 'en').toLowerCase().startsWith('zh') ? 'zh' : 'en'
    fetchScanReport(scanID, lang)
      .then((md) => { if (!cancelled) setReportMd(md) })
      .catch(() => { /* keep placeholder */ })
    return () => { cancelled = true }
  }, [scanID, i18n.resolvedLanguage, i18n.language])

  return (
    <aside className="flex h-full w-full flex-col border-l border-border/60 bg-card shadow-lifted animate-in slide-in-from-right-2 duration-200">
      <div className="flex h-11 shrink-0 items-center justify-between border-b border-border/60 bg-card/70 px-3 backdrop-blur-md">
        <div className="flex items-center gap-2">
          <Shield className="h-3.5 w-3.5 text-primary" />
          <span className="text-xs font-medium text-foreground">{t('scanDetails')}</span>
          {result && (
            <span className="text-[10px] font-mono text-muted-foreground">
              {result.summary.targets} targets / {result.summary.loots} loots
            </span>
          )}
        </div>
        <Button variant="ghost" size="icon" onClick={onClose} className="h-7 w-7 text-muted-foreground" aria-label={t('closeDetailPanel')}>
          <X className="h-3.5 w-3.5" />
        </Button>
      </div>

      <div className="flex-1 overflow-auto p-3">
        {result ? (
          <Tabs value={activeTab} onValueChange={(v: string) => setTab(v as Tab)}>
            <TabsList>
              <TabsTrigger value="assets">
                <TableProperties className="h-3.5 w-3.5" />
                {t('assets')}
              </TabsTrigger>
              {findingsCount > 0 && (
                <TabsTrigger value="findings">
                  <Shield className="h-3.5 w-3.5" />
                  {t('findings')}
                  <span className="ml-1 rounded-full bg-destructive/15 px-1.5 py-0.5 text-[10px] font-bold text-destructive">
                    {findingsCount}
                  </span>
                </TabsTrigger>
              )}
              <TabsTrigger value="report">
                <FileText className="h-3.5 w-3.5" />
                {t('report')}
              </TabsTrigger>
            </TabsList>

            <TabsContent value="assets" className="animate-in fade-in duration-150">
              <AssetResultView result={result} />
            </TabsContent>
            {findingsCount > 0 && (
              <TabsContent value="findings" className="animate-in fade-in duration-150">
                <FindingsPanel result={result} />
              </TabsContent>
            )}
            <TabsContent value="report" className="animate-in fade-in duration-150">
              <div className="prose prose-sm dark:prose-invert max-w-none">
                <MarkdownContent content={reportMd || t('noReportAvailable')} headingAnchors />
              </div>
            </TabsContent>
          </Tabs>
        ) : (
          <div className="flex items-center justify-center py-10 text-xs text-muted-foreground">
            {t('noResultsAvailable')}
          </div>
        )}
      </div>
    </aside>
  )
}
