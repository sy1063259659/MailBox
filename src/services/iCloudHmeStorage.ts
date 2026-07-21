import { openDB, type DBSchema, type IDBPDatabase } from 'idb'
import type { ICloudHMEMail, ICloudHMEMailSummary } from './iCloudHmeApi'

const DB_NAME = 'mailbox-icloud-hme-cache'
const DB_VERSION = 1
const RETENTION_MS = 7 * 24 * 60 * 60 * 1000
const MAX_PER_ALIAS = 20
const MAX_SUMMARIES = 2_000
const MAX_BODIES = 300

interface CachedSummary extends ICloudHMEMailSummary {
  storageKey: string
  aliasEmail: string
  cachedAt: string
}

interface CachedBody {
  storageKey: string
  aliasEmail: string
  message: ICloudHMEMail
  verificationCode?: string
  cachedAt: string
  lastAccessAt: string
}

interface ICloudHMECacheDB extends DBSchema {
  summaries: {
    key: string
    value: CachedSummary
    indexes: {
      'by-alias': string
      'by-cached-at': string
    }
  }
  bodies: {
    key: string
    value: CachedBody
    indexes: {
      'by-alias': string
      'by-last-access': string
    }
  }
}

let databasePromise: Promise<IDBPDatabase<ICloudHMECacheDB>> | undefined

function database(): Promise<IDBPDatabase<ICloudHMECacheDB>> {
  databasePromise ??= openDB<ICloudHMECacheDB>(DB_NAME, DB_VERSION, {
    upgrade(db) {
      const summaries = db.createObjectStore('summaries', { keyPath: 'storageKey' })
      summaries.createIndex('by-alias', 'aliasEmail')
      summaries.createIndex('by-cached-at', 'cachedAt')
      const bodies = db.createObjectStore('bodies', { keyPath: 'storageKey' })
      bodies.createIndex('by-alias', 'aliasEmail')
      bodies.createIndex('by-last-access', 'lastAccessAt')
    },
  })
  return databasePromise
}

function normalizeEmail(value: string): string {
  return value.trim().toLowerCase()
}

function storageKey(aliasEmail: string, uid: string): string {
  return normalizeEmail(aliasEmail) + '::' + uid
}

export async function listCachedICloudHMEMessages(
  aliasEmail: string,
  query = '',
): Promise<CachedSummary[]> {
  const db = await database()
  const cutoff = Date.now() - RETENTION_MS
  const normalizedQuery = query.trim().toLowerCase()
  const messages = await db.getAllFromIndex('summaries', 'by-alias', normalizeEmail(aliasEmail))
  return messages
    .filter((message) => Date.parse(message.receivedAt || message.cachedAt) >= cutoff)
    .filter((message) => {
      if (!normalizedQuery) return true
      const from = message.from.map((address) => [address.name, address.email].filter(Boolean).join(' ')).join(' ')
      return [message.subject, from, message.verificationCode].some((value) =>
        value?.toLowerCase().includes(normalizedQuery),
      )
    })
    .sort((left, right) => Date.parse(right.receivedAt) - Date.parse(left.receivedAt))
    .slice(0, MAX_PER_ALIAS)
}

export async function cacheICloudHMEMessages(
  aliasEmail: string,
  messages: ICloudHMEMailSummary[],
): Promise<void> {
  const db = await database()
  const normalized = normalizeEmail(aliasEmail)
  const transaction = db.transaction('summaries', 'readwrite')
  const now = new Date().toISOString()
  for (const message of messages) {
    const existing = await transaction.store.get(storageKey(normalized, message.id))
    await transaction.store.put({
      ...existing,
      ...message,
      aliasEmail: normalized,
      storageKey: storageKey(normalized, message.id),
      cachedAt: now,
    })
  }
  await transaction.done
  await pruneCache(db)
}

export async function getCachedICloudHMEBody(
  aliasEmail: string,
  uid: string,
): Promise<{ message: ICloudHMEMail; verificationCode?: string } | undefined> {
  const db = await database()
  const key = storageKey(aliasEmail, uid)
  const body = await db.get('bodies', key)
  if (!body || Date.parse(body.cachedAt) < Date.now() - RETENTION_MS) return undefined
  body.lastAccessAt = new Date().toISOString()
  await db.put('bodies', body)
  return { message: body.message, verificationCode: body.verificationCode }
}

export async function cacheICloudHMEBody(
  aliasEmail: string,
  message: ICloudHMEMail,
  verificationCode?: string,
): Promise<void> {
  const db = await database()
  const now = new Date().toISOString()
  await db.put('bodies', {
    storageKey: storageKey(aliasEmail, message.id),
    aliasEmail: normalizeEmail(aliasEmail),
    message,
    verificationCode,
    cachedAt: now,
    lastAccessAt: now,
  })
  await cacheICloudHMEMessages(aliasEmail, [{
    id: message.id,
    subject: message.subject,
    from: message.from,
    to: message.to,
    cc: message.cc,
    receivedAt: message.receivedAt,
    isRead: message.isRead,
    hasAttachments: false,
    verificationCode,
  }])
  await pruneCache(db)
}

export async function deleteICloudHMECacheForAlias(aliasEmail: string): Promise<void> {
  const db = await database()
  const normalized = normalizeEmail(aliasEmail)
  const transaction = db.transaction(['summaries', 'bodies'], 'readwrite')
  const [summaries, bodies] = await Promise.all([
    transaction.objectStore('summaries').index('by-alias').getAllKeys(normalized),
    transaction.objectStore('bodies').index('by-alias').getAllKeys(normalized),
  ])
  await Promise.all([
    ...summaries.map((key) => transaction.objectStore('summaries').delete(key)),
    ...bodies.map((key) => transaction.objectStore('bodies').delete(key)),
  ])
  await transaction.done
}

async function pruneCache(db: IDBPDatabase<ICloudHMECacheDB>): Promise<void> {
  const cutoff = Date.now() - RETENTION_MS
  const summaries = await db.getAll('summaries')
  const summariesByAlias = new Map<string, CachedSummary[]>()
  for (const summary of summaries) {
    const bucket = summariesByAlias.get(summary.aliasEmail) ?? []
    bucket.push(summary)
    summariesByAlias.set(summary.aliasEmail, bucket)
  }

  const summaryKeys = new Set<string>()
  for (const bucket of summariesByAlias.values()) {
    bucket
      .sort((left, right) => Date.parse(right.receivedAt) - Date.parse(left.receivedAt))
      .slice(MAX_PER_ALIAS)
      .forEach((message) => summaryKeys.add(message.storageKey))
  }
  summaries
    .filter((message) => Date.parse(message.receivedAt || message.cachedAt) < cutoff)
    .forEach((message) => summaryKeys.add(message.storageKey))
  summaries
    .sort((left, right) => Date.parse(right.cachedAt) - Date.parse(left.cachedAt))
    .slice(MAX_SUMMARIES)
    .forEach((message) => summaryKeys.add(message.storageKey))

  const bodies = await db.getAll('bodies')
  const bodyKeys = new Set(
    bodies
      .filter((body) => Date.parse(body.cachedAt) < cutoff)
      .map((body) => body.storageKey),
  )
  bodies
    .sort((left, right) => Date.parse(right.lastAccessAt) - Date.parse(left.lastAccessAt))
    .slice(MAX_BODIES)
    .forEach((body) => bodyKeys.add(body.storageKey))

  if (!summaryKeys.size && !bodyKeys.size) return
  const transaction = db.transaction(['summaries', 'bodies'], 'readwrite')
  await Promise.all([
    ...Array.from(summaryKeys, (key) => transaction.objectStore('summaries').delete(key)),
    ...Array.from(bodyKeys, (key) => transaction.objectStore('bodies').delete(key)),
  ])
  await transaction.done
}