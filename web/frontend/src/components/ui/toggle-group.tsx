import * as React from 'react'
import { cn } from '@/lib/utils'

interface ToggleGroupProps {
  value: string
  onValueChange: (value: string) => void
  children: React.ReactNode
  className?: string
  disabled?: boolean
  ariaLabel?: string
}

function ToggleGroup({ value, onValueChange, children, className, disabled, ariaLabel }: ToggleGroupProps) {
  return (
    <div
      role="group"
      aria-label={ariaLabel}
      className={cn('inline-flex items-center rounded-md border border-input bg-secondary/50 p-0.5', className)}
    >
      {React.Children.map(children, (child) => {
        if (React.isValidElement<ToggleGroupItemProps>(child)) {
          return React.cloneElement(child, {
            active: child.props.value === value,
            onClick: () => !disabled && onValueChange(child.props.value),
            disabled,
          })
        }
        return child
      })}
    </div>
  )
}

interface ToggleGroupItemProps {
  value: string
  children: React.ReactNode
  className?: string
  active?: boolean
  onClick?: () => void
  disabled?: boolean
}

function ToggleGroupItem({ children, className, active, onClick, disabled }: ToggleGroupItemProps) {
  return (
    <button
      type="button"
      onClick={onClick}
      disabled={disabled}
      className={cn(
        'inline-flex items-center justify-center rounded-sm px-3 py-1 text-xs font-medium transition-all',
        active
          ? 'bg-primary text-primary-foreground shadow-sm'
          : 'text-muted-foreground hover:text-foreground',
        disabled && 'opacity-50 cursor-not-allowed',
        className
      )}
    >
      {children}
    </button>
  )
}

export { ToggleGroup, ToggleGroupItem }
