import { apiDelete, apiGet, apiPatch, apiPost, apiRequest } from './apiClient'

export interface ICloudHMEGroup {
  id: number
  name: string
  sortOrder: number
  createdAt: string
  updatedAt: string
}

export interface ICloudHMESourceAccount {
  id: number
  name: string
  appleIdEmail: string
  icloudEmail: string
  host: 'icloud.com' | 'icloud.com.cn'
  cookieConfigured: boolean
  appPasswordConfigured: boolean
  status: string
  statusReason?: string
  aliasTotal: number
  lastValidatedAt?: string
  createdAt: string
  updatedAt: string
}

export interface ICloudHMEAlias {
  email: string
  sourceAccountId: number
  sourceAccountName: string
  anonymousId: string
  label: string
  active: boolean
  group: string
  remark: string
  mailReady: boolean
  createdAt: string
  updatedAt: string
}

export interface ICloudHMEAddress {
  name?: string
  email?: string
}

export interface ICloudHMEMail {
  id: string
  subject: string
  from: ICloudHMEAddress[]
  to: ICloudHMEAddress[]
  cc: ICloudHMEAddress[]
  receivedAt: string
  isRead: boolean
  contentType: 'text/html' | 'text/plain'
  content: string
}

export async function listICloudHMESourceAccounts(): Promise<ICloudHMESourceAccount[]> {
  const response = await apiGet<{ ok: boolean; accounts: ICloudHMESourceAccount[] }>('/icloud-hme/source-accounts')
  return response.accounts
}

export async function createICloudHMESourceAccount(input: {
  name: string
  appleIdEmail: string
  icloudEmail: string
  host: string
}): Promise<ICloudHMESourceAccount> {
  const response = await apiPost<{ ok: boolean; account: ICloudHMESourceAccount }>('/icloud-hme/source-accounts', input)
  return response.account
}

export async function saveICloudHMECookies(id: number, cookies: string): Promise<void> {
  await apiPost(`/icloud-hme/source-accounts/${id}/cookies`, { cookies })
}

export async function loginICloudHMESource(id: number, password: string, otp = ''): Promise<void> {
  await apiPost(`/icloud-hme/source-accounts/${id}/login`, { password, otp })
}

export async function saveICloudHMEAppPassword(id: number, appPassword: string): Promise<void> {
  await apiRequest(`/icloud-hme/source-accounts/${id}/app-password`, {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ appPassword }),
  })
}

export async function validateICloudHMESource(id: number): Promise<void> {
  await apiPost(`/icloud-hme/source-accounts/${id}/validate`)
}

export async function deleteICloudHMESource(id: number): Promise<void> {
  await apiDelete(`/icloud-hme/source-accounts/${id}`)
}

export async function listICloudHMEAliases(): Promise<ICloudHMEAlias[]> {
  const response = await apiGet<{ ok: boolean; aliases: ICloudHMEAlias[] }>('/icloud-hme/aliases')
  return response.aliases
}

export async function createICloudHMEAlias(id: number, label: string, group: string): Promise<string> {
  const response = await apiPost<{ ok: boolean; email: string }>(`/icloud-hme/source-accounts/${id}/aliases`, { label, group })
  return response.email
}

export async function syncICloudHMEAliases(id: number): Promise<{ imported: number; updated: number; total: number }> {
  return apiPost(`/icloud-hme/source-accounts/${id}/aliases/sync`)
}

export async function updateICloudHMERemark(email: string, remark: string): Promise<ICloudHMEAlias> {
  const response = await apiPatch<{ ok: boolean; alias: ICloudHMEAlias }>('/icloud-hme/aliases/remark', { email, remark })
  return response.alias
}

export async function moveICloudHMEAliases(emails: string[], group: string): Promise<void> {
  await apiPost('/icloud-hme/aliases/move-group', { emails, group })
}

export async function deleteICloudHMEAlias(email: string): Promise<void> {
  await apiDelete(`/icloud-hme/aliases/${encodeURIComponent(email)}`)
}

export async function getLatestICloudHMEMail(email: string): Promise<ICloudHMEMail> {
  const response = await apiPost<{ ok: boolean; email: ICloudHMEMail }>('/icloud-hme/mail/latest', { email })
  return response.email
}

export async function listICloudHMEGroups(): Promise<ICloudHMEGroup[]> {
  const response = await apiGet<{ ok: boolean; groups: ICloudHMEGroup[] }>('/icloud-hme/groups')
  return response.groups
}

export async function createICloudHMEGroup(name: string): Promise<ICloudHMEGroup> {
  const response = await apiPost<{ ok: boolean; group: ICloudHMEGroup }>('/icloud-hme/groups', { name })
  return response.group
}

export async function reorderICloudHMEGroups(ids: number[]): Promise<ICloudHMEGroup[]> {
  const response = await apiPatch<{ ok: boolean; groups: ICloudHMEGroup[] }>('/icloud-hme/groups/order', { ids })
  return response.groups
}

export async function renameICloudHMEGroup(id: number, name: string): Promise<ICloudHMEGroup> {
  const response = await apiPatch<{ ok: boolean; group: ICloudHMEGroup }>(`/icloud-hme/groups/${id}`, { name })
  return response.group
}

export async function deleteICloudHMEGroup(id: number): Promise<void> {
  await apiDelete(`/icloud-hme/groups/${id}`)
}