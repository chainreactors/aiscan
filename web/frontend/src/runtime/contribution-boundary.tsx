import { Component, type ErrorInfo, type ReactNode } from 'react'
import { useTranslation } from 'react-i18next'

type Props = { id: string; resetKey: unknown; children: ReactNode; visible?: boolean }

// A failed optional view must not unmount the workbench and its state owners.
export class ContributionBoundary extends Component<Props, { failed: boolean }> {
  state = { failed: false }
  static getDerivedStateFromError() { return { failed: true } }
  componentDidCatch(error: Error, info: ErrorInfo) { console.error(`Contribution ${this.props.id} failed`, error, info.componentStack) }
  componentDidUpdate(previous: Props) {
    if (this.state.failed && previous.resetKey !== this.props.resetKey) this.setState({ failed: false })
  }
  render() {
    if (!this.state.failed) return this.props.children
    if (this.props.visible === false) return null
    return <ContributionFailure id={this.props.id} retry={() => this.setState({ failed: false })} />
  }
}

function ContributionFailure({ id, retry }: { id: string; retry: () => void }) {
  const { t } = useTranslation('app')
  return <span role="alert" data-failed-contribution={id} className="inline-flex max-w-full items-center gap-2 rounded border border-border bg-card p-2 text-xs text-muted-foreground">
    <span>{t('pluginViewFailed')}</span>
    <button type="button" onClick={retry} className="shrink-0 text-primary underline">{t('crashReset')}</button>
  </span>
}
