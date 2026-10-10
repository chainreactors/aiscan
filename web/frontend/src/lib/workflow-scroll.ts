// Native scrollbar drags also fire scroll events. Record our own final, clamped
// positions so those events can be distinguished from manual scrolling.
const automatic = new WeakMap<HTMLElement, { top: number; left: number }>()

export function scrollWorkflowViewport(element: HTMLElement, top: number, left: number) {
  element.scrollTo({ top, left, behavior: 'instant' })
  automatic.set(element, { top: element.scrollTop, left: element.scrollLeft })
}

export function isWorkflowAutomaticScroll(element: HTMLElement) {
  const position = automatic.get(element)
  return position?.top === element.scrollTop && position.left === element.scrollLeft
}

export function scrollWorkflowKey(element: HTMLElement, key: string, shift: boolean) {
  const horizontal = key === 'ArrowLeft' || key === 'ArrowRight'
    || (key === 'Home' || key === 'End') && element.scrollHeight <= element.clientHeight
  const size = horizontal ? element.clientWidth : element.clientHeight
  const position = horizontal ? element.scrollLeft : element.scrollTop
  const forward = ['ArrowDown', 'ArrowRight', 'PageDown'].includes(key) || key === ' ' && !shift
  const next = key === 'Home' ? 0 : key === 'End' ? horizontal ? element.scrollWidth : element.scrollHeight
    : position + (forward ? 1 : -1) * (key.startsWith('Arrow') ? 40 : size * .85)
  // Native PageUp/PageDown animate after keyup; an instant step avoids residual
  // movement undoing a subsequent explicit Follow or card selection.
  element.scrollTo({ top: horizontal ? element.scrollTop : next, left: horizontal ? next : element.scrollLeft, behavior: 'instant' })
}
