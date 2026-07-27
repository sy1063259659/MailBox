import { apiDelete, apiGet, apiPatch, apiPost } from './apiClient'

export type SMSMailboxType = 'icloud_hme'

export interface SMSMailboxBinding {
  email: string
  boundAt: string
}

export interface SMSAccount {
  id: number
  phone: string
  providerHost: string
  remark: string
  status: 'active' | 'invalid'
  invalidAt?: string
  linkedMailboxType: SMSMailboxType | ''
  linkedMailboxEmail: string
  linkedMailboxEmails: string[]
  linkedMailboxes: SMSMailboxBinding[]
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

function normalizeSMSAccount(account: SMSAccount): SMSAccount {
  const linkedMailboxEmails = account.linkedMailboxEmails
    ?? (account.linkedMailboxType === 'icloud_hme' && account.linkedMailboxEmail ? [account.linkedMailboxEmail] : [])
  return {
    ...account,
    status: account.status ?? 'active',
    linkedMailboxEmails,
    linkedMailboxes: account.linkedMailboxes ?? linkedMailboxEmails.map((email) => ({
      email,
      boundAt: account.lastCheckedAt ?? account.updatedAt,
    })),
  }
}

export async function listSMSAccounts(): Promise<SMSAccount[]> {
  const response = await apiGet<{ ok: boolean; accounts: SMSAccount[] }>('/sms-accounts')
  return response.accounts.map(normalizeSMSAccount)
}

export async function importSMSAccounts(text: string, overwrite: boolean): Promise<SMSImportResult> {
  return apiPost<SMSImportResult>('/sms-accounts/import', { text, overwrite })
}

export async function updateSMSRemark(phone: string, remark: string): Promise<SMSAccount> {
  const response = await apiPatch<{ ok: boolean; account: SMSAccount }>('/sms-accounts/remark', { phone, remark })
  return normalizeSMSAccount(response.account)
}

export async function bindSMSMailbox(
  phone: string,
  emails: string[],
): Promise<SMSAccount> {
  const response = await apiPatch<{ ok: boolean; account: SMSAccount }>('/sms-accounts/binding', {
    phone,
    emails,
  })
  return normalizeSMSAccount(response.account)
}

export async function listSMSMailboxes(): Promise<SMSMailboxReference[]> {
  const response = await apiGet<{ ok: boolean; mailboxes: SMSMailboxReference[] }>('/sms-accounts/mailboxes')
  return response.mailboxes
}

export async function updateSMSStatus(phone: string, status: SMSAccount['status']): Promise<SMSAccount> {
  const response = await apiPatch<{ ok: boolean; account: SMSAccount }>('/sms-accounts/status', { phone, status })
  return normalizeSMSAccount(response.account)
}

export async function assignSMSMailbox(email: string, phone: string): Promise<SMSAccount[]> {
  const response = await apiPatch<{ ok: boolean; accounts: SMSAccount[] }>('/sms-accounts/mailbox-binding', {
    email,
    phone,
  })
  return response.accounts.map(normalizeSMSAccount)
}

export async function getLatestSMS(phone: string): Promise<SMSLatestResult> {
  return apiPost<SMSLatestResult>('/sms-accounts/latest', { phone })
}

export async function deleteSMSAccount(phone: string): Promise<void> {
  await apiDelete<{ ok: boolean }>(`/sms-accounts/${encodeURIComponent(phone)}`)
}
