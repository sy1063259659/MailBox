import { defineStore } from 'pinia'
import type {
  GptAccount,
  GptAccountStatus,
  GptQuotaSnapshot,
  GptSubscriptionExpiryBucket,
} from '@/types'
import {
  completeGptOAuth,
  importGptToken,
  listGptAccounts,
  refreshAllGptAccounts,
  refreshGptAccount,
  startGptOAuth,
  unlinkGptAccount,
} from '@/services/gptAccountApi'

interface GptAccountState {
  accounts: GptAccount[]
  loading: boolean
  bindingEmails: string[]
  refreshingEmails: string[]
  refreshingAll: boolean
  unlinkingEmails: string[]
  oauthLoginIdByEmail: Record<string, string>
  oauthAuthUrlByEmail: Record<string, string>
}

export const useGptAccountStore = defineStore('gptAccount', {
  state: (): GptAccountState => ({
    accounts: [],
    loading: false,
    bindingEmails: [],
    refreshingEmails: [],
    refreshingAll: false,
    unlinkingEmails: [],
    oauthLoginIdByEmail: {},
    oauthAuthUrlByEmail: {},
  }),

  getters: {
    accountByMailEmail: (state): Map<string, GptAccount> => {
      const map = new Map<string, GptAccount>()
      for (const account of state.accounts) {
        map.set(account.mailAccountEmail.toLowerCase(), account)
      }
      return map
    },
  },

  actions: {
    async loadAccounts(): Promise<void> {
      this.loading = true
      try {
        const accounts = await listGptAccounts()
        this.accounts = accounts.map(normalizeGptAccount)
      } finally {
        this.loading = false
      }
    },

    async bindByTokenJson(mailAccountEmail: string, tokenJson: string): Promise<GptAccount> {
      this.markBusy('bindingEmails', mailAccountEmail, true)
      try {
        const account = normalizeGptAccount(await importGptToken({ mailAccountEmail, tokenJson }))
        this.upsertAccount(account)
        return account
      } finally {
        this.markBusy('bindingEmails', mailAccountEmail, false)
      }
    },

    async startOAuth(mailAccountEmail: string): Promise<string> {
      this.markBusy('bindingEmails', mailAccountEmail, true)
      try {
        const result = await startGptOAuth({ mailAccountEmail })
        const email = mailAccountEmail.toLowerCase()
        this.oauthLoginIdByEmail[email] = result.loginId
        this.oauthAuthUrlByEmail[email] = result.authUrl
        return result.authUrl
      } finally {
        this.markBusy('bindingEmails', mailAccountEmail, false)
      }
    },

    async completeOAuth(mailAccountEmail: string, callbackUrl: string): Promise<GptAccount> {
      const email = mailAccountEmail.toLowerCase()
      const loginId = this.oauthLoginIdByEmail[email]
      if (!loginId) {
        throw new Error('请先发起 OAuth 授权')
      }
      this.markBusy('bindingEmails', mailAccountEmail, true)
      try {
        const account = normalizeGptAccount(await completeGptOAuth({ mailAccountEmail, loginId, callbackUrl }))
        this.upsertAccount(account)
        delete this.oauthLoginIdByEmail[email]
        delete this.oauthAuthUrlByEmail[email]
        return account
      } finally {
        this.markBusy('bindingEmails', mailAccountEmail, false)
      }
    },

    async refreshOne(mailAccountEmail: string): Promise<GptAccount> {
      this.markBusy('refreshingEmails', mailAccountEmail, true)
      try {
        const account = normalizeGptAccount(await refreshGptAccount(mailAccountEmail))
        this.upsertAccount(account)
        return account
      } finally {
        this.markBusy('refreshingEmails', mailAccountEmail, false)
      }
    },

    async refreshAll(): Promise<void> {
      this.refreshingAll = true
      try {
        const result = await refreshAllGptAccounts()
        this.accounts = result.accounts.map(normalizeGptAccount)
      } finally {
        this.refreshingAll = false
      }
    },

    async unlink(mailAccountEmail: string): Promise<void> {
      this.markBusy('unlinkingEmails', mailAccountEmail, true)
      try {
        await unlinkGptAccount(mailAccountEmail)
        const normalizedEmail = mailAccountEmail.toLowerCase()
        this.accounts = this.accounts.filter((account) => account.mailAccountEmail.toLowerCase() !== normalizedEmail)
      } finally {
        this.markBusy('unlinkingEmails', mailAccountEmail, false)
      }
    },

    upsertAccount(account: GptAccount): void {
      const normalizedEmail = account.mailAccountEmail.toLowerCase()
      const index = this.accounts.findIndex((item) => item.mailAccountEmail.toLowerCase() === normalizedEmail)
      if (index >= 0) {
        this.accounts.splice(index, 1, account)
        return
      }
      this.accounts.push(account)
    },

    markBusy(key: 'bindingEmails' | 'refreshingEmails' | 'unlinkingEmails', email: string, busy: boolean): void {
      const normalizedEmail = email.toLowerCase()
      const values = new Set(this[key].map((value) => value.toLowerCase()))
      if (busy) {
        values.add(normalizedEmail)
      } else {
        values.delete(normalizedEmail)
      }
      this[key] = Array.from(values)
    },
  },
})

function normalizeGptAccount(raw: GptAccount | Record<string, unknown>): GptAccount {
  const source = raw as Record<string, unknown>
  const mailAccountEmail = stringField(source.mailAccountEmail) || stringField(source.mailEmail) || stringField(source.email)
  const subscriptionActiveUntil = stringField(raw.subscriptionActiveUntil)
    || stringField(source.subscriptionExpiresAt)
    || stringField(source.expiresAt)
  const planType = stringField(raw.planType) || stringField(source.plan) || stringField(source.planName)
  const authFilePlanType = stringField(raw.authFilePlanType)

  return {
    id: numberField(raw.id) ?? 0,
    mailAccountEmail,
    gptEmail: stringField(raw.gptEmail) || stringField(source.accountEmail) || stringField(source.openaiEmail) || mailAccountEmail,
    accountId: stringField(raw.accountId) || undefined,
    organizationId: stringField(raw.organizationId) || undefined,
    accountName: stringField(raw.accountName) || undefined,
    accountStructure: stringField(raw.accountStructure) || undefined,
    planType: planType || undefined,
    planLabel: stringField(raw.planLabel) || formatPlanLabel(planType, authFilePlanType),
    authFilePlanType: authFilePlanType || undefined,
    subscriptionActiveUntil: subscriptionActiveUntil || undefined,
    subscriptionExpiryBucket: normalizeExpiryBucket(stringField(raw.subscriptionExpiryBucket) || stringField(source.expiryBucket), subscriptionActiveUntil),
    hourlyQuota: normalizeQuota(raw.hourlyQuota ?? source.hourlyPercentage, source.hourlyResetTime, source.hourlyWindowMinutes, source.hourlyWindowPresent),
    weeklyQuota: normalizeQuota(raw.weeklyQuota ?? source.weeklyPercentage, source.weeklyResetTime, source.weeklyWindowMinutes, source.weeklyWindowPresent),
    status: normalizeStatus(stringField(raw.status)),
    statusReason: stringField(raw.statusReason) || stringField(source.statusMessage) || stringField(source.errorMessage) || undefined,
    requiresReauth: Boolean(raw.requiresReauth),
    reauthReason: stringField(raw.reauthReason) || undefined,
    quotaErrorCode: stringField(raw.quotaErrorCode) || undefined,
    quotaErrorMessage: stringField(raw.quotaErrorMessage) || undefined,
    tokenExpiresAt: stringField(raw.tokenExpiresAt) || undefined,
    usageUpdatedAt: stringField(raw.usageUpdatedAt) || stringField(raw.lastRefreshAt) || undefined,
    lastRefreshAt: stringField(raw.lastRefreshAt) || undefined,
    createdAt: stringField(raw.createdAt) || undefined,
    updatedAt: stringField(raw.updatedAt) || undefined,
  }
}

function stringField(value: unknown): string {
  return typeof value === 'string' ? value.trim() : ''
}

function normalizeStatus(value: string): GptAccountStatus {
  const normalized = value.toLowerCase()
  switch (normalized) {
    case 'active':
    case 'expired':
    case 'quota_limited':
    case 'reauth_required':
    case 'banned_or_disabled':
    case 'error':
    case 'unknown':
      return normalized
    case 'disabled':
    case 'banned':
      return 'banned_or_disabled'
    case 'quota':
    case 'rate_limited':
      return 'quota_limited'
    case 'reauth':
    case 'auth_required':
    case 'token_expired':
      return 'reauth_required'
    default:
      return 'unknown'
  }
}

function normalizeExpiryBucket(value: string, expiresAt?: string): GptSubscriptionExpiryBucket {
  const normalized = value.toLowerCase()
  if (
    normalized === 'active'
    || normalized === 'within_24h'
    || normalized === 'within_7d'
    || normalized === 'within_30d'
    || normalized === 'expired'
    || normalized === 'unknown'
  ) {
    return normalized
  }
  if (!expiresAt) {
    return 'unknown'
  }
  const expiresTime = new Date(expiresAt).getTime()
  if (Number.isNaN(expiresTime)) {
    return 'unknown'
  }
  const now = Date.now()
  if (expiresTime <= now) {
    return 'expired'
  }
  const daysUntilExpiry = (expiresTime - now) / 86_400_000
  if (daysUntilExpiry <= 1) {
    return 'within_24h'
  }
  if (daysUntilExpiry <= 7) {
    return 'within_7d'
  }
  if (daysUntilExpiry <= 30) {
    return 'within_30d'
  }
  return 'active'
}

function normalizeQuota(value: unknown, resetAt?: unknown, windowMinutes?: unknown, present?: unknown): GptQuotaSnapshot | undefined {
  if (typeof value === 'number') {
    return {
      percentage: value,
      resetAt: stringField(resetAt) || undefined,
      windowMinutes: numberField(windowMinutes),
      present: typeof present === 'boolean' ? present : undefined,
    }
  }
  if (!value || typeof value !== 'object') {
    return undefined
  }
  const quota = value as Record<string, unknown>
  return {
    percentage: numberField(quota.percentage) ?? numberField(quota.remaining),
    resetAt: stringField(quota.resetAt) || undefined,
    windowMinutes: numberField(quota.windowMinutes),
    present: typeof quota.present === 'boolean' ? quota.present : undefined,
  }
}

function numberField(value: unknown): number | undefined {
  return typeof value === 'number' && Number.isFinite(value) ? value : undefined
}

function formatPlanLabel(planType: string, authFilePlanType: string): string | undefined {
  const normalized = planType.trim().toLowerCase()
  if (!normalized) {
    return undefined
  }
  if (normalized.includes('team')) return 'TEAM'
  if (normalized.includes('enterprise')) return 'ENTERPRISE'
  if (normalized.includes('business')) return 'BUSINESS'
  if (normalized.includes('edu')) return 'EDU'
  if (normalized.includes('go')) return 'GO'
  if (normalized.includes('plus')) return 'PLUS'
  if (normalized.includes('pro')) {
    const proType = `${authFilePlanType} ${planType}`.toLowerCase()
    return proType.includes('prolite') || proType.includes('pro-lite') || proType.includes('5x')
      ? 'PRO 5x'
      : 'PRO 20x'
  }
  if (normalized.includes('free')) return 'FREE'
  return normalized.toUpperCase()
}
