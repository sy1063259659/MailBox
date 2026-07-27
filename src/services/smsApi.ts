import { apiDelete, apiGet, apiPatch, apiPost } from './apiClient'

export type SMSMailboxType = 'icloud_hme'

export interface SMSAccount {
  id: number
  phone: string
  providerHost: string
  remark: string
  linkedMailboxType: SMSMailboxType | ''
  linkedMailboxEmail: string
  linkedMailboxEmails: string[]
  lastCheckedAt?: string
  lastError?: string
  createdAt: string
  updatedAt: string
  receiveUrlConfigured: boolean
}

export interface SMSMailboxReference {
  type: SMSMailboxType
  email: string
}

export interface SMSImportResult {
  imported: number
  updated: number
  errors: string[]
}

export interface SMSLatestResult {
  ok: boolean
  phone: string
  message: string
  code: string
  available: boolean
  checkedAt: string
}

export async function listSMSAccounts(): Promise<SMSAccount[]> {
  const response = await apiGet<{ ok: boolean; accounts: SMSAccount[] }>('/sms-accounts')
  return response.accounts.map((account) => ({
    ...account,
    linkedMailboxEmails: account.linkedMailboxEmails
      ?? (account.linkedMailboxType === 'icloud_hme' && account.linkedMailboxEmail ? [account.linkedMailboxEmail] : []),
  }))
}

export async function importSMSAccounts(text: string, overwrite: boolean): Promise<SMSImportResult> {
  return apiPost<SMSImportResult>('/sms-accounts/import', { text, overwrite })
}

export async function updateSMSRemark(phone: string, remark: string): Promise<SMSAccount> {
  const response = await apiPatch<{ ok: boolean; account: SMSAccount }>('/sms-accounts/remark', { phone, remark })
  return response.account
}

export async function bindSMSMailbox(
  phone: string,
  emails: string[],
): Promise<SMSAccount> {
  const response = await apiPatch<{ ok: boolean; account: SMSAccount }>('/sms-accounts/binding', {
    phone,
    emails,
  })
  return response.account
}

export async function listSMSMailboxes(): Promise<SMSMailboxReference[]> {
  const response = await apiGet<{ ok: boolean; mailboxes: SMSMailboxReference[] }>('/sms-accounts/mailboxes')
  return response.mailboxes
}

export async function getLatestSMS(phone: string): Promise<SMSLatestResult> {
  return apiPost<SMSLatestResult>('/sms-accounts/latest', { phone })
}

export async function deleteSMSAccount(phone: string): Promise<void> {
  await apiDelete<{ ok: boolean }>(`/sms-accounts/${encodeURIComponent(phone)}`)
}
