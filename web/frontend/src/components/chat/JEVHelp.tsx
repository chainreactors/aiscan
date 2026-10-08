import { memo, useCallback, useEffect, useId, useRef, useState, type ReactNode } from 'react'
import { CircleHelp } from 'lucide-react'
import { Popover, PopoverContent, PopoverTrigger } from '@cyber/ui'
import './JEVHelp.css'

// Hover for a quick explanation; click or tap to keep it open.
export const JEVHelp = memo(function JEVHelp({ title, children, testId, className, trigger, triggerClassName }: { title: string; children: ReactNode; testId?: string; className?: string; trigger?: ReactNode; triggerClassName?: string }) {
  const titleId = useId()
  const [open, setOpen] = useState(false)
  const [pinned, setPinned] = useState(false)
  const closeTimer = useRef<ReturnType<typeof setTimeout>>()
  const triggerElement = useRef<HTMLButtonElement>(null), contentElement = useRef<HTMLDivElement>(null)
  const cancelClose = useCallback(() => { clearTimeout(closeTimer.current) }, [])
  const closeLater = useCallback(() => { cancelClose(); if (!pinned) closeTimer.current = setTimeout(() => setOpen(false), 180) }, [cancelClose, pinned])
  useEffect(() => cancelClose, [cancelClose])
  useEffect(() => {
    if (!open) return
    const escape = (event: KeyboardEvent) => {
      if (event.key !== 'Escape') return
      // Handle the topmost help before the drawer's document-level listener.
      // The trigger keeps focus for keyboard inspection without auto-scrolling.
      event.preventDefault(); event.stopPropagation()
      cancelClose(); setOpen(false); setPinned(false)
      if (contentElement.current?.contains(document.activeElement)) triggerElement.current?.focus({ preventScroll: true })
    }
    window.addEventListener('keydown', escape, true)
    return () => window.removeEventListener('keydown', escape, true)
  }, [open, cancelClose])
  return <Popover open={open} onOpenChange={next => { cancelClose(); setOpen(next); if (!next) setPinned(false) }}>
    <PopoverTrigger asChild><button ref={triggerElement} type="button" className={triggerClassName || 'jev-help-trigger'} aria-label={title}
      onClick={event => { event.preventDefault(); cancelClose(); setPinned(!pinned); setOpen(!pinned) }}
      onPointerEnter={event => { if (event.pointerType === 'mouse') { cancelClose(); setOpen(true) } }}
      onPointerLeave={event => { if (event.pointerType === 'mouse') closeLater() }}>{trigger || <CircleHelp aria-hidden="true" />}</button></PopoverTrigger>
    <PopoverContent ref={contentElement} className={`jev-help-content ${className || ''}`} align="start" sideOffset={8} aria-labelledby={titleId} data-testid={testId}
      onPointerEnter={cancelClose} onPointerLeave={closeLater} onOpenAutoFocus={event => event.preventDefault()} onCloseAutoFocus={event => event.preventDefault()}>
      <h3 id={titleId}>{title}</h3>{children}
    </PopoverContent>
  </Popover>
})
