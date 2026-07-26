import { apiDelete, apiGet, apiPatch, apiPost, apiRequest } from './apiClient'

export interface ICloudHMEGroup {
  id: number
  name: string
  sortOrder: number
  createdAt: string
  updatedAt: string
}

export interface ICloudHMESourceAccount {
  id: number
  name: string
  appleIdEmail: string
  icloudEmail: string
  host: 'icloud.com' | 'icloud.com.cn'
  cookieConfigured: boolean
  appPasswordConfigured: boolean
  status: string
  statusReason?: string
  aliasTotal: number
  lastValidatedAt?: string
  lastSyncedAt?: string
  lastCreatedAt?: string
  lastErrorAt?: string
  automationEnabled: boolean
  nextCreateAt?: string
  cooldownLevel: number
  consecutiveLimitCount: number
  lastLimitAt?: string
  lastAutoAttemptAt?: string
  probeStage: number
  probeSuccessStreak: number
  probeSuccessTarget: number
  probeStableStage: number
  probeRecoveryMode: boolean
  probeLastIntervalSeconds: number
  probeLastRecoverySeconds: number
  probeLastLimitStage: number
  createdAt: string
  updatedAt: string
}

export interface ICloudHMEAlias {
  email: string
  sourceAccountId: number
  sourceAccountName: string
  anonymousId: string
  label: string
  active: boolean
  appleStatus: 'active' | 'inactive' | 'deleted' | 'unknown'
  deactivatedAt?: string
  deletedAt?: string
  lastSyncedAt?: string
  group: string
  remark: string
  mailReady: boolean
  receiveKeyConfigured: boolean
  receiveKeyUpdatedAt?: string
  inventoryStatus: 'available' | 'reserved' | 'sold'
  soldAt?: string
  gptStatus: 'unregistered' | 'plus' | 'deactivated'
  gptPlusActivatedAt?: string
  gptDeactivatedAt?: string
  gptLastScannedAt?: string
  gptScanError?: string
  createdAt: string
  updatedAt: string
}

export interface ICloudHMEAutomation {
  enabled: boolean
  targetAvailableCount: number
  targetGroup: string
  labelPrefix: string
  /** Origin used for public receive URLs. Empty means "use the current origin". */
  publicMailOrigin: string
  availableCount: number
  reservedCount: number
  soldCount: number
  pendingCount: number
  nextCreateAt?: string
  lastSuccessAt?: string
  lastLimitAt?: string
  updatedBy: string
  updatedAt: string
}

export interface ICloudHMEAutomationEvent {
  id: number
  sourceAccountId?: number
  sourceName?: string
  eventType: string
  result: string
  errorCode?: string
  message?: string
  nextAttemptAt?: string
  retryCount: number
  probeStage: number
  intervalSeconds: number
  recoverySeconds: number
  targetIntervalMinSeconds: number
  targetIntervalMaxSeconds: number
  createdAt: string
}

export interface ICloudHMEGPTScanResult {
  scanned: number
  plusFound: number
  bannedFound: number
  errors: number
}

export interface ICloudHMEReceiveKeyRecord {
  email: string
  key: string
  updatedAt: string
}

export interface ICloudHMEAddress {
  name?: string
  email?: string
}

export interface ICloudHMEMail {
  id: string
  subject: string
  from: ICloudHMEAddress[]
  to: ICloudHMEAddress[]
  cc: ICloudHMEAddress[]
  receivedAt: string
  isRead: boolean
  contentType: 'text/html' | 'text/plain'
  content: string
}

export interface ICloudHMEJobItem {
  id: number
  jobId: number
  sequence: number
  sourceAccountId?: number
  label: string
  email?: string
  status: 'pending' | 'running' | 'completed' | 'failed' | 'cancelled'
  attempts: number
  errorCode?: string
  errorMessage?: string
  startedAt?: string
  finishedAt?: string
  createdAt: string
  updatedAt: string
  nextAttemptAt?: string
  retryClass?: string
}

export interface ICloudHMEJob {
  id: number
  mode: 'fixed' | 'pool'
  sourceAccountId?: number
  labelPrefix: string
  groupName: string
  requestedCount: number
  status: 'pending' | 'running' | 'cancel_requested' | 'completed' | 'partial_failed' | 'cancelled'
  completedCount: number
  failedCount: number
  cancelledCount: number
  createdBy: string
  origin: 'manual' | 'automation'
  errorMessage?: string
  startedAt?: string
  finishedAt?: string
  createdAt: string
  updatedAt: string
  items?: ICloudHMEJobItem[]
}

export interface ICloudHMEMailSummary {
  id: string
  subject: string
  from: ICloudHMEAddress[]
  to: ICloudHMEAddress[]
  cc: ICloudHMEAddress[]
  receivedAt: string
  isRead: boolean
  hasAttachments: boolean
  verificationCode?: string
}

export interface ICloudHMEMailPage {
  messages: ICloudHMEMailSummary[]
  nextCursor?: string
}
export async function listICloudHMESourceAccounts(): Promise<ICloudHMESourceAccount[]> {
  const response = await apiGet<{ ok: boolean; accounts: ICloudHMESourceAccount[] }>('/icloud-hme/source-accounts')
  return response.accounts
}

export async function createICloudHMESourceAccount(input: {
  name: string
  appleIdEmail: string
  icloudEmail: string
  host: string
}): Promise<ICloudHMESourceAccount> {
  const response = await apiPost<{ ok: boolean; account: ICloudHMESourceAccount }>('/icloud-hme/source-accounts', input)
  return response.account
}

export async function saveICloudHMECookies(id: number, cookies: string): Promise<void> {
  await apiPost(`/icloud-hme/source-accounts/${id}/cookies`, { cookies })
}

export async function loginICloudHMESource(id: number, password: string, otp = ''): Promise<void> {
  await apiPost(`/icloud-hme/source-accounts/${id}/login`, { password, otp })
}

export async function saveICloudHMEAppPassword(id: number, appPassword: string): Promise<void> {
  await apiRequest(`/icloud-hme/source-accounts/${id}/app-password`, {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ appPassword }),
  })
}

export async function validateICloudHMESource(id: number): Promise<void> {
  await apiPost(`/icloud-hme/source-accounts/${id}/validate`)
}

export async function resumeICloudHMESourceAutomation(id: number): Promise<void> {
  await apiPost(`/icloud-hme/source-accounts/${id}/automation/resume`)
}

export async function deleteICloudHMESource(id: number): Promise<void> {
  await apiDelete(`/icloud-hme/source-accounts/${id}`)
}

export async function listICloudHMEAliases(): Promise<ICloudHMEAlias[]> {
  const response = await apiGet<{ ok: boolean; aliases: ICloudHMEAlias[] }>('/icloud-hme/aliases')
  return response.aliases
}

export async function getICloudHMEAutomation(): Promise<ICloudHMEAutomation> {
  const response = await apiGet<{ ok: boolean; automation: ICloudHMEAutomation }>('/icloud-hme/automation')
  return response.automation
}

export async function updateICloudHMEAutomation(input: {
  enabled: boolean
  targetAvailableCount: number
  targetGroup: string
  labelPrefix: string
  publicMailOrigin: string
}): Promise<ICloudHMEAutomation> {
  const response = await apiRequest<{ ok: boolean; automation: ICloudHMEAutomation }>('/icloud-hme/automation', {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(input),
  })
  return response.automation
}

export async function listICloudHMEAutomationEvents(): Promise<ICloudHMEAutomationEvent[]> {
  const response = await apiGet<{ ok: boolean; events: ICloudHMEAutomationEvent[] }>('/icloud-hme/automation/events')
  return response.events
}

export async function scanICloudHMEGPTStatus(): Promise<ICloudHMEGPTScanResult> {
  const response = await apiPost<{ ok: boolean; result: ICloudHMEGPTScanResult }>('/icloud-hme/gpt-status/scan')
  return response.result
}

export async function updateICloudHMEInventoryStatus(
  emails: string[],
  status: 'available' | 'reserved' | 'sold',
): Promise<void> {
  await apiPost('/icloud-hme/aliases/inventory-status', { emails, status })
}

export async function createICloudHMEAlias(id: number, label: string, group: string): Promise<string> {
  const response = await apiPost<{ ok: boolean; email: string }>(`/icloud-hme/source-accounts/${id}/aliases`, { label, group })
  return response.email
}

export async function syncICloudHMEAliases(id: number): Promise<{ imported: number; updated: number; total: number }> {
  return apiPost(`/icloud-hme/source-accounts/${id}/aliases/sync`)
}

export async function updateICloudHMERemark(email: string, remark: string): Promise<ICloudHMEAlias> {
  const response = await apiPatch<{ ok: boolean; alias: ICloudHMEAlias }>('/icloud-hme/aliases/remark', { email, remark })
  return response.alias
}

export async function moveICloudHMEAliases(emails: string[], group: string): Promise<void> {
  await apiPost('/icloud-hme/aliases/move-group', { emails, group })
}

export async function deleteICloudHMEAlias(email: string): Promise<void> {
  await apiDelete(`/icloud-hme/aliases/${encodeURIComponent(email)}`)
}

export async function generateICloudHMEReceiveKeys(emails: string[]): Promise<number> {
  const response = await apiPost<{ ok: boolean; generated: number }>('/icloud-hme/aliases/receive-keys/generate', { emails })
  return response.generated
}

export async function exportICloudHMEReceiveKeys(emails: string[]): Promise<ICloudHMEReceiveKeyRecord[]> {
  const response = await apiPost<{ ok: boolean; records: ICloudHMEReceiveKeyRecord[] }>('/icloud-hme/aliases/receive-keys/export', { emails })
  return response.records
}

export async function revealICloudHMEReceiveKey(email: string): Promise<ICloudHMEReceiveKeyRecord> {
  const response = await apiPost<{ ok: boolean; record: ICloudHMEReceiveKeyRecord }>(
    '/icloud-hme/aliases/' + encodeURIComponent(email) + '/receive-key/reveal',
  )
  return response.record
}

export async function resetICloudHMEReceiveKey(email: string): Promise<ICloudHMEReceiveKeyRecord> {
  const response = await apiPost<{ ok: boolean; record: ICloudHMEReceiveKeyRecord }>(
    '/icloud-hme/aliases/' + encodeURIComponent(email) + '/receive-key/reset',
  )
  return response.record
}

export async function getLatestICloudHMEMail(email: string): Promise<ICloudHMEMail> {
  const response = await apiPost<{ ok: boolean; email: ICloudHMEMail }>('/icloud-hme/mail/latest', { email })
  return response.email
}

export async function listICloudHMEGroups(): Promise<ICloudHMEGroup[]> {
  const response = await apiGet<{ ok: boolean; groups: ICloudHMEGroup[] }>('/icloud-hme/groups')
  return response.groups
}

export async function createICloudHMEGroup(name: string): Promise<ICloudHMEGroup> {
  const response = await apiPost<{ ok: boolean; group: ICloudHMEGroup }>('/icloud-hme/groups', { name })
  return response.group
}

export async function reorderICloudHMEGroups(ids: number[]): Promise<ICloudHMEGroup[]> {
  const response = await apiPatch<{ ok: boolean; groups: ICloudHMEGroup[] }>('/icloud-hme/groups/order', { ids })
  return response.groups
}

export async function renameICloudHMEGroup(id: number, name: string): Promise<ICloudHMEGroup> {
  const response = await apiPatch<{ ok: boolean; group: ICloudHMEGroup }>(`/icloud-hme/groups/${id}`, { name })
  return response.group
}

export async function deleteICloudHMEGroup(id: number): Promise<void> {
  await apiDelete(`/icloud-hme/groups/${id}`)
}
export async function startICloudHMELogin(id: number, password: string): Promise<{
  otpRequired: boolean
  challengeId?: string
  expiresAt?: string
}> {
  return apiPost('/icloud-hme/source-accounts/' + id + '/login/start', { password })
}

export async function completeICloudHMELogin(id: number, challengeId: string, otp: string): Promise<void> {
  await apiPost('/icloud-hme/source-accounts/' + id + '/login/complete', { challengeId, otp })
}

export async function validateAllICloudHMESources(): Promise<Array<{ id: number; ok: boolean; message?: string }>> {
  const response = await apiPost<{ ok: boolean; results: Array<{ id: number; ok: boolean; message?: string }> }>('/icloud-hme/source-accounts/validate-all')
  return response.results
}

export async function syncAllICloudHMESources(): Promise<Array<{ id: number; imported: number; updated: number; error?: string }>> {
  const response = await apiPost<{ ok: boolean; results: Array<{ id: number; imported: number; updated: number; error?: string }> }>('/icloud-hme/source-accounts/sync-all')
  return response.results
}

export async function createICloudHMEJob(input: {
  mode: 'fixed' | 'pool'
  sourceAccountId?: number
  labelPrefix: string
  group: string
  count: number
}): Promise<ICloudHMEJob> {
  const response = await apiPost<{ ok: boolean; job: ICloudHMEJob }>('/icloud-hme/jobs', input)
  return response.job
}

export async function listICloudHMEJobs(): Promise<ICloudHMEJob[]> {
  const response = await apiGet<{ ok: boolean; jobs: ICloudHMEJob[] }>('/icloud-hme/jobs')
  return response.jobs
}

export async function getICloudHMEJob(id: number): Promise<ICloudHMEJob> {
  const response = await apiGet<{ ok: boolean; job: ICloudHMEJob }>('/icloud-hme/jobs/' + id)
  return response.job
}

export async function cancelICloudHMEJob(id: number): Promise<void> {
  await apiPost('/icloud-hme/jobs/' + id + '/cancel')
}

export async function retryICloudHMEJob(id: number): Promise<void> {
  await apiPost('/icloud-hme/jobs/' + id + '/retry')
}

export interface ICloudHMELifecycleTask {
  id: string
  action: 'deactivate' | 'reactivate'
  status: 'running' | 'completed'
  total: number
  done: number
  results: Array<{ email: string; ok: boolean; error?: string }>
}

export async function startICloudHMEAliasLifecycle(
  emails: string[],
  action: 'deactivate' | 'reactivate',
): Promise<{ taskId: string; total: number }> {
  const response = await apiPost<{ ok: boolean; taskId: string; total: number }>(
    '/icloud-hme/aliases/lifecycle',
    { emails, action },
  )
  return { taskId: response.taskId, total: response.total }
}

export async function getICloudHMELifecycleTask(taskId: string): Promise<ICloudHMELifecycleTask> {
  const response = await apiGet<{ ok: boolean; task: ICloudHMELifecycleTask }>(
    '/icloud-hme/lifecycle-tasks/' + encodeURIComponent(taskId),
  )
  return response.task
}

export async function permanentlyDeleteICloudHMEAlias(email: string, confirmEmail: string): Promise<void> {
  await apiPost('/icloud-hme/aliases/' + encodeURIComponent(email) + '/delete-apple', { confirmEmail })
}

export async function listICloudHMEMessages(
  email: string,
  limit = 20,
  cursor = '',
): Promise<ICloudHMEMailPage> {
  const response = await apiPost<{ ok: boolean; messages: ICloudHMEMailSummary[]; nextCursor?: string }>(
    '/icloud-hme/mail/messages',
    { email, limit, cursor },
  )
  return { messages: response.messages, nextCursor: response.nextCursor }
}

export async function getICloudHMEMessage(
  email: string,
  uid: string,
): Promise<{ message: ICloudHMEMail; verificationCode?: string }> {
  const response = await apiPost<{ ok: boolean; message: ICloudHMEMail; verificationCode?: string }>(
    '/icloud-hme/mail/message',
    { email, uid },
  )
  return { message: response.message, verificationCode: response.verificationCode }
}

export async function refreshICloudHMECode(
  email: string,
): Promise<{ code?: string; message?: ICloudHMEMail }> {
  const response = await apiPost<{ ok: boolean; code?: string; message?: ICloudHMEMail }>(
    '/icloud-hme/mail/code',
    { email },
  )
  return { code: response.code, message: response.message }
}
