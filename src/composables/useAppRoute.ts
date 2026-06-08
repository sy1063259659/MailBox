import { ref, type Ref } from 'vue'

export type AppRouteName = 'login' | 'mailboxes' | 'gptAccounts' | 'sms'

const STORAGE_KEY = 'gptbox.ui.route'
const LEGACY_STORAGE_KEY = 'mailbox.ui.route'
const routeState = ref<AppRouteName>('login')
let initialized = false

function isAppRouteName(value: unknown): value is AppRouteName {
  return value === 'login' || value === 'mailboxes' || value === 'gptAccounts' || value === 'sms'
}

function parseHash(hash: string): AppRouteName | undefined {
  const normalized = hash.replace(/^#\/?/, '').trim()
  const [firstSegment] = normalized.split('/')
  if (isAppRouteName(firstSegment)) {
    return firstSegment
  }
  return undefined
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
      const parsed = JSON.parse(raw) as unknown
      if (isAppRouteName(parsed)) {
        window.localStorage.setItem(STORAGE_KEY, JSON.stringify(parsed))
        window.localStorage.removeItem(LEGACY_STORAGE_KEY)
        return parsed
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

  const nextHash = route === 'login' ? '#/login' : `#/app/${route}`
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
      routeState.value = nextRoute
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
