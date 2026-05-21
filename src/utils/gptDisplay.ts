import type {
  GptAccount,
  GptAccountStatus,
  GptQuotaSnapshot,
  GptSubscriptionExpiryBucket,
} from '@/types'

export const gptStatusType: Record<GptAccountStatus, 'info' | 'success' | 'warning' | 'danger'> = {
  active: 'success',
  expired: 'warning',
  quota_limited: 'warning',
  reauth_required: 'warning',
  banned_or_disabled: 'danger',
  error: 'danger',
  unknown: 'info',
}

export const gptStatusText: Record<GptAccountStatus, string> = {
  active: '正常',
  expired: '已过期',
  quota_limited: '额度受限',
  reauth_required: '需重登',
  banned_or_disabled: '禁用',
  error: '错误',
  unknown: '未知',
}

export const expiryBucketType: Record<GptSubscriptionExpiryBucket, 'info' | 'success' | 'warning' | 'danger'> = {
  active: 'success',
  within_24h: 'danger',
  within_7d: 'warning',
  within_30d: 'warning',
  expired: 'danger',
  unknown: 'info',
}

export function formatDateTime(value?: string): string {
  if (!value) {
    return '未同步'
  }
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) {
    return value
  }
  return date.toLocaleString('zh-CN', { hour12: false })
}

export function formatShortDate(value?: string): string {
  if (!value) {
    return '未知'
  }
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) {
    return value
  }
  return date.toLocaleDateString('zh-CN')
}

export function formatQuota(quota?: GptQuotaSnapshot): string {
  if (!quota || quota.present === false || quota.percentage === undefined) {
    return '未知'
  }
  return `${quota.percentage}%`
}

export function quotaPercent(quota?: GptQuotaSnapshot): number {
  if (!quota || quota.present === false || quota.percentage === undefined) {
    return 0
  }
  return Math.max(0, Math.min(100, quota.percentage))
}

export function quotaStatus(quota?: GptQuotaSnapshot): 'success' | 'warning' | 'exception' | undefined {
  const percentage = quotaPercent(quota)
  if (!quota || quota.present === false || quota.percentage === undefined) {
    return undefined
  }
  if (percentage <= 10) return 'exception'
  if (percentage <= 30) return 'warning'
  return 'success'
}

export function planText(account?: GptAccount): string {
  return account?.planLabel?.trim() || account?.planType?.trim()?.toUpperCase() || 'FREE'
}

export function compactGptStatus(account?: GptAccount): { text: string; type: 'info' | 'success' | 'warning' | 'danger' } {
  if (!account) {
    return { text: '未绑定', type: 'info' }
  }
  if (account.status === 'active') {
    return { text: '已绑定', type: 'success' }
  }
  return { text: gptStatusText[account.status], type: gptStatusType[account.status] }
}
