import { ref, type Ref } from 'vue'

export type AppRouteName = 'login' | 'mailboxes'

const STORAGE_KEY = 'gptbox.ui.route'
const LEGACY_STORAGE_KEY = 'mailbox.ui.route'
const routeState = ref<AppRouteName>('login')
let initialized = false

function normalizeRoute(value: unknown): AppRouteName | undefined {
  if (value === 'login' || value === 'mailboxes') {
    return value
  }
  if (value === 'gptAccounts' || value === 'sms') {
    return 'mailboxes'
  }
  return undefined
}

function parseHash(hash: string): AppRouteName | undefined {
  const segments = hash.replace(/^#\/?/, '').trim().split('/').filter(Boolean)
  return normalizeRoute(segments.at(-1))
}

function readInitialRoute(): AppRouteName {
  if (typeof window === 'undefined') {
    return 'login'
  }

  const hashRoute = parseHash(window.location.hash)
  if (hashRoute) {
    return hashRoute
  }

  try {
    const raw = window.localStorage.getItem(STORAGE_KEY) ?? window.localStorage.getItem(LEGACY_STORAGE_KEY)
    if (raw) {
      const route = normalizeRoute(JSON.parse(raw) as unknown)
      if (route) {
        window.localStorage.setItem(STORAGE_KEY, JSON.stringify(route))
        window.localStorage.removeItem(LEGACY_STORAGE_KEY)
        return route
      }
    }
  } catch {
    // best-effort only
  }

  return 'login'
}

function applyRoute(route: AppRouteName) {
  routeState.value = route
  if (typeof window === 'undefined') {
    return
  }

  try {
    window.localStorage.setItem(STORAGE_KEY, JSON.stringify(route))
  } catch {
    // best-effort only
  }

  const nextHash = route === 'login' ? '#/login' : '#/app/mailboxes'
  if (window.location.hash !== nextHash) {
    window.location.hash = nextHash
  }
}

function initializeRouteSync() {
  if (initialized || typeof window === 'undefined') {
    return
  }
  initialized = true
  routeState.value = readInitialRoute()
  window.addEventListener('hashchange', () => {
    const nextRoute = readInitialRoute()
    if (routeState.value !== nextRoute) {
      applyRoute(nextRoute)
    } else if (window.location.hash !== (nextRoute === 'login' ? '#/login' : '#/app/mailboxes')) {
      applyRoute(nextRoute)
    }
  })
}

initializeRouteSync()

export function useAppRoute(): Ref<AppRouteName> {
  return routeState
}

export function navigateToAppRoute(route: AppRouteName): void {
  applyRoute(route)
}
