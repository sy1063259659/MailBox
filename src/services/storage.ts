import { deleteDB, openDB, type DBSchema, type IDBPDatabase, type IDBPObjectStore } from 'idb'
import type {
  MailBody,
  MailFolder,
  MailMessage,
  MessageFilter,
  StorageStats,
  SyncState,
} from '../types'

const DB_NAME = 'mailbox-cache'
const LEGACY_DB_NAME = 'mailbox-graph-manager'
const DB_VERSION = 4
const MAX_MESSAGE_BODY_CACHE = 2000

type MessageStorageKey = `${string}::${MailFolder}::${string}`
type MessageBodyStorageKey = `${string}::${string}`
type SyncStateStorageKey = `${string}::${MailFolder}`
type MessageIndexName = 'by-account' | 'by-folder' | 'by-account-folder' | 'by-received-at'

interface StoredMailMessage extends MailMessage {
  storageKey: MessageStorageKey
  accountFolderKey: [string, MailFolder]
}

interface StoredMailBody extends MailBody {
  storageKey: MessageBodyStorageKey
}

interface StoredSyncState extends SyncState {
  storageKey: SyncStateStorageKey
}

interface MailboxDatabase extends DBSchema {
  messages: {
    key: MessageStorageKey
    value: StoredMailMessage
    indexes: {
      'by-account': string
      'by-folder': MailFolder
      'by-account-folder': [string, MailFolder]
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
let legacyMigrationPromise: Promise<void> | undefined

const getDatabase = (): Promise<IDBPDatabase<MailboxDatabase>> => {
  databasePromise ??= openDB<MailboxDatabase>(DB_NAME, DB_VERSION, {
    upgrade(database, _oldVersion, _newVersion, transaction) {
      const rawDatabase = database as unknown as IDBDatabase
      if (rawDatabase.objectStoreNames.contains('accounts')) {
        rawDatabase.deleteObjectStore('accounts')
      }

      const messages = database.objectStoreNames.contains('messages')
        ? transaction.objectStore('messages')
        : database.createObjectStore('messages', {
            keyPath: 'storageKey',
          })
      ensureIndex(messages, 'by-account', 'accountEmail')
      ensureIndex(messages, 'by-folder', 'folder')
      ensureIndex(messages, 'by-account-folder', 'accountFolderKey')
      ensureIndex(messages, 'by-received-at', 'receivedAt')
      if (_oldVersion < 4) {
        void removeLegacyMessageSummaries(messages)
      }

      const messageBodies = database.objectStoreNames.contains('messageBodies')
        ? undefined
        : database.createObjectStore('messageBodies', {
            keyPath: 'storageKey',
          })
      messageBodies?.createIndex('by-account', 'accountEmail')

      const syncStates = database.objectStoreNames.contains('syncStates')
        ? undefined
        : database.createObjectStore('syncStates', {
            keyPath: 'storageKey',
          })
      syncStates?.createIndex('by-account', 'accountEmail')
    },
  })

  return databasePromise
}

const ensureIndex = (
  store: {
    indexNames: DOMStringList
    createIndex: (name: MessageIndexName, keyPath: string | string[]) => unknown
  },
  name: MessageIndexName,
  keyPath: string | string[],
): void => {
  if (!store.indexNames.contains(name)) {
    store.createIndex(name, keyPath)
  }
}

const ensureLegacyMigration = async (): Promise<void> => {
  legacyMigrationPromise ??= migrateLegacyDatabase()
  await legacyMigrationPromise
}

const migrateLegacyDatabase = async (): Promise<void> => {
  if (!(await databaseExists(LEGACY_DB_NAME))) {
    return
  }

  const legacyDatabase = await openDB<MailboxDatabase>(LEGACY_DB_NAME)
  const database = await getDatabase()

  try {
    const [messages, messageBodies, syncStates] = await Promise.all([
      legacyDatabase.objectStoreNames.contains('messages')
        ? legacyDatabase.getAll('messages')
        : Promise.resolve([]),
      legacyDatabase.objectStoreNames.contains('messageBodies')
        ? legacyDatabase.getAll('messageBodies')
        : Promise.resolve([]),
      legacyDatabase.objectStoreNames.contains('syncStates')
        ? legacyDatabase.getAll('syncStates')
        : Promise.resolve([]),
    ])

    const transaction = database.transaction(['messages', 'messageBodies', 'syncStates'], 'readwrite')
    await Promise.all([
      ...messages.map((message) => transaction.objectStore('messages').put(message)),
      ...messageBodies.map((body) => transaction.objectStore('messageBodies').put(body)),
      ...syncStates.map((syncState) => transaction.objectStore('syncStates').put(syncState)),
    ])
    await transaction.done
  } finally {
    legacyDatabase.close()
  }

  await deleteDB(LEGACY_DB_NAME)
}

const databaseExists = async (name: string): Promise<boolean> => {
  if (!('databases' in indexedDB)) {
    return false
  }
  const databases = await indexedDB.databases()
  return databases.some((database) => database.name === name)
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

const toStoredMessage = (message: MailMessage): StoredMailMessage => {
  const cleanMessage = withoutLegacyMessageSummary(message)
  return {
    ...cleanMessage,
    storageKey: createMessageKey(cleanMessage.accountEmail, cleanMessage.folder, cleanMessage.messageId),
    accountFolderKey: [cleanMessage.accountEmail, cleanMessage.folder],
  }
}

const fromStoredMessage = ({
  storageKey: _storageKey,
  accountFolderKey: _accountFolderKey,
  ...message
}: StoredMailMessage): MailMessage => withoutLegacyMessageSummary(message)

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

export const deleteLocalMailDataForAccount = async (email: string): Promise<void> => {
  await ensureLegacyMigration()
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
  await ensureLegacyMigration()
  const database = await getDatabase()
  const transaction = database.transaction('messages', 'readwrite')
  const store = transaction.objectStore('messages')

  await Promise.all(messages.map((message) => store.put(toStoredMessage(message))))
  await transaction.done
}

export const upsertMessage = async (message: MailMessage): Promise<void> => {
  await bulkUpsertMessages([message])
}

export const listMessages = async (
  accountEmail: string,
  folder?: MailFolder,
): Promise<MailMessage[]> => {
  await ensureLegacyMigration()
  const database = await getDatabase()
  const messages = await database.getAllFromIndex('messages', 'by-account', accountEmail)
  return messages
    .filter((message) => folder === undefined || message.folder === folder)
    .sort(sortByReceivedAtDesc)
    .map(fromStoredMessage)
}

export const filterMessages = async (filter: MessageFilter): Promise<MailMessage[]> => {
  await ensureLegacyMigration()
  const database = await getDatabase()
  const source = await getMessagesForFilter(database, filter)

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

const getMessagesForFilter = async (
  database: IDBPDatabase<MailboxDatabase>,
  filter: MessageFilter,
): Promise<StoredMailMessage[]> => {
  if (filter.accountEmail && filter.folder) {
    const messages = await database.getAllFromIndex('messages', 'by-account-folder', [filter.accountEmail, filter.folder])
    return messages.length > 0 ? messages : database.getAllFromIndex('messages', 'by-account', filter.accountEmail)
  }
  if (filter.accountEmail) {
    return database.getAllFromIndex('messages', 'by-account', filter.accountEmail)
  }
  if (filter.folder) {
    return database.getAllFromIndex('messages', 'by-folder', filter.folder)
  }
  return database.getAll('messages')
}

export const getMessageBody = async (
  accountEmail: string,
  messageId: string,
): Promise<MailBody | undefined> => {
  await ensureLegacyMigration()
  const database = await getDatabase()
  const body = await database.get('messageBodies', createMessageBodyKey(accountEmail, messageId))
  return body === undefined ? undefined : fromStoredBody(body)
}

export const saveMessageBody = async (body: MailBody): Promise<void> => {
  await ensureLegacyMigration()
  const database = await getDatabase()
  await database.put('messageBodies', toStoredBody(body))
  await pruneMessageBodyCache(database)
}

export const getSyncState = async (
  accountEmail: string,
  folder: MailFolder,
): Promise<SyncState | undefined> => {
  await ensureLegacyMigration()
  const database = await getDatabase()
  const syncState = await database.get('syncStates', createSyncStateKey(accountEmail, folder))
  return syncState === undefined ? undefined : fromStoredSyncState(syncState)
}

export const saveSyncState = async (syncState: SyncState): Promise<void> => {
  await ensureLegacyMigration()
  const database = await getDatabase()
  await database.put('syncStates', toStoredSyncState(syncState))
}

export const clearLocalMailData = async (): Promise<void> => {
  await ensureLegacyMigration()
  const database = await getDatabase()
  const transaction = database.transaction(['messages', 'messageBodies', 'syncStates'], 'readwrite')

  await Promise.all([
    transaction.objectStore('messages').clear(),
    transaction.objectStore('messageBodies').clear(),
    transaction.objectStore('syncStates').clear(),
  ])

  await transaction.done
}

export const getStats = async (): Promise<StorageStats> => {
  await ensureLegacyMigration()
  const database = await getDatabase()
  const [messageCount, messageBodyCount, syncStateCount, messages] = await Promise.all([
    database.count('messages'),
    database.count('messageBodies'),
    database.count('syncStates'),
    database.getAll('messages'),
  ])

  return {
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

const pruneMessageBodyCache = async (database: IDBPDatabase<MailboxDatabase>): Promise<void> => {
  const count = await database.count('messageBodies')
  if (count <= MAX_MESSAGE_BODY_CACHE) {
    return
  }

  const bodies = await database.getAll('messageBodies')
  const staleBodies = bodies
    .sort((left, right) => left.fetchedAt.localeCompare(right.fetchedAt))
    .slice(0, count - MAX_MESSAGE_BODY_CACHE)
  const transaction = database.transaction('messageBodies', 'readwrite')
  await Promise.all(staleBodies.map((body) => transaction.objectStore('messageBodies').delete(body.storageKey)))
  await transaction.done
}

type DeleteTransactionStores = ['messages', 'messageBodies', 'syncStates']

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
    message.from?.name,
    message.from?.email,
    ...message.to.flatMap((address) => [address.name, address.email]),
    ...(message.cc?.flatMap((address) => [address.name, address.email]) ?? []),
  ]

  return searchableValues.some(
    (value) => value !== undefined && value.toLocaleLowerCase().includes(query),
  )
}

const withoutLegacyMessageSummary = <T extends object>(message: T): T => {
  const { ['preview']: _legacySummary, ...cleanMessage } = message as T & { preview?: unknown }
  return cleanMessage as T
}

const removeLegacyMessageSummaries = async (
  store: IDBPObjectStore<MailboxDatabase, Array<'messages' | 'messageBodies' | 'syncStates'>, 'messages', 'versionchange'>,
): Promise<void> => {
  let cursor = await store.openCursor()
  while (cursor) {
    const value = withoutLegacyMessageSummary(cursor.value)
    if (value !== cursor.value) {
      await cursor.update(value)
    }
    cursor = await cursor.continue()
  }
}
