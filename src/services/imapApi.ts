import type { MailFolder } from '@/types'
import { ApiError, apiGet, apiPost } from './apiClient'

export type ImapErrorCode =
  | 'oauth_error'
  | 'imap_auth_error'
  | 'imap_error'
  | 'imap_timeout'
  | 'bad_request'
  | 'network_error'
  | 'internal_error'

export class ImapApiError extends Error {
  readonly code: ImapErrorCode
  readonly status?: number

  constructor(code: ImapErrorCode, message: string, status?: number) {
    super(message)
    this.name = 'ImapApiError'
    this.code = code
    this.status = status
  }
}

export interface AccountCredentials {
  email: string
}

export interface ImapAddress {
  name?: string
  email?: string
}

export interface ImapMessageSummary {
  id: string
  subject: string
  from: ImapAddress[]
  to: ImapAddress[]
  cc: ImapAddress[]
  receivedAt: string
  isRead: boolean
  hasAttachments: boolean
}

export interface ImapMessageDetail {
  id: string
  subject: string
  from: ImapAddress[]
  to: ImapAddress[]
  cc: ImapAddress[]
  receivedAt: string
  isRead: boolean
  contentType: 'text' | 'html'
  content: string
}

export interface ListMessagesResult {
  ok: boolean
  folder: MailFolder
  messages: ImapMessageSummary[]
  nextCursor?: string
}

export interface GetMessageResult {
  ok: boolean
  message: ImapMessageDetail
  body: {
    contentType: 'text' | 'html'
    content: string
  }
}

export async function listImapMessages(params: {
  credentials: AccountCredentials
  folder: MailFolder
  limit?: number
  cursor?: string
}): Promise<ListMessagesResult> {
  return postJSON<ListMessagesResult>('/mail/messages', {
    email: params.credentials.email,
    folder: params.folder,
    limit: params.limit,
    cursor: params.cursor,
  })
}

export async function getImapMessage(params: {
  credentials: AccountCredentials
  folder: MailFolder
  messageId: string
}): Promise<GetMessageResult> {
  return postJSON<GetMessageResult>('/mail/message', {
    email: params.credentials.email,
    folder: params.folder,
    messageId: params.messageId,
  })
}

export async function getImapHealth(): Promise<{ ok: boolean; service: string }> {
  return apiGet<{ ok: boolean; service: string }>('/health')
}

async function postJSON<T>(path: string, body: unknown): Promise<T> {
  try {
    return await apiPost<T>(path, body)
  } catch (error) {
    if (error instanceof ApiError) {
      throw new ImapApiError(error.code as ImapErrorCode, error.message, error.status)
    }
    throw error
  }
}
