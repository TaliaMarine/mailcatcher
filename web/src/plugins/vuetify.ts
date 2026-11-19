import 'vuetify/styles'
import '@mdi/font/css/materialdesignicons.css'
import { createVuetify } from 'vuetify'

/**
 * Instrument-panel theme, shared in spirit with the Flextract network status
 * app: petrol and blue-slate rather than stock Material blue, status colours
 * tuned to sit with petrol, and IBM Plex Mono kept for real machine values
 * (addresses, ids, timestamps, sizes) via the `.mono` class.
 */
export const tokens = {
  light: {
    background: '#F7F8FA',
    surface: '#FFFFFF',
    inset: '#EDF1F5',
    ink: '#16202B',
    muted: '#5C6B7A',
    rule: '#D3DAE3',
    primary: '#00697A',
    success: '#1B7F5A',
    warning: '#B26A00',
    error: '#B3261E',
  },
  dark: {
    background: '#0E141A',
    surface: '#151D26',
    inset: '#0B1015',
    ink: '#E4EAF0',
    muted: '#93A1B0',
    rule: '#26323E',
    primary: '#4DB6C8',
    success: '#4FB88D',
    warning: '#E0A044',
    error: '#F0766E',
  },
} as const

function theme(t: (typeof tokens)['light'] | (typeof tokens)['dark'], dark: boolean) {
  return {
    dark,
    colors: {
      background: t.background,
      surface: t.surface,
      'surface-variant': t.inset,
      'on-background': t.ink,
      'on-surface': t.ink,
      'on-surface-variant': t.muted,
      primary: t.primary,
      'on-primary': dark ? '#04262D' : '#FFFFFF',
      secondary: t.muted,
      success: t.success,
      warning: t.warning,
      error: t.error,
      info: t.primary,
      inset: t.inset,
    },
    variables: {
      'border-color': t.rule.replace('#', '').match(/../g)!.map((h) => parseInt(h, 16)).join(','),
      'border-opacity': 1,
      'high-emphasis-opacity': 1,
      'medium-emphasis-opacity': 0.72,
    },
  }
}

const stored = localStorage.getItem('mc-theme')
const preferDark = window.matchMedia?.('(prefers-color-scheme: dark)').matches

export default createVuetify({
  theme: {
    defaultTheme: stored ?? (preferDark ? 'dark' : 'light'),
    themes: {
      light: theme(tokens.light, false),
      dark: theme(tokens.dark, true),
    },
  },
  defaults: {
    global: { ripple: false },
    VBtn: { variant: 'text', rounded: 'md' },
    VCard: { variant: 'flat', border: true, rounded: 'md' },
    VChip: { size: 'small', rounded: 'md' },
    VTextField: { variant: 'outlined', density: 'comfortable', color: 'primary' },
    VTooltip: { location: 'bottom' },
  },
})
