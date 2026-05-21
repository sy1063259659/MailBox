import { ref, watch, type Ref } from 'vue'

interface LocalUiStateOptions<T> {
  validate?: (value: unknown) => value is T
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
      if (rawValue !== null) {
        const parsedValue = JSON.parse(rawValue) as unknown
        state.value = options.validate?.(parsedValue) === false
          ? defaultValue
          : parsedValue as T
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
