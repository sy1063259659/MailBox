import { apiGet, apiPost } from './apiClient'

export interface AuthUser {
  username: string
}

export async function login(username: string, password: string): Promise<AuthUser> {
  const response = await apiPost<{ ok: boolean; username: string }>('/auth/login', { username, password })
  return { username: response.username }
}

export async function logout(): Promise<void> {
  await apiPost<{ ok: boolean }>('/auth/logout')
}

export async function getCurrentUser(): Promise<AuthUser> {
  const response = await apiGet<{ ok: boolean; username: string }>('/auth/me')
  return { username: response.username }
}
