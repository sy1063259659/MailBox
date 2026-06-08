import { ref, watch, type Ref } from 'vue'

interface LocalUiStateOptions<T> {
  validate?: (value: unknown) => value is T
  legacyKey?: string
}

export function useLocalUiState<T>(
  key: string,
  defaultValue: T,
  options: LocalUiStateOptions<T> = {},
): Ref<T> {
  const state = ref(defaultValue) as Ref<T>

  if (typeof window !== 'undefined') {
    try {
      const rawValue = window.localStorage.getItem(key)
        ?? (options.legacyKey ? window.localStorage.getItem(options.legacyKey) : null)
      if (rawValue !== null) {
        const parsedValue = JSON.parse(rawValue) as unknown
        state.value = options.validate?.(parsedValue) === false
          ? defaultValue
          : parsedValue as T
        if (options.legacyKey) {
          window.localStorage.setItem(key, JSON.stringify(state.value))
          window.localStorage.removeItem(options.legacyKey)
        }
      }
    } catch {
      state.value = defaultValue
    }

    watch(
      state,
      (value) => {
        try {
          window.localStorage.setItem(key, JSON.stringify(value))
        } catch {
          // Persisting UI state is best-effort only.
        }
      },
      { deep: true },
    )
  }

  return state
}
