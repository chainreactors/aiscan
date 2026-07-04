import { useEffect, useRef } from 'react'

// The elements a keyboard user can Tab to inside a panel.
const FOCUSABLE = [
  'a[href]',
  'button:not([disabled])',
  'textarea:not([disabled])',
  'input:not([disabled])',
  'select:not([disabled])',
  '[tabindex]:not([tabindex="-1"])',
].join(',')

/**
 * Accessibility plumbing for the hand-rolled (non-Radix) modal panels —
 * AgentPanel / ConfigPanel / DeployPanel. Radix `Dialog` (see ConfirmDialog)
 * gives focus-trap + focus-restore + Esc for free; these panels keep bespoke
 * layouts, so this lifts them to the same bar without a rewrite:
 *   • Esc closes (bubble phase, so a Radix Select/popover's own Esc still wins
 *     when it's open — its handler stops propagation before this one runs).
 *   • On open, focus moves into the panel: its first focusable control, else the
 *     panel container itself (give the ref'd element tabIndex={-1} for that).
 *   • Tab is trapped inside, wrapping at both edges and pulled back in if focus
 *     ever escapes to the page behind.
 *   • On close, focus returns to whatever held it when the panel opened (the
 *     trigger) instead of falling to <body> and losing the user's place.
 *
 * `onClose` is read through a ref so an inline `() => setOpen(false)` prop — a
 * new identity every render — doesn't re-run the effect and yank focus back to
 * the first control on each parent re-render.
 */
export function useDialogA11y(
  open: boolean,
  onClose: () => void,
  contentRef: React.RefObject<HTMLElement>,
) {
  const onCloseRef = useRef(onClose)
  onCloseRef.current = onClose

  useEffect(() => {
    if (!open) return
    const content = contentRef.current
    // Remember the trigger so focus can return to it on close.
    const restoreTo = document.activeElement as HTMLElement | null

    const focusable = () =>
      content
        ? Array.from(content.querySelectorAll<HTMLElement>(FOCUSABLE)).filter(
            (el) => el.offsetParent !== null,
          )
        : []

    const first = focusable()[0]
    if (first) first.focus()
    else content?.focus()

    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
        e.preventDefault()
        onCloseRef.current()
        return
      }
      if (e.key !== 'Tab' || !content) return
      const items = focusable()
      if (items.length === 0) {
        // Nothing focusable inside — hold focus on the container so Tab can't
        // walk out into the page behind the modal.
        e.preventDefault()
        content.focus()
        return
      }
      const firstEl = items[0]
      const lastEl = items[items.length - 1]
      const active = document.activeElement as HTMLElement | null
      // Recover only when focus has genuinely fallen off (onto <body>). A Radix
      // Select/Popover renders its list in a portal outside `content`, so a plain
      // `!content.contains(active)` check would wrongly reclaim focus from it and
      // break arrow-key navigation — portaled content is never <body>.
      const escaped = !active || active === document.body
      if (e.shiftKey) {
        if (active === firstEl || escaped) {
          e.preventDefault()
          lastEl.focus()
        }
      } else if (active === lastEl || escaped) {
        e.preventDefault()
        firstEl.focus()
      }
    }

    document.addEventListener('keydown', onKey)
    return () => {
      document.removeEventListener('keydown', onKey)
      if (restoreTo && document.contains(restoreTo)) restoreTo.focus()
    }
  }, [open, contentRef])
}
