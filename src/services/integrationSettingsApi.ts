import { apiGet, apiPost } from './apiClient'

interface IntegrationAPIKeyResponse {
  ok: boolean
  apiKey: string
}

export async function getIntegrationAPIKey(): Promise<string> {
  const response = await apiGet<IntegrationAPIKeyResponse>('/settings/integration-api-key')
  return response.apiKey
}

export async function resetIntegrationAPIKey(): Promise<string> {
  const response = await apiPost<IntegrationAPIKeyResponse>('/settings/integration-api-key')
  return response.apiKey
}
