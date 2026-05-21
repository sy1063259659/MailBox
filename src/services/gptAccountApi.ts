import type { GptAccount } from '@/types'
import { apiDelete, apiGet, apiPost } from './apiClient'

export interface ImportGptTokenPayload {
  mailAccountEmail: string
  tokenJson: string
}

export interface StartGptOAuthPayload {
  mailAccountEmail: string
}

export interface StartGptOAuthResult {
  loginId: string
  authUrl: string
}

export interface CompleteGptOAuthPayload {
  mailAccountEmail: string
  loginId: string
  callbackUrl: string
}

export interface RefreshGptAccountsResult {
  accounts: GptAccount[]
}

interface GptAccountsResponse {
  ok: boolean
  accounts?: GptAccount[]
  gptAccounts?: GptAccount[]
}

interface GptAccountResponse {
  ok: boolean
  account?: GptAccount
  gptAccount?: GptAccount
}

export async function listGptAccounts(): Promise<GptAccount[]> {
  const response = await apiGet<GptAccountsResponse>('/gpt-accounts')
  return response.accounts ?? response.gptAccounts ?? []
}

export async function importGptToken(payload: ImportGptTokenPayload): Promise<GptAccount> {
  const response = await apiPost<GptAccountResponse>('/gpt-accounts/import-token', payload)
  const account = response.account ?? response.gptAccount
  if (!account) {
    throw new Error('后端未返回 GPT/Codex 账号')
  }
  return account
}

export async function startGptOAuth(payload: StartGptOAuthPayload): Promise<StartGptOAuthResult> {
  const response = await apiPost<{ ok: boolean; loginId: string; authUrl: string }>('/gpt-accounts/oauth/start', payload)
  return {
    loginId: response.loginId,
    authUrl: response.authUrl,
  }
}

export async function completeGptOAuth(payload: CompleteGptOAuthPayload): Promise<GptAccount> {
  const response = await apiPost<GptAccountResponse>('/gpt-accounts/oauth/complete', payload)
  const account = response.account ?? response.gptAccount
  if (!account) {
    throw new Error('后端未返回 GPT/Codex 账号')
  }
  return account
}

export async function refreshGptAccount(mailAccountEmail: string): Promise<GptAccount> {
  const response = await apiPost<GptAccountResponse>(
    `/gpt-accounts/${encodeURIComponent(mailAccountEmail)}/refresh`,
  )
  const account = response.account ?? response.gptAccount
  if (!account) {
    throw new Error('后端未返回 GPT/Codex 账号')
  }
  return account
}

export async function refreshAllGptAccounts(): Promise<RefreshGptAccountsResult> {
  const response = await apiPost<GptAccountsResponse>('/gpt-accounts/refresh-all')
  return { accounts: response.accounts ?? response.gptAccounts ?? [] }
}

export async function unlinkGptAccount(mailAccountEmail: string): Promise<void> {
  await apiDelete<{ ok: boolean }>(`/gpt-accounts/${encodeURIComponent(mailAccountEmail)}`)
}
