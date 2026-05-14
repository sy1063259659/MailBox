import { defineStore } from 'pinia'
import type {
  MailAccount,
  MailAddress,
  MailBody,
  MailFolder,
  MailMessage,
  MessageFilter,
  SyncState,
} from '@/types'
import {
  ImapApiError,
  getImapMessage,
  listImapMessages,
  type ImapAddress,
  type ImapMessageDetail,
  type ImapMessageSummary,
} from '@/services/imapApi'
import {
  bulkUpsertMessages,
  filterMessages,
  getMessageBody,
  getSyncState,
  saveMessageBody,
  saveSyncState,
} from '@/services/storage'
import { useAccountStore } from './account'

const DEFAULT_PAGE_SIZE = 50
const MAIL_FOLDERS: MailFolder[] = ['inbox', 'junkemail']
const BODY_CACHE_VERSION = 2
const DEFAULT_BATCH_CONCURRENCY = 3

type SyncKey = `${string}::${MailFolder}`

export interface MailStoreFilter {
  accountEmail: string
  group: string
  folder: MailFolder
  query: string
  isRead: boolean | undefined
  hasAttachments: boolean | undefined
}

export interface AccountSyncResult {
  accountEmail: string
  folder: MailFolder
  synced: number
  nextLink?: string
  error?: string
}

export interface BatchSyncResult extends AccountSyncResult {
  status: 'success' | 'failed'
}

interface MailState {
  messages: MailMessage[]
  selectedMessage: MailMessage | undefined
  selectedBody: MailBody | undefined
  filter: MailStoreFilter
  nextLinks: Partial<Record<SyncKey, string>>
  syncingAccounts: Record<string, boolean>
  loading: boolean
  bodyLoading: boolean
  errorMessage: string | undefined
  syncErrors: Record<string, string>
  viewingAccountEmail: string
  batchSyncRunning: boolean
  batchSyncTotal: number
  batchSyncDone: number
  batchSyncSuccess: number
  batchSyncFailed: number
  batchSyncResults: BatchSyncResult[]
}

const createSyncKey = (accountEmail: string, folder: MailFolder): SyncKey =>
  `${accountEmail}::${folder}`

const toMailAddress = (address: ImapAddress): MailAddress => ({
  name: address.name,
  email: address.email ?? '',
})

const toAddressList = (addresses: ImapAddress[] | null | undefined): MailAddress[] =>
  (addresses ?? []).map(toMailAddress)

const getErrorMessage = (error: unknown): string =>
  error instanceof Error ? error.message : '未知错误'

const imapSummaryToMailMessage = (
  accountEmail: string,
  folder: MailFolder,
  summary: ImapMessageSummary,
  existingMessage: MailMessage | undefined,
): MailMessage => {
  const now = new Date().toISOString()

  return {
    accountEmail,
    folder,
    messageId: summary.id,
    subject: summary.subject,
    from: summary.from?.[0] ? toMailAddress(summary.from[0]) : undefined,
    to: toAddressList(summary.to),
    cc: toAddressList(summary.cc),
    receivedAt: summary.receivedAt || now,
    preview: sanitizePreview(summary.preview),
    isRead: summary.isRead,
    hasAttachments: summary.hasAttachments,
    createdAt: existingMessage?.createdAt ?? now,
    updatedAt: now,
  }
}

const imapDetailToMailBody = (
  accountEmail: string,
  message: ImapMessageDetail,
): MailBody => {
  const now = new Date().toISOString()

  return {
    accountEmail,
    messageId: message.id,
    contentType: message.contentType,
    content: message.content,
    fetchedAt: now,
    updatedAt: now,
    metadata: { parserVersion: BODY_CACHE_VERSION },
  }
}

const getAccountEmailsForFilter = (
  accounts: MailAccount[],
  filter: Pick<MailStoreFilter, 'accountEmail' | 'group'>,
): string[] => {
  if (filter.accountEmail) {
    return [filter.accountEmail]
  }

  return accounts
    .filter((account) => !filter.group || account.group === filter.group)
    .map((account) => account.email)
}

const sanitizePreview = (value: string | undefined): string => {
  if (!value) {
    return ''
  }

  let text = decodeQuotedPrintable(value)
  text = text.replace(/<\/?[^>]+>/g, ' ')
  text = text.replace(/&nbsp;/gi, ' ').replace(/&amp;/gi, '&')
  text = text.replace(/\s+/g, ' ').trim()

  const maxLength = 180
  if (text.length > maxLength) {
    return `${text.slice(0, maxLength).trimEnd()}…`
  }

  return text
}

const decodeQuotedPrintable = (value: string): string => {
  return value
    .replace(/=\r?\n/g, '')
    .replace(/=([A-Fa-f0-9]{2})/g, (_match, hex: string) =>
      String.fromCharCode(Number.parseInt(hex, 16)),
    )
}

export const useMailStore = defineStore('mail', {
  state: (): MailState => ({
    messages: [],
    selectedMessage: undefined,
    selectedBody: undefined,
    filter: {
      accountEmail: '',
      group: '',
      folder: 'inbox',
      query: '',
      isRead: undefined,
      hasAttachments: undefined,
    },
    nextLinks: {},
    syncingAccounts: {},
    loading: false,
    bodyLoading: false,
    errorMessage: undefined,
    syncErrors: {},
    viewingAccountEmail: '',
    batchSyncRunning: false,
    batchSyncTotal: 0,
    batchSyncDone: 0,
    batchSyncSuccess: 0,
    batchSyncFailed: 0,
    batchSyncResults: [],
  }),

  getters: {
    hasMore: (state): boolean => {
      const accountEmail = state.filter.accountEmail

      if (!accountEmail) {
        return false
      }

      return state.nextLinks[createSyncKey(accountEmail, state.filter.folder)] !== undefined
    },
  },

  actions: {
    async loadMessages(): Promise<void> {
      this.loading = true
      this.errorMessage = undefined

      try {
        const accountEmails = getAccountEmailsForFilter(useAccountStore().accounts, this.filter)
        const messageFilter: MessageFilter = {
          folder: this.filter.folder,
          query: this.filter.query,
          isRead: this.filter.isRead,
          hasAttachments: this.filter.hasAttachments,
        }

        if (this.filter.accountEmail) {
          this.messages = (await filterMessages({
            ...messageFilter,
            accountEmail: this.filter.accountEmail,
          })).map(sanitizeStoredMessage)
          return
        }

        if (this.filter.group) {
          const messageLists = await Promise.all(
            accountEmails.map((accountEmail) =>
              filterMessages({
                ...messageFilter,
                accountEmail,
              }),
            ),
          )
          this.messages = messageLists.flat().map(sanitizeStoredMessage).sort(sortByReceivedAtDesc)
          return
        }

        this.messages = (await filterMessages(messageFilter)).map(sanitizeStoredMessage)
      } finally {
        this.loading = false
      }
    },

    setFilter(filter: Partial<MailStoreFilter>): void {
      this.filter = { ...this.filter, ...filter }
      if (
        Object.hasOwn(filter, 'accountEmail')
        || Object.hasOwn(filter, 'folder')
        || Object.hasOwn(filter, 'query')
      ) {
        this.selectedMessage = undefined
        this.selectedBody = undefined
      }
    },

    async viewInbox(accountEmail: string): Promise<AccountSyncResult | undefined> {
      this.setFilter({
        accountEmail,
        folder: 'inbox',
        group: '',
        query: '',
        isRead: undefined,
        hasAttachments: undefined,
      })
      this.viewingAccountEmail = accountEmail

      const result = await this.syncAccountFolder(accountEmail, 'inbox')
      await this.loadMessages()
      if (this.messages.length > 0 && (!this.selectedMessage || this.selectedMessage.accountEmail !== accountEmail)) {
        await this.selectMessage(this.messages[0])
      }

      return result
    },

    async syncAccountFolder(
      accountEmail: string,
      folder?: MailFolder,
      nextLink?: string,
    ): Promise<AccountSyncResult> {
      const targetFolder = folder ?? this.filter.folder
      const account = useAccountStore().accounts.find((item) => item.email === accountEmail)

      if (!account) {
        return {
          accountEmail,
          folder: targetFolder,
          synced: 0,
          error: '账号不存在',
        }
      }

      const syncKey = createSyncKey(accountEmail, targetFolder)
      this.syncingAccounts[accountEmail] = true
      delete this.syncErrors[accountEmail]

      try {
        const listResult = await listImapMessages({
          credentials: {
            email: account.email,
          },
          folder: targetFolder,
          cursor: nextLink,
          limit: DEFAULT_PAGE_SIZE,
        })
        const existingMessages = await filterMessages({ accountEmail, folder: targetFolder })
        const existingById = new Map(existingMessages.map((message) => [message.messageId, message]))
        const messages = (listResult.messages ?? []).map((message) =>
          imapSummaryToMailMessage(accountEmail, targetFolder, message, existingById.get(message.id)),
        )
        const now = new Date().toISOString()

        await bulkUpsertMessages(messages)

        const nextSyncState: SyncState = {
          accountEmail,
          folder: targetFolder,
          status: 'success',
          lastSyncedAt: now,
          nextLink: listResult.nextCursor,
          cursor: listResult.nextCursor,
          messageCount: existingMessages.length + messages.length,
          updatedAt: now,
        }
        await saveSyncState(nextSyncState)

        if (listResult.nextCursor) {
          this.nextLinks[syncKey] = listResult.nextCursor
        } else {
          delete this.nextLinks[syncKey]
        }

        await useAccountStore().loadAccounts()
        await this.loadMessages()

        return {
          accountEmail,
          folder: targetFolder,
          synced: messages.length,
          nextLink: listResult.nextCursor,
        }
      } catch (error) {
        const errorMessage = getErrorMessage(error)
        await saveSyncState({
          accountEmail,
          folder: targetFolder,
          status: error instanceof ImapApiError && ['oauth_error', 'imap_auth_error'].includes(error.code)
            ? 'token_expired'
            : 'error',
          errorMessage,
          messageCount: 0,
          updatedAt: new Date().toISOString(),
        })

        this.syncErrors[accountEmail] = errorMessage
        await useAccountStore().loadAccounts()

        return {
          accountEmail,
          folder: targetFolder,
          synced: 0,
          error: errorMessage,
        }
      } finally {
        this.syncingAccounts[accountEmail] = false
      }
    },

    async syncSelectedAccounts(folders: MailFolder[] = MAIL_FOLDERS): Promise<AccountSyncResult[]> {
      const accountEmails = getAccountEmailsForFilter(useAccountStore().accounts, this.filter)
      const results: AccountSyncResult[] = []

      for (const accountEmail of accountEmails) {
        for (const folder of folders) {
          const result = await this.syncAccountFolder(accountEmail, folder, undefined)
          results.push(result)
        }
      }

      return results
    },

    async syncAccountsBatch(
      accountEmails: string[],
      folder: MailFolder = 'inbox',
      concurrency = DEFAULT_BATCH_CONCURRENCY,
    ): Promise<BatchSyncResult[]> {
      const uniqueEmails = Array.from(new Set(accountEmails.filter(Boolean)))
      this.batchSyncRunning = true
      this.batchSyncTotal = uniqueEmails.length
      this.batchSyncDone = 0
      this.batchSyncSuccess = 0
      this.batchSyncFailed = 0
      this.batchSyncResults = []

      if (uniqueEmails.length === 0) {
        this.batchSyncRunning = false
        return []
      }

      let nextIndex = 0
      const workerCount = Math.min(Math.max(1, concurrency), uniqueEmails.length)

      const runNext = async (): Promise<void> => {
        while (nextIndex < uniqueEmails.length) {
          const accountEmail = uniqueEmails[nextIndex]
          nextIndex += 1

          const result = await this.syncAccountFolder(accountEmail, folder)
          const batchResult: BatchSyncResult = {
            ...result,
            status: result.error ? 'failed' : 'success',
          }
          this.batchSyncResults.push(batchResult)
          this.batchSyncDone += 1
          if (batchResult.status === 'failed') {
            this.batchSyncFailed += 1
          } else {
            this.batchSyncSuccess += 1
          }
        }
      }

      try {
        await Promise.all(Array.from({ length: workerCount }, runNext))
        await useAccountStore().refreshStats()
        return this.batchSyncResults
      } finally {
        this.batchSyncRunning = false
      }
    },

    async loadMore(): Promise<AccountSyncResult | undefined> {
      if (!this.filter.accountEmail) {
        this.errorMessage = '请选择单个账号后再加载更多'
        return undefined
      }

      const nextLink = this.nextLinks[createSyncKey(this.filter.accountEmail, this.filter.folder)]
        ?? (await getSyncState(this.filter.accountEmail, this.filter.folder))?.cursor

      if (!nextLink) {
        return undefined
      }

      return this.syncAccountFolder(this.filter.accountEmail, this.filter.folder, nextLink)
    },

    async selectMessage(message: MailMessage): Promise<void> {
      this.selectedMessage = message
      this.viewingAccountEmail = message.accountEmail
      await this.loadMessageBody(message)
    },

    async loadMessageBody(message?: MailMessage): Promise<void> {
      const targetMessage = message ?? this.selectedMessage

      if (!targetMessage) {
        this.errorMessage = '请选择邮件'
        return
      }

      this.bodyLoading = true
      this.errorMessage = undefined

      try {
        const cachedBody = await getMessageBody(targetMessage.accountEmail, targetMessage.messageId)

        if (cachedBody?.metadata?.parserVersion === BODY_CACHE_VERSION) {
          this.selectedBody = cachedBody
          return
        }

        const detailResult = await getImapMessage({
          credentials: {
            email: targetMessage.accountEmail,
          },
          folder: targetMessage.folder,
          messageId: targetMessage.messageId,
        })

        const body = imapDetailToMailBody(targetMessage.accountEmail, {
          ...detailResult.message,
          contentType: detailResult.body.contentType,
          content: detailResult.body.content,
        })
        await saveMessageBody(body)

        this.selectedBody = body
        await useAccountStore().loadAccounts()
      } catch (error) {
        this.errorMessage = getErrorMessage(error)
        await useAccountStore().loadAccounts()
      } finally {
        this.bodyLoading = false
      }
    },
  },
})

function sortByReceivedAtDesc(left: MailMessage, right: MailMessage): number {
  return right.receivedAt.localeCompare(left.receivedAt)
}

function sanitizeStoredMessage(message: MailMessage): MailMessage {
  return {
    ...message,
    preview: sanitizePreview(message.preview),
  }
}
