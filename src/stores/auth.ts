import { defineStore } from 'pinia'
import { ApiError } from '@/services/apiClient'
import { getCurrentUser, login, logout, type AuthUser } from '@/services/authApi'

interface AuthState {
  user: AuthUser | undefined
  loading: boolean
  checked: boolean
}

export const useAuthStore = defineStore('auth', {
  state: (): AuthState => ({
    user: undefined,
    loading: false,
    checked: false,
  }),

  getters: {
    isAuthenticated: (state): boolean => state.user !== undefined,
  },

  actions: {
    async checkSession(): Promise<void> {
      this.loading = true
      try {
        this.user = await getCurrentUser()
      } catch (error) {
        if (error instanceof ApiError && error.status === 401) {
          this.user = undefined
          return
        }
        this.user = undefined
      } finally {
        this.checked = true
        this.loading = false
      }
    },

    async login(username: string, password: string): Promise<void> {
      this.loading = true
      try {
        this.user = await login(username, password)
        this.checked = true
      } finally {
        this.loading = false
      }
    },

    async logout(): Promise<void> {
      await logout()
      this.user = undefined
      this.checked = true
    },

    markLoggedOut(): void {
      this.user = undefined
      this.checked = true
    },
  },
})
