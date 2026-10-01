import { useTranslation } from 'react-i18next'
import { EasmResultView, type SCOResultModel } from '@cyber/cstx-easm'

export default function AssetResultView({ model, anchorPrefix }: { model: SCOResultModel; anchorPrefix?: string }) {
  const { t } = useTranslation('findings')
  return <EasmResultView model={model} anchorPrefix={anchorPrefix} linkLabel={name => t('linkTo', { name })}
    labels={Object.fromEntries(['hosts', 'noHosts', 'ips', 'ports', 'apps', 'urls', 'frameworks', 'vulns', 'sitemap', 'expandAll', 'collapseAll', 'details', 'request', 'response'].map(key => [key, t(key)]))} />
}
