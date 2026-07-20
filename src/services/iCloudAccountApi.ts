import { apiDelete, apiGet, apiPatch, apiPost } from './apiClient'

export interface ICloudAccount {
  email: string
  key: string
  group: string
  remark: string
  createdAt: string
  updatedAt: string
}

export interface ICloudGroup {
  id: number
  name: string
  sortOrder: number
  createdAt: string
  updatedAt: string
}

export interface ICloudImportResult {
  imported: number
  updated: number
  errors: string[]
}

export interface ICloudMailboxInfo {
  id: number
  address: string
  active: boolean
}

export interface ICloudLatestEmail {
  id: number
  to: string
  from: string
  subject: string
  text: string
  html: string
  received_at: string
  created_at: string
  verification_code: string
  mail_type: string
  invite_link: string
  process_status: string
}

export interface ICloudLatestMailResponse {
  ok: boolean
  mailbox?: ICloudMailboxInfo
  email?: ICloudLatestEmail
}

export async function listICloudAccounts(): Promise<ICloudAccount[]> {
  const response = await apiGet<{ ok: boolean; accounts: ICloudAccount[] }>('/icloud-accounts')
  return response.accounts
}

export async function importICloudAccounts(
  text: string,
  overwrite: boolean,
  group: string,
): Promise<ICloudImportResult> {
  return apiPost<ICloudImportResult>('/icloud-accounts/import', { text, overwrite, group })
}

export async function getLatestICloudMail(email: string): Promise<ICloudLatestMailResponse> {
  return apiPost<ICloudLatestMailResponse>('/icloud-accounts/latest', { email })
}

export async function updateICloudRemark(email: string, remark: string): Promise<ICloudAccount> {
  const response = await apiPatch<{ ok: boolean; account: ICloudAccount }>('/icloud-accounts/remark', {
    email,
    remark,
  })
  return response.account
}

export async function moveICloudAccounts(emails: string[], group: string): Promise<void> {
  await apiPost<{ ok: boolean }>('/icloud-accounts/move-group', { emails, group })
}

export async function deleteICloudAccount(email: string): Promise<void> {
  await apiDelete<{ ok: boolean }>(`/icloud-accounts/${encodeURIComponent(email)}`)
}

export async function listICloudGroups(): Promise<ICloudGroup[]> {
  const response = await apiGet<{ ok: boolean; groups: ICloudGroup[] }>('/icloud-groups')
  return response.groups
}

export async function createICloudGroup(name: string): Promise<ICloudGroup> {
  const response = await apiPost<{ ok: boolean; group: ICloudGroup }>('/icloud-groups', { name })
  return response.group
}

export async function reorderICloudGroups(ids: number[]): Promise<ICloudGroup[]> {
  const response = await apiPatch<{ ok: boolean; groups: ICloudGroup[] }>('/icloud-groups/order', { ids })
  return response.groups
}

export async function renameICloudGroup(id: number, name: string): Promise<ICloudGroup> {
  const response = await apiPatch<{ ok: boolean; group: ICloudGroup }>(`/icloud-groups/${id}`, { name })
  return response.group
}

export async function deleteICloudGroup(id: number): Promise<void> {
  await apiDelete<{ ok: boolean }>(`/icloud-groups/${id}`)
}