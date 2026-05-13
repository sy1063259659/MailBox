import { deleteDB, openDB, type DBSchema, type IDBPDatabase, type IDBPObjectStore } from 'idb'
import type {
  MailAccount,
  MailBody,
  MailFolder,
  MailMessage,
  MessageFilter,
  StorageExport,
  StorageStats,
  SyncState,
} from '../types'

const DB_NAME = 'mailbox-graph-manager'
const DB_VERSION = 1

type MessageStorageKey = `${string}::${MailFolder}::${string}`
type MessageBodyStorageKey = `${string}::${string}`
type SyncStateStorageKey = `${string}::${MailFolder}`

interface StoredMailMessage extends MailMessage {
  storageKey: MessageStorageKey
}

interface StoredMailBody extends MailBody {
  storageKey: MessageBodyStorageKey
}

interface StoredSyncState extends SyncState {
  storageKey: SyncStateStorageKey
}

interface MailboxDatabase extends DBSchema {
  accounts: {
    key: string
    value: MailAccount
    indexes: {
      'by-status': string
    }
  }
  messages: {
    key: MessageStorageKey
    value: StoredMailMessage
    indexes: {
      'by-account': string
      'by-folder': MailFolder
      'by-received-at': string
    }
  }
  messageBodies: {
    key: MessageBodyStorageKey
    value: StoredMailBody
    indexes: {
      'by-account': string
    }
  }
  syncStates: {
    key: SyncStateStorageKey
    value: StoredSyncState
    indexes: {
      'by-account': string
    }
  }
}

let databasePromise: Promise<IDBPDatabase<MailboxDatabase>> | undefined

const getDatabase = (): Promise<IDBPDatabase<MailboxDatabase>> => {
  databasePromise ??= openDB<MailboxDatabase>(DB_NAME, DB_VERSION, {
    upgrade(database) {
      const accounts = database.createObjectStore('accounts', { keyPath: 'email' })
      accounts.createIndex('by-status', 'status')

      const messages = database.createObjectStore('messages', {
        keyPath: 'storageKey',
      })
      messages.createIndex('by-account', 'accountEmail')
      messages.createIndex('by-folder', 'folder')
      messages.createIndex('by-received-at', 'receivedAt')

      const messageBodies = database.createObjectStore('messageBodies', {
        keyPath: 'storageKey',
      })
      messageBodies.createIndex('by-account', 'accountEmail')

      const syncStates = database.createObjectStore('syncStates', {
        keyPath: 'storageKey',
      })
      syncStates.createIndex('by-account', 'accountEmail')
    },
  })

  return databasePromise
}

export const createMessageKey = (
  accountEmail: string,
  folder: MailFolder,
  messageId: string,
): MessageStorageKey => `${accountEmail}::${folder}::${messageId}`

export const createMessageBodyKey = (
  accountEmail: string,
  messageId: string,
): MessageBodyStorageKey => `${accountEmail}::${messageId}`

export const createSyncStateKey = (
  accountEmail: string,
  folder: MailFolder,
): SyncStateStorageKey => `${accountEmail}::${folder}`

const toStoredMessage = (message: MailMessage): StoredMailMessage => ({
  ...message,
  storageKey: createMessageKey(message.accountEmail, message.folder, message.messageId),
})

const fromStoredMessage = ({ storageKey: _storageKey, ...message }: StoredMailMessage): MailMessage =>
  message

const toStoredBody = (body: MailBody): StoredMailBody => ({
  ...body,
  storageKey: createMessageBodyKey(body.accountEmail, body.messageId),
})

const fromStoredBody = ({ storageKey: _storageKey, ...body }: StoredMailBody): MailBody => body

const toStoredSyncState = (syncState: SyncState): StoredSyncState => ({
  ...syncState,
  storageKey: createSyncStateKey(syncState.accountEmail, syncState.folder),
})

const fromStoredSyncState = ({
  storageKey: _storageKey,
  ...syncState
}: StoredSyncState): SyncState => syncState

const hasMessagePrefix = (message: StoredMailMessage, accountEmail: string): boolean =>
  message.storageKey.startsWith(`${accountEmail}::`)

const hasBodyPrefix = (body: StoredMailBody, accountEmail: string): boolean =>
  body.storageKey.startsWith(`${accountEmail}::`)

const hasSyncStatePrefix = (syncState: StoredSyncState, accountEmail: string): boolean =>
  syncState.storageKey.startsWith(`${accountEmail}::`)

export const upsertAccount = async (account: MailAccount): Promise<void> => {
  const database = await getDatabase()
  await database.put('accounts', account)
}

export const listAccounts = async (): Promise<MailAccount[]> => {
  const database = await getDatabase()
  return database.getAll('accounts')
}

export const getAccount = async (email: string): Promise<MailAccount | undefined> => {
  const database = await getDatabase()
  return database.get('accounts', email)
}

export const deleteAccount = async (email: string): Promise<void> => {
  const database = await getDatabase()
  const transaction = database.transaction(
    ['accounts', 'messages', 'messageBodies', 'syncStates'],
    'readwrite',
  )

  await Promise.all([
    transaction.objectStore('accounts').delete(email),
    deleteMessagesForAccount(transaction.objectStore('messages'), email),
    deleteMessageBodiesForAccount(transaction.objectStore('messageBodies'), email),
    deleteSyncStatesForAccount(transaction.objectStore('syncStates'), email),
  ])

  await transaction.done
}

export const deleteLocalMailDataForAccount = async (email: string): Promise<void> => {
  const database = await getDatabase()
  const transaction = database.transaction(
    ['messages', 'messageBodies', 'syncStates'],
    'readwrite',
  )

  await Promise.all([
    deleteMessagesForAccount(transaction.objectStore('messages'), email),
    deleteMessageBodiesForAccount(transaction.objectStore('messageBodies'), email),
    deleteSyncStatesForAccount(transaction.objectStore('syncStates'), email),
  ])

  await transaction.done
}

export const bulkUpsertMessages = async (messages: MailMessage[]): Promise<void> => {
  const database = await getDatabase()
  const transaction = database.transaction('messages', 'readwrite')
  const store = transaction.objectStore('messages')

  await Promise.all(messages.map((message) => store.put(toStoredMessage(message))))
  await transaction.done
}

export const listMessages = async (
  accountEmail: string,
  folder?: MailFolder,
): Promise<MailMessage[]> => {
  const database = await getDatabase()
  const messages = await database.getAllFromIndex('messages', 'by-account', accountEmail)
  return messages
    .filter((message) => folder === undefined || message.folder === folder)
    .sort(sortByReceivedAtDesc)
    .map(fromStoredMessage)
}

export const filterMessages = async (filter: MessageFilter): Promise<MailMessage[]> => {
  const database = await getDatabase()
  const source = filter.accountEmail
    ? await database.getAllFromIndex('messages', 'by-account', filter.accountEmail)
    : await database.getAll('messages')

  const query = filter.query?.trim().toLocaleLowerCase()
  const filtered = source.filter((message) => {
    if (filter.folder !== undefined && message.folder !== filter.folder) {
      return false
    }

    if (filter.isRead !== undefined && message.isRead !== filter.isRead) {
      return false
    }

    if (filter.hasAttachments !== undefined && message.hasAttachments !== filter.hasAttachments) {
      return false
    }

    if (query !== undefined && query.length > 0 && !matchesMessageQuery(message, query)) {
      return false
    }

    return true
  })

  const sorted = filtered.sort(sortByReceivedAtDesc)
  const limited = filter.limit === undefined ? sorted : sorted.slice(0, filter.limit)
  return limited.map(fromStoredMessage)
}

export const getMessageBody = async (
  accountEmail: string,
  messageId: string,
): Promise<MailBody | undefined> => {
  const database = await getDatabase()
  const body = await database.get('messageBodies', createMessageBodyKey(accountEmail, messageId))
  return body === undefined ? undefined : fromStoredBody(body)
}

export const saveMessageBody = async (body: MailBody): Promise<void> => {
  const database = await getDatabase()
  await database.put('messageBodies', toStoredBody(body))
}

export const getSyncState = async (
  accountEmail: string,
  folder: MailFolder,
): Promise<SyncState | undefined> => {
  const database = await getDatabase()
  const syncState = await database.get('syncStates', createSyncStateKey(accountEmail, folder))
  return syncState === undefined ? undefined : fromStoredSyncState(syncState)
}

export const saveSyncState = async (syncState: SyncState): Promise<void> => {
  const database = await getDatabase()
  await database.put('syncStates', toStoredSyncState(syncState))
}

export const clearAll = async (): Promise<void> => {
  const database = await getDatabase()
  const transaction = database.transaction(
    ['accounts', 'messages', 'messageBodies', 'syncStates'],
    'readwrite',
  )

  await Promise.all([
    transaction.objectStore('accounts').clear(),
    transaction.objectStore('messages').clear(),
    transaction.objectStore('messageBodies').clear(),
    transaction.objectStore('syncStates').clear(),
  ])

  await transaction.done
}

export const clearLocalMailData = async (): Promise<void> => {
  const database = await getDatabase()
  const transaction = database.transaction(['messages', 'messageBodies', 'syncStates'], 'readwrite')

  await Promise.all([
    transaction.objectStore('messages').clear(),
    transaction.objectStore('messageBodies').clear(),
    transaction.objectStore('syncStates').clear(),
  ])

  await transaction.done
}

export const exportAll = async (): Promise<StorageExport> => {
  const database = await getDatabase()
  const [accounts, messages, messageBodies, syncStates] = await Promise.all([
    database.getAll('accounts'),
    database.getAll('messages'),
    database.getAll('messageBodies'),
    database.getAll('syncStates'),
  ])

  return {
    accounts,
    messages: messages.map(fromStoredMessage),
    messageBodies: messageBodies.map(fromStoredBody),
    syncStates: syncStates.map(fromStoredSyncState),
    exportedAt: new Date().toISOString(),
  }
}

export const getStats = async (): Promise<StorageStats> => {
  const database = await getDatabase()
  const [accountCount, messageCount, messageBodyCount, syncStateCount, messages] = await Promise.all([
    database.count('accounts'),
    database.count('messages'),
    database.count('messageBodies'),
    database.count('syncStates'),
    database.getAll('messages'),
  ])

  return {
    accountCount,
    messageCount,
    messageBodyCount,
    syncStateCount,
    messagesByFolder: messages.reduce<Record<MailFolder, number>>(
      (counts, message) => ({
        ...counts,
        [message.folder]: counts[message.folder] + 1,
      }),
      { inbox: 0, junkemail: 0 },
    ),
  }
}

export const deleteStorageDatabase = async (): Promise<void> => {
  databasePromise = undefined
  await deleteDB(DB_NAME)
}

type DeleteTransactionStores = ['accounts', 'messages', 'messageBodies', 'syncStates']

const deleteMessagesForAccount = async (
  store: IDBPObjectStore<MailboxDatabase, DeleteTransactionStores, 'messages', 'readwrite'>,
  accountEmail: string,
): Promise<void> => {
  let cursor = await store.openCursor()

  while (cursor !== null) {
    if (hasMessagePrefix(cursor.value, accountEmail)) {
      await cursor.delete()
    }

    cursor = await cursor.continue()
  }
}

const deleteMessageBodiesForAccount = async (
  store: IDBPObjectStore<MailboxDatabase, DeleteTransactionStores, 'messageBodies', 'readwrite'>,
  accountEmail: string,
): Promise<void> => {
  let cursor = await store.openCursor()

  while (cursor !== null) {
    if (hasBodyPrefix(cursor.value, accountEmail)) {
      await cursor.delete()
    }

    cursor = await cursor.continue()
  }
}

const deleteSyncStatesForAccount = async (
  store: IDBPObjectStore<MailboxDatabase, DeleteTransactionStores, 'syncStates', 'readwrite'>,
  accountEmail: string,
): Promise<void> => {
  let cursor = await store.openCursor()

  while (cursor !== null) {
    if (hasSyncStatePrefix(cursor.value, accountEmail)) {
      await cursor.delete()
    }

    cursor = await cursor.continue()
  }
}

const sortByReceivedAtDesc = (left: StoredMailMessage, right: StoredMailMessage): number =>
  right.receivedAt.localeCompare(left.receivedAt)

const matchesMessageQuery = (message: StoredMailMessage, query: string): boolean => {
  const searchableValues = [
    message.subject,
    message.preview,
    message.from?.name,
    message.from?.email,
    ...message.to.flatMap((address) => [address.name, address.email]),
    ...(message.cc?.flatMap((address) => [address.name, address.email]) ?? []),
  ]

  return searchableValues.some(
    (value) => value !== undefined && value.toLocaleLowerCase().includes(query),
  )
}
