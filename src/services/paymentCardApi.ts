import { apiDelete, apiGet, apiPatch, apiPost, apiRequest } from './apiClient'

export interface PaymentCard {
  id: number
  numberMasked: string
  last4: string
  expiry: string
  status: 'active' | 'disabled' | 'used'
  failureReason?: string
  linkedEmails: string[]
  leaseOwner?: string
  leaseExpiresAt?: string
  usedAt?: string
  createdAt: string
  updatedAt: string
}

export interface IntegrationLease {
  id: string
  type: 'email' | 'card' | 'sms'
  resource: string
  ownerId: string
  queueId: string
  state: 'running' | 'held'
  expiresAt: string
}

export async function listPaymentCards(): Promise<PaymentCard[]> {
  const response = await apiGet<{ ok: boolean; cards: PaymentCard[] }>('/payment-cards')
  return response.cards
}

export async function importPaymentCards(text: string): Promise<{ cards: PaymentCard[]; errors: string[] }> {
  return apiPost('/payment-cards', { text })
}

export async function updatePaymentCardStatus(id: number, status: 'active' | 'disabled', reason = ''): Promise<void> {
  await apiPatch(`/payment-cards/${id}`, { status, reason })
}

export async function resetPaymentCard(id: number): Promise<void> {
  await apiPost(`/payment-cards/${id}/reset`)
}

export async function deletePaymentCard(id: number): Promise<void> {
  await apiDelete(`/payment-cards/${id}`)
}

export async function linkPaymentCard(email: string, cardId: number): Promise<void> {
  await apiPost('/icloud-hme/card-link', { email, cardId })
}

export async function unlinkPaymentCard(email: string, cardId: number): Promise<void> {
  await apiRequest('/icloud-hme/card-link', {
    method: 'DELETE', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ email, cardId }),
  })
}

export async function listIntegrationLeases(): Promise<IntegrationLease[]> {
  const response = await apiGet<{ ok: boolean; leases: IntegrationLease[] }>('/integration/v1/leases')
  return response.leases
}

export async function forceReleaseIntegrationLease(leaseId: string): Promise<void> {
  await apiPost('/integration/v1/leases/force-release', { leaseId })
}
