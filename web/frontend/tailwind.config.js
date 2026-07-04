import { aspectPreset } from './cyber-ui/packages/theme/src/tailwind-preset.ts'

const preset = aspectPreset()

/** @type {import('tailwindcss').Config} */
export default {
  presets: [preset],
  darkMode: 'class',
  content: [
    './index.html',
    './src/**/*.{js,ts,jsx,tsx}',
    './cyber-ui/packages/*/src/**/*.{js,ts,jsx,tsx}',
  ],
  theme: {
    extend: {
      ...preset.theme?.extend,
      colors: {
        ...preset.theme?.extend?.colors,
        // Cortex — the AI/agent role (used only where the AI is the actor)
        ai: {
          DEFAULT: 'hsl(var(--ai))',
          foreground: 'hsl(var(--ai-foreground))',
        },
      },
      fontFamily: {
        // Latin → Inter; Chinese → MiSans (refined) → Noto Sans SC (system deep fallback) → local PingFang.
        sans: ['Inter Variable', 'Inter', 'MiSans', 'Noto Sans SC', 'PingFang SC', 'system-ui', '-apple-system', 'sans-serif'],
        // Mono stack carries a CJK fallback too, so Chinese in eyebrows / breadcrumbs
        // renders MiSans instead of leaking to a random system CJK face.
        mono: ['JetBrains Mono Variable', 'JetBrains Mono', 'ui-monospace', 'SFMono-Regular', 'Menlo', 'MiSans', 'PingFang SC', 'monospace'],
        // Space Grotesk — the characterful Latin display face (wordmark, headings, numerals).
        // Chinese headings fall through to MiSans (Space Grotesk has no CJK glyphs).
        display: ['Space Grotesk Variable', 'Space Grotesk', 'Inter Variable', 'Inter', 'MiSans', 'PingFang SC', 'system-ui', 'sans-serif'],
      },
      boxShadow: {
        ...preset.theme?.extend?.boxShadow,
        // Theme-aware elevation — values live in index.css (:root / .dark) so light
        // gets a cool layered umbra and dark keeps its glassy top-lit black drop.
        soft: 'var(--shadow-soft)',
        lifted: 'var(--shadow-lifted)',
        elevated: 'var(--shadow-elevated)',
        glow: '0 8px 26px -10px hsl(var(--primary) / 0.55)',
        'glow-sm': '0 4px 14px -8px hsl(var(--primary) / 0.55)',
      },
      keyframes: {
        ...preset.theme?.extend?.keyframes,
        'fade-in': {
          from: { opacity: '0', transform: 'translateY(4px)' },
          to: { opacity: '1', transform: 'translateY(0)' },
        },
        'slide-in-right': {
          from: { transform: 'translateX(-100%)' },
          to: { transform: 'translateX(0)' },
        },
        'pulse-glow': {
          '0%, 100%': { opacity: '1' },
          '50%': { opacity: '0.45' },
        },
      },
      animation: {
        ...preset.theme?.extend?.animation,
        'fade-in': 'fade-in 0.3s ease-out',
        'slide-in-right': 'slide-in-right 0.2s ease-out',
        'pulse-glow': 'pulse-glow 2s ease-in-out infinite',
      },
    },
  },
  plugins: [
    require('tailwindcss-animate'),
    require('@tailwindcss/typography'),
  ],
}
