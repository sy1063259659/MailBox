import type { MailAccount } from '@/types'
import { apiDelete, apiGet, apiPatch, apiPost } from './apiClient'

export interface AccountImportResult {
  imported: number
  updated: number
  errors: string[]
}

export interface MailGroup {
  id: number
  name: string
  createdAt: string
  updatedAt: string
}

export async function listRemoteAccounts(): Promise<MailAccount[]> {
  const response = await apiGet<{ ok: boolean; accounts: MailAccount[] }>('/accounts')
  return response.accounts
}

export async function importRemoteAccounts(text: string, overwrite: boolean): Promise<AccountImportResult> {
  return apiPost<AccountImportResult>('/accounts/import', { text, overwrite })
}

export async function deleteRemoteAccount(email: string): Promise<void> {
  await apiDelete<{ ok: boolean }>(`/accounts/${encodeURIComponent(email)}`)
}

export async function moveRemoteAccountsToGroup(emails: string[], group: string): Promise<void> {
  await apiPost<{ ok: boolean }>('/accounts/move-group', { emails, group })
}

export async function exportRemoteAccounts(): Promise<string> {
  const response = await apiGet<{ ok: boolean; text: string }>('/accounts/export')
  return response.text
}

export async function listRemoteGroups(): Promise<MailGroup[]> {
  const response = await apiGet<{ ok: boolean; groups: MailGroup[] }>('/groups')
  return response.groups
}

export async function createRemoteGroup(name: string): Promise<MailGroup> {
  const response = await apiPost<{ ok: boolean; group: MailGroup }>('/groups', { name })
  return response.group
}

export async function renameRemoteGroup(id: number, name: string): Promise<MailGroup> {
  const response = await apiPatch<{ ok: boolean; group: MailGroup }>(`/groups/${id}`, { name })
  return response.group
}

export async function deleteRemoteGroup(id: number): Promise<void> {
  await apiDelete<{ ok: boolean }>(`/groups/${id}`)
}
