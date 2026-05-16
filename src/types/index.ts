export type MailFolder = 'inbox' | 'junkemail'

export type AccountStatus =
  | 'idle'
  | 'syncing'
  | 'success'
  | 'error'
  | 'token_expired'
  | 'rate_limited'

export interface MailAccount {
  email: string
  password: string
  clientId: string
  refreshToken: string
  group: string
  remark: string
  displayName: string
  status: AccountStatus
  parentEmail?: string
  splitIndex?: number
  splitGeneratedAt?: string
  children?: MailAccount[]
  provider?: 'microsoft' | 'outlook' | 'office365' | 'imap' | 'unknown'
  createdAt: string
  updatedAt: string
  lastSyncAt?: string
  errorMessage?: string
  accessTokenExpiresAt?: string
  metadata?: Record<string, unknown>
}

export interface MailAddress {
  name?: string
  email: string
}

export interface MailAttachmentSummary {
  id: string
  name: string
  contentType?: string
  size?: number
  isInline?: boolean
}

export interface MailMessage {
  accountEmail: string
  folder: MailFolder
  messageId: string
  conversationId?: string
  internetMessageId?: string
  subject: string
  from?: MailAddress
  to: MailAddress[]
  cc?: MailAddress[]
  bcc?: MailAddress[]
  receivedAt: string
  sentAt?: string
  preview?: string
  isRead: boolean
  hasAttachments: boolean
  attachments?: MailAttachmentSummary[]
  importance?: 'low' | 'normal' | 'high'
  categories?: string[]
  webLink?: string
  createdAt: string
  updatedAt: string
  metadata?: Record<string, unknown>
}

export interface MailBody {
  accountEmail: string
  messageId: string
  contentType: 'text' | 'html'
  content: string
  fetchedAt: string
  updatedAt: string
  metadata?: Record<string, unknown>
}

export interface SyncState {
  accountEmail: string
  folder: MailFolder
  status: AccountStatus
  lastSyncedAt?: string
  nextLink?: string
  deltaLink?: string
  cursor?: string
  errorCode?: string
  errorMessage?: string
  retryAfterSeconds?: number
  messageCount: number
  updatedAt: string
}

export interface StorageStats {
  messageCount: number
  messageBodyCount: number
  syncStateCount: number
  messagesByFolder: Record<MailFolder, number>
}

export interface MessageFilter {
  accountEmail?: string
  folder?: MailFolder
  isRead?: boolean
  hasAttachments?: boolean
  query?: string
  limit?: number
}
