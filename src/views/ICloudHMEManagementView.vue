<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Check, CopyDocument, Delete, Document, EditPen, Files, FolderOpened, Key, Link, Message, More, Refresh, Setting, View } from '@element-plus/icons-vue'
import MailboxTopbar from '@/components/MailboxTopbar.vue'
import {
  completeICloudHMELogin, getICloudHMEJob, getICloudHMEMessage, listICloudHMEMessages,
  exportICloudHMEReceiveKeys, generateICloudHMEReceiveKeys,
  permanentlyDeleteICloudHMEAlias, refreshICloudHMECode, saveICloudHMEAppPassword,
  revealICloudHMEReceiveKey, resetICloudHMEReceiveKey,
  saveICloudHMECookies, startICloudHMELogin, syncAllICloudHMESources, syncICloudHMEAliases,
  updateICloudHMEAliasLifecycle, validateAllICloudHMESources, validateICloudHMESource,
  getICloudHMEAutomation, listICloudHMEAutomationEvents, updateICloudHMEAutomation,
  updateICloudHMEInventoryStatus, scanICloudHMEGPTStatus,
  type ICloudHMEAlias, type ICloudHMEGroup, type ICloudHMEJob, type ICloudHMEMail,
  type ICloudHMEMailSummary, type ICloudHMESourceAccount, type ICloudHMEReceiveKeyRecord,
  type ICloudHMEAutomation, type ICloudHMEAutomationEvent,
} from '@/services/iCloudHmeApi'
import {
  cacheICloudHMEBody, cacheICloudHMEMessages, deleteICloudHMECacheForAlias,
  getCachedICloudHMEBody, listCachedICloudHMEMessages,
} from '@/services/iCloudHmeStorage'
import {
  assignSMSMailbox, getLatestSMS, listSMSAccounts, updateSMSStatus,
  type SMSAccount, type SMSLatestResult,
} from '@/services/smsApi'
import { ICLOUD_HME_DEFAULT_GROUP, useICloudHmeStore } from '@/stores/iCloudHme'
import { formatDateTime } from '@/utils/dateTime'
import { plainMailBlocks } from '@/utils/mailBody'

const ICLOUD_HME_PUBLIC_MAIL_ORIGIN = 'https://inbox-api.xyue.online'

const store = useICloudHmeStore()
const keyword = ref('')
const sourceFilter = ref<number | 'all'>('all')
const statusFilter = ref<'all' | ICloudHMEAlias['appleStatus']>('all')
const gptStatusFilter = ref<'all' | ICloudHMEAlias['gptStatus']>('all')
const page = ref(1)
const pageSize = ref(20)
const selectedRows = ref<ICloudHMEAlias[]>([])
const copiedValues = ref(new Set<string>())
const draggingGroupId = ref<number>()
const renamingGroupId = ref<number>()
const deletingGroupId = ref<number>()
const busy = ref(false)
const receiveKeyBusy = ref(false)
const gptScanBusy = ref(false)
const receiveURLBusyEmails = ref(new Set<string>())
const receiveKeyDialogVisible = ref(false)
const receiveKeyRecord = ref<ICloudHMEReceiveKeyRecord>()
const automation = ref<ICloudHMEAutomation>()
const automationDialogVisible = ref(false)
const automationEventsVisible = ref(false)
const automationEvents = ref<ICloudHMEAutomationEvent[]>([])
const automationSaving = ref(false)
const automationForm = ref({
  enabled: false,
  targetAvailableCount: 20,
  targetGroup: ICLOUD_HME_DEFAULT_GROUP,
  labelPrefix: 'MailBox',
})

const sourceDialogVisible = ref(false)
const sourceForm = ref({ name: '', appleIdEmail: '', icloudEmail: '', host: 'icloud.com' })
const sourceCreating = ref(false)
const sourceBatchBusy = ref(false)
const sourceActionId = ref<number>()
const credentialDialogVisible = ref(false)
const credentialMode = ref<'cookies' | 'login' | 'appPassword'>('cookies')
const credentialSource = ref<ICloudHMESourceAccount>()
const credentialForm = ref({ cookies: '', password: '', otp: '', appPassword: '' })
const credentialSaving = ref(false)
const loginChallengeId = ref('')
const loginStep = ref<'password' | 'otp'>('password')

const jobsDialogVisible = ref(false)
const selectedJob = ref<ICloudHMEJob>()
const jobActionId = ref<number>()
const moveDialogVisible = ref(false)
const targetGroup = ref(ICLOUD_HME_DEFAULT_GROUP)

const mailVisible = ref(false)
const mailAlias = ref<ICloudHMEAlias>()
const mailMessages = ref<ICloudHMEMailSummary[]>([])
const mailKeyword = ref('')
const mailListLoading = ref(false)
const mailBodyLoading = ref(false)
const mailCodeLoading = ref(false)
const mailNextCursor = ref('')
const selectedMail = ref<ICloudHMEMailSummary>()
const mailDetail = ref<ICloudHMEMail>()
const verificationCode = ref('')
const smsAccounts = ref<SMSAccount[]>([])
const smsCodeVisible = ref(false)
const smsCodeAlias = ref<ICloudHMEAlias>()
const smsCodeAccount = ref<SMSAccount>()
const smsCodeResult = ref<SMSLatestResult>()
const smsCodeLoading = ref(false)
const smsCodePolling = ref(false)
const smsBindingVisible = ref(false)
const smsBindingAlias = ref<ICloudHMEAlias>()
const smsBindingPhone = ref('')
const smsBindingKeyword = ref('')
const smsBindingSaving = ref(false)
const smsStatusUpdatingPhone = ref('')

const filteredAliases = computed(() => {
  const query = keyword.value.trim().toLowerCase()
  return store.aliases.filter((alias) => {
    if (store.selectedGroup && alias.group !== store.selectedGroup) return false
    if (sourceFilter.value !== 'all' && alias.sourceAccountId !== sourceFilter.value) return false
    if (statusFilter.value !== 'all' && alias.appleStatus !== statusFilter.value) return false
    if (gptStatusFilter.value !== 'all' && alias.gptStatus !== gptStatusFilter.value) return false
    return !query || [alias.email, alias.sourceAccountName, alias.label, alias.group, alias.remark, alias.gptStatus]
      .some((value) => value.toLowerCase().includes(query))
  })
})
const pagedAliases = computed(() => filteredAliases.value.slice((page.value - 1) * pageSize.value, page.value * pageSize.value))
const groupCounts = computed(() => {
  const counts = new Map<string, number>()
  store.aliases.forEach((alias) => counts.set(alias.group, (counts.get(alias.group) ?? 0) + 1))
  return counts
})
const primaryProbeSource = computed(() => store.sources
  .filter((source) => source.automationEnabled)
  .sort((left, right) => {
    const leftTime = left.nextCreateAt ? new Date(left.nextCreateAt).getTime() : Number.MAX_SAFE_INTEGER
    const rightTime = right.nextCreateAt ? new Date(right.nextCreateAt).getTime() : Number.MAX_SAFE_INTEGER
    return leftTime - rightTime
  })[0])
const filteredMailMessages = computed(() => {
  const query = mailKeyword.value.trim().toLowerCase()
  if (!query) return mailMessages.value
  return mailMessages.value.filter((message) => {
    const from = message.from.map((item) => (item.name || '') + ' ' + (item.email || '')).join(' ')
    return [message.subject, from, message.verificationCode].some((value) => value?.toLowerCase().includes(query))
  })
})
const mailHtml = computed(() => {
  const content = mailDetail.value?.content?.trim() ?? ''
  if (!content || mailDetail.value?.contentType !== 'text/html') return ''
  const styles = '<meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><base target="_blank"><style>html{background:#f4f6f9;color-scheme:light}*{box-sizing:border-box}body{max-width:760px;margin:0 auto!important;padding:28px!important;background:#fff;color:#1f2937;overflow-wrap:anywhere}img{max-width:100%!important;height:auto!important}table{max-width:100%!important}a{color:#2563eb}</style>'
  if (/<\/head>/i.test(content)) return content.replace(/<\/head>/i, styles + '</head>')
  return '<!doctype html><html><head>' + styles + '</head><body>' + content + '</body></html>'
})
const mailBlocks = computed(() => plainMailBlocks(mailDetail.value?.contentType === 'text/plain' ? mailDetail.value.content : ''))
const smsAccountByAlias = computed(() => {
  const accounts = new Map<string, SMSAccount>()
  smsAccounts.value.forEach((account) => {
    account.linkedMailboxEmails.forEach((email) => accounts.set(email.toLowerCase(), account))
  })
  return accounts
})
const sortedSMSAccounts = computed(() => {
  const query = smsBindingKeyword.value.trim().toLowerCase()
  return [...smsAccounts.value]
    .filter((account) => !query || [
      account.phone,
      account.remark,
      ...account.linkedMailboxEmails,
    ].some((value) => value.toLowerCase().includes(query)))
    .sort((left, right) => {
      const statusDifference = Number(left.status === 'invalid') - Number(right.status === 'invalid')
      if (statusDifference) return statusDifference
      const countDifference = left.linkedMailboxEmails.length - right.linkedMailboxEmails.length
      if (countDifference) return countDifference
      const leftLastBoundAt = lastSMSBindingTime(left)
      const rightLastBoundAt = lastSMSBindingTime(right)
      if (leftLastBoundAt !== rightLastBoundAt) return leftLastBoundAt - rightLastBoundAt
      return left.phone.localeCompare(right.phone)
    })
})
const smsBindingCurrentAccount = computed(() =>
  smsBindingAlias.value ? boundSMSAccount(smsBindingAlias.value) : undefined,
)

let jobPollTimer: number | undefined
let jobPolling = false
let smsCodeTimer: number | undefined
watch([keyword, sourceFilter, statusFilter, gptStatusFilter, () => store.selectedGroup], () => { page.value = 1; selectedRows.value = [] })
watch(() => filteredAliases.value.length, (total) => { page.value = Math.min(page.value, Math.max(1, Math.ceil(total / pageSize.value))) })
onMounted(async () => {
  try {
    await Promise.all([store.load(), loadAutomation(), loadSMSBindings()])
  } catch (error) { showError(error, '加载隐藏邮箱失败') }
  jobPollTimer = window.setInterval(pollJobs, 15000)
})
onBeforeUnmount(() => {
  if (jobPollTimer) window.clearInterval(jobPollTimer)
  stopSMSCodePolling()
})
watch(smsCodeVisible, (visible) => {
  if (!visible) stopSMSCodePolling()
})

async function pollJobs() {
  if (jobPolling) return
  jobPolling = true
  try {
    await Promise.all([store.loadJobs(), store.loadSources(), loadAutomation()])
    if (selectedJob.value) selectedJob.value = await getICloudHMEJob(selectedJob.value.id)
  } catch { /* Background polling stays silent. */ }
  finally { jobPolling = false }
}

async function loadAutomation() {
  automation.value = await getICloudHMEAutomation()
}

async function loadSMSBindings() {
  smsAccounts.value = await listSMSAccounts()
}

function boundSMSAccount(alias: ICloudHMEAlias) {
  return smsAccountByAlias.value.get(alias.email.toLowerCase())
}

function lastSMSBindingTime(account: SMSAccount) {
  return account.linkedMailboxes.reduce((latest, binding) => {
    const timestamp = Date.parse(binding.boundAt)
    return Number.isFinite(timestamp) ? Math.max(latest, timestamp) : latest
  }, 0)
}

function boundSMSBinding(alias: ICloudHMEAlias) {
  const account = boundSMSAccount(alias)
  if (!account) return undefined
  const binding = account.linkedMailboxes.find((item) => item.email.toLowerCase() === alias.email.toLowerCase())
  return binding ? { account, binding } : undefined
}

function openSMSBinding(alias: ICloudHMEAlias) {
  const current = boundSMSAccount(alias)
  smsBindingAlias.value = alias
  smsBindingPhone.value = current?.status === 'active' ? current.phone : ''
  smsBindingKeyword.value = ''
  smsBindingVisible.value = true
}

function smsAccountSelectable(account: SMSAccount) {
  return account.status === 'active'
    && (account.phone === smsBindingPhone.value || account.linkedMailboxEmails.length < 3)
}

async function toggleSMSStatus(account: SMSAccount) {
  const nextStatus: SMSAccount['status'] = account.status === 'active' ? 'invalid' : 'active'
  if (nextStatus === 'invalid') {
    await ElMessageBox.confirm(
      `标记失效后将停止取码，但保留已绑定的 ${account.linkedMailboxEmails.length} 个隐藏邮箱。`,
      '标记接码失效',
      { type: 'warning', confirmButtonText: '标记失效' },
    )
  }
  smsStatusUpdatingPhone.value = account.phone
  try {
    const updated = await updateSMSStatus(account.phone, nextStatus)
    const index = smsAccounts.value.findIndex((item) => item.phone === account.phone)
    if (index >= 0) smsAccounts.value[index] = updated
    if (nextStatus === 'invalid' && smsBindingPhone.value === account.phone) {
      smsBindingPhone.value = ''
    }
    ElMessage.success(nextStatus === 'invalid' ? '接码已标记失效' : '接码已恢复')
  } catch (error) {
    showError(error, '更新接码状态失败')
  } finally {
    smsStatusUpdatingPhone.value = ''
  }
}

async function saveSMSBinding(phone = smsBindingPhone.value) {
  if (!smsBindingAlias.value || smsBindingSaving.value) return
  smsBindingSaving.value = true
  try {
    smsAccounts.value = await assignSMSMailbox(smsBindingAlias.value.email, phone)
    smsBindingPhone.value = phone
    smsBindingVisible.value = false
    ElMessage.success(phone ? '接码绑定已保存' : '接码绑定已解除')
  } catch (error) {
    showError(error, '保存接码绑定失败')
  } finally {
    smsBindingSaving.value = false
  }
}

async function openSMSCode(alias: ICloudHMEAlias) {
  let account = boundSMSAccount(alias)
  if (!account) {
    try {
      await loadSMSBindings()
      account = boundSMSAccount(alias)
    } catch (error) {
      showError(error, '加载接码绑定失败')
      return
    }
  }
  if (!account) {
    ElMessage.info('该隐藏邮箱尚未绑定接码账号')
    return
  }
  if (account.status === 'invalid') {
    ElMessage.warning('该接码账号已失效，请更换或恢复后再查看验证码')
    return
  }
  smsCodeAlias.value = alias
  smsCodeAccount.value = account
  smsCodeResult.value = undefined
  smsCodeVisible.value = true
  startSMSCodePolling()
}

async function refreshSMSCode() {
  if (!smsCodeAccount.value || smsCodeLoading.value) return
  smsCodeLoading.value = true
  try {
    smsCodeResult.value = await getLatestSMS(smsCodeAccount.value.phone)
    if (smsCodeResult.value.code) {
      stopSMSCodePolling()
      ElMessage.success('已获取短信验证码')
    }
  } catch (error) {
    stopSMSCodePolling()
    showError(error, '获取短信验证码失败')
  } finally {
    smsCodeLoading.value = false
  }
}

function startSMSCodePolling() {
  stopSMSCodePolling()
  smsCodePolling.value = true
  void refreshSMSCode()
  smsCodeTimer = window.setInterval(refreshSMSCode, 5000)
}

function stopSMSCodePolling() {
  if (smsCodeTimer) window.clearInterval(smsCodeTimer)
  smsCodeTimer = undefined
  smsCodePolling.value = false
}

async function copySMSCode() {
  if (!smsCodeResult.value?.code) return
  await copyValue(smsCodeResult.value.code, '短信验证码')
}

function openAutomationSettings() {
  const current = automation.value
  automationForm.value = {
    enabled: current?.enabled ?? false,
    targetAvailableCount: current?.targetAvailableCount ?? 20,
    targetGroup: current?.targetGroup || ICLOUD_HME_DEFAULT_GROUP,
    labelPrefix: current?.labelPrefix || 'MailBox',
  }
  automationDialogVisible.value = true
}

async function saveAutomation() {
  automationSaving.value = true
  try {
    automation.value = await updateICloudHMEAutomation(automationForm.value)
    automationDialogVisible.value = false
    ElMessage.success(automation.value.enabled ? '自动补货已启用' : '自动补货设置已保存')
  } catch (error) { showError(error, '保存自动补货设置失败') }
  finally { automationSaving.value = false }
}

async function openAutomationEvents() {
  automationEventsVisible.value = true
  try { automationEvents.value = await listICloudHMEAutomationEvents() }
  catch (error) { showError(error, '加载运行记录失败') }
}

async function markInventory(status: 'available' | 'reserved' | 'sold') {
  if (!selectedRows.value.length) return ElMessage.warning('请先勾选隐藏邮箱')
  busy.value = true
  try {
    await updateICloudHMEInventoryStatus(selectedRows.value.map((item) => item.email), status)
    selectedRows.value = []
    await Promise.all([store.load(), loadAutomation()])
    ElMessage.success(status === 'sold' ? '已标记为已售' : status === 'reserved' ? '已标记为预留' : '已恢复为可售')
  } catch (error) { showError(error, '更新库存状态失败') }
  finally { busy.value = false }
}

function inventoryText(status: ICloudHMEAlias['inventoryStatus']) {
  return status === 'sold' ? '已售' : status === 'reserved' ? '预留' : '可售'
}

function inventoryType(status: ICloudHMEAlias['inventoryStatus']) {
  return status === 'sold' ? 'info' : status === 'reserved' ? 'warning' : 'success'
}
async function selectJob(job?: ICloudHMEJob) {
  if (!job) {
    selectedJob.value = undefined
    return
  }
  selectedJob.value = job
  try { selectedJob.value = await getICloudHMEJob(job.id) }
  catch (error) { showError(error, '加载任务明细失败') }
}
function showError(error: unknown, fallback: string) { ElMessage.error(error instanceof Error ? error.message : fallback) }
function handleSelection(rows: ICloudHMEAlias[]) { selectedRows.value = rows }
function groupCount(group: ICloudHMEGroup) { return groupCounts.value.get(group.name) ?? 0 }
function canDeleteGroup(group: ICloudHMEGroup) { return group.name !== ICLOUD_HME_DEFAULT_GROUP && groupCount(group) === 0 }
function sourceStatusType(status: string) { return status === 'active' ? 'success' : ['pending', 'cooldown'].includes(status) ? 'warning' : 'danger' }
function sourceStatusText(status: string) { return status === 'active' ? '会话正常' : status === 'cooldown' ? '创建冷却' : status === 'pending' ? '待配置' : '需处理' }
function probeRangeText(stage: number) {
  return ['5–7 分钟', '3–5 分钟', '2–3 分钟'][Math.min(2, Math.max(0, stage))] || '5–7 分钟'
}
function probeStableText(stage: number) {
  return stage >= 0 ? probeRangeText(stage) : '采样中'
}
function formatProbeDuration(seconds: number) {
  if (!seconds) return '—'
  if (seconds < 60) return `${seconds} 秒`
  const minutes = seconds / 60
  return minutes < 60 ? `${minutes.toFixed(minutes < 10 ? 1 : 0)} 分钟` : `${(minutes / 60).toFixed(1)} 小时`
}
function eventProbeRange(event: ICloudHMEAutomationEvent) {
  if (event.probeStage < 0) return '—'
  if (event.targetIntervalMinSeconds && event.targetIntervalMaxSeconds) {
    return `${event.targetIntervalMinSeconds / 60}–${event.targetIntervalMaxSeconds / 60} 分钟`
  }
  return probeRangeText(event.probeStage)
}
function aliasStatusType(status: ICloudHMEAlias['appleStatus']) { return status === 'active' ? 'success' : status === 'inactive' ? 'warning' : status === 'deleted' ? 'danger' : 'info' }
function aliasStatusText(status: ICloudHMEAlias['appleStatus']) { return { active: '已启用', inactive: '已停用', deleted: '已永久删除', unknown: '状态未知' }[status] }
function gptStatusType(status: ICloudHMEAlias['gptStatus']) { return status === 'plus' ? 'success' : status === 'deactivated' ? 'danger' : 'info' }
function gptStatusText(status: ICloudHMEAlias['gptStatus']) { return status === 'plus' ? 'PLUS' : status === 'deactivated' ? '已封号' : '未开通' }
function gptSurvivalText(alias: ICloudHMEAlias) {
  if (!alias.gptPlusActivatedAt || !alias.gptDeactivatedAt) return ''
  const seconds = Math.max(0, Math.floor((new Date(alias.gptDeactivatedAt).getTime() - new Date(alias.gptPlusActivatedAt).getTime()) / 1000))
  const days = Math.floor(seconds / 86400)
  const hours = Math.floor((seconds % 86400) / 3600)
  const minutes = Math.floor((seconds % 3600) / 60)
  return days ? `${days}天 ${hours}小时` : hours ? `${hours}小时 ${minutes}分钟` : `${minutes}分钟`
}
async function scanGPTStatus() {
  gptScanBusy.value = true
  try {
    const result = await scanICloudHMEGPTStatus()
    await store.load()
    ElMessage.success(`扫描 ${result.scanned} 个邮箱，发现 PLUS ${result.plusFound} 个、封号 ${result.bannedFound} 个${result.errors ? `，${result.errors} 个主账号失败` : ''}`)
  } catch (error) { showError(error, '扫描 GPT 状态失败') } finally { gptScanBusy.value = false }
}
function jobStatusText(status: ICloudHMEJob['status']) { return { pending: '等待中', running: '执行中', cancel_requested: '取消中', completed: '已完成', partial_failed: '部分失败', cancelled: '已取消' }[status] }
function jobStatusType(status: ICloudHMEJob['status']) { return status === 'completed' ? 'success' : ['running', 'pending'].includes(status) ? 'primary' : status === 'partial_failed' ? 'danger' : 'info' }
function jobProgress(job: ICloudHMEJob) { return Math.round(((job.completedCount + job.failedCount + job.cancelledCount) / Math.max(job.requestedCount, 1)) * 100) }
function formatAddress(addresses: Array<{ name?: string; email?: string }>) { return addresses.map((item) => item.name ? item.name + ' <' + item.email + '>' : item.email).filter(Boolean).join(', ') || '未知发件人' }
async function copyValue(value: string, label = '邮箱') {
  await navigator.clipboard.writeText(value)
  copiedValues.value = new Set(copiedValues.value).add(value)
  ElMessage.success(label + '已复制')
  window.setTimeout(() => { const next = new Set(copiedValues.value); next.delete(value); copiedValues.value = next }, 1200)
}
async function copySelected() {
  const targets = selectedRows.value.length ? selectedRows.value : filteredAliases.value
  if (!targets.length) return ElMessage.warning('没有可复制的隐藏邮箱')
  await navigator.clipboard.writeText(targets.map((item) => item.email).join('\n'))
  ElMessage.success('已复制 ' + targets.length + ' 个隐藏邮箱')
}
function receiveMailURL(kind: 'latest' | 'history', record = receiveKeyRecord.value) {
  if (!record) return ''
  const url = new URL('/api/public/icloud-hme/mail/' + kind, ICLOUD_HME_PUBLIC_MAIL_ORIGIN)
  url.searchParams.set('address', record.email)
  url.searchParams.set('key', record.key)
  if (kind === 'history') url.searchParams.set('limit', '20')
  return url.toString()
}
async function receiveKeyRecordsForAliases(targets: ICloudHMEAlias[]) {
  const missing = targets.filter((item) => !item.receiveKeyConfigured)
  if (missing.length) {
    await generateICloudHMEReceiveKeys(missing.map((item) => item.email))
    await store.load()
  }
  const records = await exportICloudHMEReceiveKeys(targets.map((item) => item.email))
  const byEmail = new Map(records.map((item) => [item.email.toLowerCase(), item]))
  return targets.map((item) => byEmail.get(item.email.toLowerCase())).filter((item): item is ICloudHMEReceiveKeyRecord => Boolean(item))
}
async function copyLatestReceiveURLs(format: 'url' | 'email-url' = 'url') {
  const requested = selectedRows.value.length ? [...selectedRows.value] : [...filteredAliases.value]
  const targets = requested.filter((item) => item.appleStatus === 'active')
  if (!targets.length) return ElMessage.warning('没有可复制取件 URL 的启用邮箱')
  receiveKeyBusy.value = true
  try {
    const records = await receiveKeyRecordsForAliases(targets)
    if (!records.length) return ElMessage.warning('目标隐藏邮箱尚未配置收件密钥')
    const content = records.map((item) => {
      const url = receiveMailURL('latest', item)
      return format === 'email-url' ? item.email + '---' + url : url
    }).join('\n')
    await navigator.clipboard.writeText(content)
    const skipped = requested.length - records.length
    const label = format === 'email-url' ? '条邮箱和取件 URL' : '个最新取件 URL'
    ElMessage.success('已复制 ' + records.length + ' ' + label + (skipped ? '，跳过 ' + skipped + ' 个不可用邮箱' : ''))
  } catch (error) {
    showError(error, format === 'email-url' ? '复制邮箱和取件 URL 失败' : '复制最新取件 URL 失败')
  } finally { receiveKeyBusy.value = false }
}
async function copyLatestReceiveURL(alias: ICloudHMEAlias) {
  if (alias.appleStatus !== 'active') return ElMessage.warning('该隐藏邮箱当前不可用')
  receiveURLBusyEmails.value = new Set(receiveURLBusyEmails.value).add(alias.email)
  try {
    const records = await receiveKeyRecordsForAliases([alias])
    if (!records[0]) return ElMessage.warning('该隐藏邮箱尚未配置收件密钥')
    await copyValue(receiveMailURL('latest', records[0]), '最新取件 URL')
  } catch (error) { showError(error, '复制最新取件 URL 失败') } finally {
    const next = new Set(receiveURLBusyEmails.value)
    next.delete(alias.email)
    receiveURLBusyEmails.value = next
  }
}
async function generateSelectedReceiveKeys() {
  if (!selectedRows.value.length) return ElMessage.warning('请先勾选隐藏邮箱')
  receiveKeyBusy.value = true
  try {
    const generated = await generateICloudHMEReceiveKeys(selectedRows.value.map((item) => item.email))
    await store.load()
    generated
      ? ElMessage.success('已为 ' + generated + ' 个隐藏邮箱生成收件密钥')
      : ElMessage.info('所选隐藏邮箱均已配置收件密钥')
  } catch (error) { showError(error, '生成收件密钥失败') } finally { receiveKeyBusy.value = false }
}
async function openReceiveKey(alias: ICloudHMEAlias) {
  receiveKeyBusy.value = true
  try {
    if (!alias.receiveKeyConfigured) {
      await generateICloudHMEReceiveKeys([alias.email])
      await store.load()
    }
    receiveKeyRecord.value = await revealICloudHMEReceiveKey(alias.email)
    receiveKeyDialogVisible.value = true
  } catch (error) { showError(error, '读取收件密钥失败') } finally { receiveKeyBusy.value = false }
}
function clearReceiveKey() { receiveKeyRecord.value = undefined }
async function resetReceiveKey() {
  const current = receiveKeyRecord.value
  if (!current) return
  await ElMessageBox.confirm('重置后旧密钥和旧收件链接会立即失效。确定继续？', '重置收件密钥', { type: 'warning' })
  receiveKeyBusy.value = true
  try {
    receiveKeyRecord.value = await resetICloudHMEReceiveKey(current.email)
    await store.load()
    ElMessage.success('收件密钥已重置')
  } catch (error) { showError(error, '重置收件密钥失败') } finally { receiveKeyBusy.value = false }
}
async function copyReceiveURL(kind: 'latest' | 'history') {
  const value = receiveMailURL(kind)
  if (value) await copyValue(value, kind === 'latest' ? '最新邮件链接' : '历史邮件链接')
}
async function exportReceiveKeys() {
  const targets = selectedRows.value.length ? selectedRows.value : filteredAliases.value
  if (!targets.length) return ElMessage.warning('没有可导出的隐藏邮箱')
  receiveKeyBusy.value = true
  try {
    const records = await exportICloudHMEReceiveKeys(targets.map((item) => item.email))
    if (!records.length) return ElMessage.warning('目标隐藏邮箱尚未配置收件密钥')
    const content = records.map((item) => item.email + '----' + item.key).join('\n')
    const blob = new Blob([content], { type: 'text/plain;charset=utf-8' })
    const link = document.createElement('a')
    link.href = URL.createObjectURL(blob)
    link.download = 'icloud-hme-receive-keys-' + new Date().toISOString().slice(0, 10) + '.txt'
    link.click()
    URL.revokeObjectURL(link.href)
    const skipped = targets.length - records.length
    ElMessage.success('已导出 ' + records.length + ' 个收件密钥' + (skipped ? '，跳过 ' + skipped + ' 个未配置账号' : ''))
  } catch (error) { showError(error, '导出收件密钥失败') } finally { receiveKeyBusy.value = false }
}
async function createSource() {
  if (!sourceForm.value.name.trim() || !sourceForm.value.appleIdEmail.trim() || !sourceForm.value.icloudEmail.trim()) return ElMessage.warning('请填写完整主账号信息')
  sourceCreating.value = true
  try {
    await store.createSource(sourceForm.value)
    sourceForm.value = { name: '', appleIdEmail: '', icloudEmail: '', host: 'icloud.com' }
    ElMessage.success('主账号已添加')
  } catch (error) { showError(error, '添加主账号失败') } finally { sourceCreating.value = false }
}
function openCredential(source: ICloudHMESourceAccount, mode: 'cookies' | 'login' | 'appPassword') {
  credentialSource.value = source
  credentialMode.value = mode
  clearCredentialForm()
  credentialDialogVisible.value = true
}
function clearCredentialForm() {
  credentialForm.value = { cookies: '', password: '', otp: '', appPassword: '' }
  loginChallengeId.value = ''
  loginStep.value = 'password'
}
async function saveCredential() {
  const source = credentialSource.value
  if (!source) return
  credentialSaving.value = true
  try {
    if (credentialMode.value === 'cookies') {
      await saveICloudHMECookies(source.id, credentialForm.value.cookies)
      credentialDialogVisible.value = false
    } else if (credentialMode.value === 'appPassword') {
      await saveICloudHMEAppPassword(source.id, credentialForm.value.appPassword)
      credentialDialogVisible.value = false
    } else if (loginStep.value === 'password') {
      const result = await startICloudHMELogin(source.id, credentialForm.value.password)
      credentialForm.value.password = ''
      if (result.otpRequired && result.challengeId) {
        loginChallengeId.value = result.challengeId
        loginStep.value = 'otp'
        ElMessage.info('请输入 Apple 双重认证验证码')
      } else credentialDialogVisible.value = false
    } else {
      await completeICloudHMELogin(source.id, loginChallengeId.value, credentialForm.value.otp)
      credentialDialogVisible.value = false
    }
    await store.load()
    if (!credentialDialogVisible.value) ElMessage.success('主账号配置已保存')
  } catch (error) { showError(error, '保存主账号配置失败') } finally { credentialSaving.value = false }
}
async function validateSource(source: ICloudHMESourceAccount) {
  sourceActionId.value = source.id
  try { await validateICloudHMESource(source.id); await store.load(); ElMessage.success('Apple 会话有效') }
  catch (error) { await store.load(); showError(error, '会话验证失败') } finally { sourceActionId.value = undefined }
}
async function syncSource(source: ICloudHMESourceAccount) {
  sourceActionId.value = source.id
  try {
    const result = await syncICloudHMEAliases(source.id)
    await store.load()
    ElMessage.success('同步完成：新增 ' + result.imported + '，更新 ' + result.updated)
  } catch (error) { await store.load(); showError(error, '同步主账号失败') } finally { sourceActionId.value = undefined }
}
async function validateAllSources() {
  sourceBatchBusy.value = true
  try {
    const results = await validateAllICloudHMESources()
    await store.load()
    const failed = results.filter((item) => !item.ok).length
    ElMessage.success(failed ? '验证完成，' + failed + ' 个账号需处理' : '全部主账号会话正常')
  } catch (error) { showError(error, '批量验证失败') } finally { sourceBatchBusy.value = false }
}
async function syncAllSources() {
  sourceBatchBusy.value = true
  try {
    const results = await syncAllICloudHMESources()
    await store.load()
    const failed = results.filter((item) => item.error).length
    ElMessage.success(failed ? '同步完成，' + failed + ' 个账号失败' : '全部主账号同步完成')
  } catch (error) { showError(error, '批量同步失败') } finally { sourceBatchBusy.value = false }
}
async function deleteSource(source: ICloudHMESourceAccount) {
  await ElMessageBox.confirm('确定删除主账号“' + source.name + '”？只有没有隐藏邮箱时才能删除。', '删除主账号', { type: 'warning' })
  try { await store.deleteSource(source.id); ElMessage.success('主账号已删除') }
  catch (error) { showError(error, '删除主账号失败') }
}
async function cancelJob(job: ICloudHMEJob) {
  jobActionId.value = job.id
  try { await store.cancelJob(job.id); ElMessage.success('已请求取消任务') }
  catch (error) { showError(error, '取消任务失败') } finally { jobActionId.value = undefined }
}
async function retryJob(job: ICloudHMEJob) {
  jobActionId.value = job.id
  try { await store.retryJob(job.id); ElMessage.success('失败项已重新排队') }
  catch (error) { showError(error, '重试任务失败') } finally { jobActionId.value = undefined }
}
async function editRemark(alias: ICloudHMEAlias) {
  const result = await ElMessageBox.prompt('备注最多 500 个字符，留空表示清除。', '编辑备注 · ' + alias.email, {
    inputValue: alias.remark,
    inputType: 'textarea',
    inputValidator: (value) => Array.from(value.trim()).length <= 500 || '备注最多 500 个字符',
  })
  try { await store.updateRemark(alias.email, result.value); ElMessage.success('备注已保存') }
  catch (error) { showError(error, '保存备注失败') }
}
function openMove() {
  if (!selectedRows.value.length) return ElMessage.warning('请先勾选隐藏邮箱')
  targetGroup.value = store.selectedGroup || ICLOUD_HME_DEFAULT_GROUP
  moveDialogVisible.value = true
}
async function submitMove() {
  busy.value = true
  try {
    await store.moveToGroup(selectedRows.value.map((item) => item.email), targetGroup.value)
    selectedRows.value = []
    moveDialogVisible.value = false
    ElMessage.success('分组已更新')
  } catch (error) { showError(error, '移动分组失败') } finally { busy.value = false }
}
async function lifecycleSelected(action: 'deactivate' | 'reactivate', rows = selectedRows.value) {
  if (!rows.length) return ElMessage.warning('请先勾选隐藏邮箱')
  const actionText = action === 'deactivate' ? '停用' : '恢复'
  await ElMessageBox.confirm('确定在 Apple 侧' + actionText + ' ' + rows.length + ' 个隐藏邮箱？', actionText + '隐藏邮箱', { type: 'warning' })
  busy.value = true
  try {
    const results = await updateICloudHMEAliasLifecycle(rows.map((item) => item.email), action)
    await store.load()
    const failed = results.filter((item) => !item.ok).length
    failed ? ElMessage.warning(actionText + '完成，' + failed + ' 个失败') : ElMessage.success(actionText + '完成')
  } catch (error) { showError(error, actionText + '失败') } finally { busy.value = false }
}
async function permanentlyDeleteAlias(alias: ICloudHMEAlias) {
  const result = await ElMessageBox.prompt(
    '该操作会永久删除 Apple 侧隐藏邮箱、本地记录和本地邮件缓存，且不可恢复。请输入完整隐藏邮箱确认：' + alias.email,
    '永久删除隐藏邮箱',
    { type: 'warning', inputValidator: (value) => value.trim().toLowerCase() === alias.email.toLowerCase() || '输入的邮箱不匹配' },
  )
  busy.value = true
  try {
    await permanentlyDeleteICloudHMEAlias(alias.email, result.value)
    await deleteICloudHMECacheForAlias(alias.email)
    store.aliases = store.aliases.filter((item) => item.email !== alias.email)
    selectedRows.value = selectedRows.value.filter((item) => item.email !== alias.email)
    ElMessage.success('Apple 隐藏邮箱、本地记录与邮件缓存已永久删除')
  } catch (error) { showError(error, '永久删除失败') } finally { busy.value = false }
}
async function handleRowCommand(command: string, alias: ICloudHMEAlias) {
  if (command === 'receive-key') await openReceiveKey(alias)
  if (command === 'deactivate') await lifecycleSelected('deactivate', [alias])
  if (command === 'reactivate') await lifecycleSelected('reactivate', [alias])
  if (command === 'delete') await permanentlyDeleteAlias(alias)
}
async function handleToolbarCommand(command: string) {
  if (command === 'automation-events') await openAutomationEvents()
  if (command === 'scan-gpt') await scanGPTStatus()
  if (command === 'generate-receive-keys') await generateSelectedReceiveKeys()
  if (command === 'export-receive-keys') await exportReceiveKeys()
  if (command === 'deactivate') await lifecycleSelected('deactivate')
  if (command === 'reactivate') await lifecycleSelected('reactivate')
  if (command === 'export-txt') exportAliases('txt')
  if (command === 'export-csv') exportAliases('csv')
}
function exportAliases(format: 'txt' | 'csv') {
  const targets = selectedRows.value.length ? selectedRows.value : filteredAliases.value
  if (!targets.length) return ElMessage.warning('没有可导出的隐藏邮箱')
  const fields = (alias: ICloudHMEAlias) => [alias.email, alias.sourceAccountName, alias.label, alias.group, alias.appleStatus, alias.remark]
  const content = format === 'txt'
    ? targets.map((alias) => fields(alias).join('----')).join('\n')
    : '\uFEFF' + [['隐藏邮箱', '主账号', 'Apple标签', '分组', '状态', '备注'], ...targets.map(fields)]
      .map((row) => row.map((value) => '"' + value.replaceAll('"', '""') + '"').join(',')).join('\n')
  const blob = new Blob([content], { type: format === 'csv' ? 'text/csv;charset=utf-8' : 'text/plain;charset=utf-8' })
  const link = document.createElement('a')
  link.href = URL.createObjectURL(blob)
  link.download = 'icloud-hme-' + new Date().toISOString().slice(0, 10) + '.' + format
  link.click()
  URL.revokeObjectURL(link.href)
}
async function openMail(alias: ICloudHMEAlias) {
  if (!alias.mailReady) {
    const source = store.sources.find((item) => item.id === alias.sourceAccountId)
    if (source) {
      ElMessage.info('请先为该 Apple 主账号配置 App 专用密码，保存后即可在后台查看邮件')
      openCredential(source, 'appPassword')
    } else {
      ElMessage.warning('未找到该隐藏邮箱所属的 Apple 主账号')
    }
    return
  }
  mailAlias.value = alias
  mailVisible.value = true
  mailMessages.value = await listCachedICloudHMEMessages(alias.email)
  mailNextCursor.value = ''
  mailKeyword.value = ''
  selectedMail.value = mailMessages.value[0]
  mailDetail.value = undefined
  verificationCode.value = ''
  if (selectedMail.value) await selectMail(selectedMail.value, true)
  await refreshMailList()
}
async function openSelectedMail() {
  if (selectedRows.value.length !== 1) return ElMessage.warning('请选择一个隐藏邮箱查看邮件')
  await openMail(selectedRows.value[0])
}
async function openMailFromReceiveKey() {
  const email = receiveKeyRecord.value?.email
  if (!email) return
  const alias = store.aliases.find((item) => item.email === email)
  if (!alias) return ElMessage.warning('隐藏邮箱记录不存在')
  receiveKeyDialogVisible.value = false
  await openMail(alias)
}
async function refreshMailList(loadMore = false) {
  if (!mailAlias.value || mailListLoading.value) return
  mailListLoading.value = true
  try {
    const result = await listICloudHMEMessages(mailAlias.value.email, 20, loadMore ? mailNextCursor.value : '')
    await cacheICloudHMEMessages(mailAlias.value.email, result.messages)
    mailNextCursor.value = result.nextCursor ?? ''
    if (loadMore) {
      const merged = new Map(mailMessages.value.map((message) => [message.id, message]))
      result.messages.forEach((message) => merged.set(message.id, message))
      mailMessages.value = Array.from(merged.values()).sort((left, right) => Date.parse(right.receivedAt) - Date.parse(left.receivedAt))
    } else {
      mailMessages.value = await listCachedICloudHMEMessages(mailAlias.value.email, mailKeyword.value)
    }
    if (!selectedMail.value && mailMessages.value[0]) await selectMail(mailMessages.value[0], true)
  } catch (error) {
    mailMessages.value.length ? ElMessage.warning('在线刷新失败，当前显示本地缓存') : showError(error, '获取邮件列表失败')
  } finally { mailListLoading.value = false }
}
async function selectMail(summary: ICloudHMEMailSummary, cacheFirst = false) {
  if (!mailAlias.value) return
  selectedMail.value = summary
  mailDetail.value = undefined
  verificationCode.value = summary.verificationCode ?? ''
  const cached = await getCachedICloudHMEBody(mailAlias.value.email, summary.id)
  if (cached) {
    mailDetail.value = cached.message
    verificationCode.value = cached.verificationCode ?? verificationCode.value
    if (cacheFirst) return
  }
  mailBodyLoading.value = true
  try {
    const result = await getICloudHMEMessage(mailAlias.value.email, summary.id)
    mailDetail.value = result.message
    verificationCode.value = result.verificationCode ?? ''
    await cacheICloudHMEBody(mailAlias.value.email, result.message, result.verificationCode)
    mailMessages.value = await listCachedICloudHMEMessages(mailAlias.value.email, mailKeyword.value)
  } catch (error) { if (!cached) showError(error, '获取邮件正文失败') } finally { mailBodyLoading.value = false }
}
async function refreshCode() {
  if (!mailAlias.value || mailCodeLoading.value) return
  mailCodeLoading.value = true
  try {
    const result = await refreshICloudHMECode(mailAlias.value.email)
    verificationCode.value = result.code ?? ''
    if (result.message) {
      mailDetail.value = result.message
      selectedMail.value = {
        id: result.message.id, subject: result.message.subject, from: result.message.from,
        to: result.message.to, cc: result.message.cc, receivedAt: result.message.receivedAt,
        isRead: result.message.isRead, hasAttachments: false, verificationCode: result.code,
      }
      await cacheICloudHMEBody(mailAlias.value.email, result.message, result.code)
      mailMessages.value = await listCachedICloudHMEMessages(mailAlias.value.email, mailKeyword.value)
    }
    ElMessage.success(result.code ? '已获取最新验证码' : '最近邮件中未识别到验证码')
  } catch (error) { showError(error, '刷新验证码失败') } finally { mailCodeLoading.value = false }
}
async function renameGroup(group: ICloudHMEGroup) {
  const result = await ElMessageBox.prompt('请输入新的分组名称', '重命名隐藏邮箱分组', {
    inputValue: group.name, inputValidator: (value) => Boolean(value.trim()) || '分组名称不能为空',
  })
  if (result.value.trim() === group.name) return
  renamingGroupId.value = group.id
  try { await store.renameGroup(group.id, group.name, result.value.trim()); ElMessage.success('分组已重命名') }
  catch (error) { showError(error, '重命名分组失败') } finally { renamingGroupId.value = undefined }
}
async function deleteGroup(group: ICloudHMEGroup) {
  await ElMessageBox.confirm('确定删除空分组“' + group.name + '”？', '删除隐藏邮箱分组', { type: 'warning' })
  deletingGroupId.value = group.id
  try { await store.deleteGroup(group.id, group.name); ElMessage.success('分组已删除') }
  catch (error) { showError(error, '删除分组失败') } finally { deletingGroupId.value = undefined }
}
function dragStart(group: ICloudHMEGroup, event: DragEvent) {
  draggingGroupId.value = group.id
  event.dataTransfer?.setData('text/plain', String(group.id))
}
async function dropGroup(target: ICloudHMEGroup, event: DragEvent) {
  event.preventDefault()
  const sourceId = draggingGroupId.value ?? Number(event.dataTransfer?.getData('text/plain'))
  draggingGroupId.value = undefined
  if (!sourceId || sourceId === target.id) return
  const groups = [...store.groups]
  const from = groups.findIndex((group) => group.id === sourceId)
  const to = groups.findIndex((group) => group.id === target.id)
  if (from < 0 || to < 0) return
  const [source] = groups.splice(from, 1)
  if (!source) return
  groups.splice(to, 0, source)
  try { await store.reorderGroups(groups.map((group) => group.id)) }
  catch (error) { await store.load(); showError(error, '分组排序失败') }
}
</script>
<template>
  <section class="faka-shell icloud-hme-shell">
    <aside class="faka-sidebar">
      <div class="faka-brand"><el-icon><Link /></el-icon><span>MailBox</span></div>
      <nav class="faka-nav">
        <section class="sidebar-panel group-panel">
          <div class="sidebar-panel-head"><span><el-icon><FolderOpened /></el-icon>隐藏邮箱分组</span><strong>{{ store.groups.length }}</strong></div>
          <div class="sidebar-panel-body group-panel-body">
            <button class="faka-nav-item sidebar-list-row pinned-row" :class="{ active: !store.selectedGroup }" @click="store.selectedGroup = ''">
              <el-icon><FolderOpened /></el-icon><span>全部隐藏邮箱</span><small>{{ store.aliases.length }}</small>
            </button>
            <button v-for="group in store.groups" :key="group.id" class="faka-nav-item sidebar-list-row group-nav-item"
              :class="{ active: store.selectedGroup === group.name, dragging: draggingGroupId === group.id }"
              draggable="true" @click="store.selectedGroup = group.name" @dragstart="dragStart(group, $event)"
              @dragover.prevent @drop="dropGroup(group, $event)" @dragend="draggingGroupId = undefined">
              <span class="drag-handle">⋮⋮</span><el-icon><FolderOpened /></el-icon><span>{{ group.name }}</span>
              <div class="group-actions"><small>{{ groupCount(group) }}</small>
                <el-button v-if="group.name !== ICLOUD_HME_DEFAULT_GROUP" link :icon="EditPen" :loading="renamingGroupId === group.id" @click.stop="renameGroup(group)" />
                <el-button v-if="canDeleteGroup(group)" link :icon="Delete" :loading="deletingGroupId === group.id" @click.stop="deleteGroup(group)" />
              </div>
            </button>
          </div>
        </section>
      </nav>
      <div class="faka-total-card"><el-icon><Files /></el-icon><span>隐藏邮箱</span><strong>{{ store.aliases.length }}</strong></div>
    </aside>

    <main class="faka-main">
      <MailboxTopbar :search-value="keyword" workspace-mode="accounts" placeholder="搜索隐藏邮箱、主账号、标签、分组或备注..." @search-input="keyword = $event" />
      <section class="faka-card">
        <div class="hme-automation-summary">
          <div><span>可售库存</span><strong>{{ automation?.availableCount ?? 0 }} / {{ automation?.targetAvailableCount ?? 20 }}</strong></div>
          <div><span>待创建</span><strong>{{ automation?.pendingCount ?? 0 }}</strong></div>
          <div><span>预留 / 已售</span><strong>{{ automation?.reservedCount ?? 0 }} / {{ automation?.soldCount ?? 0 }}</strong></div>
          <div><span>自动化</span><el-tag :type="automation?.enabled ? 'success' : 'info'">{{ automation?.enabled ? '运行中' : '已暂停' }}</el-tag></div>
          <div><span>下次执行</span><strong>{{ automation?.nextCreateAt ? formatDateTime(automation.nextCreateAt) : '等待可用主账号' }}</strong></div>
          <div><span>当前探测区间</span><strong>{{ primaryProbeSource ? probeRangeText(primaryProbeSource.probeStage) : '等待主账号' }}</strong></div>
          <div><span>暂定稳定区间</span><strong>{{ primaryProbeSource ? probeStableText(primaryProbeSource.probeStableStage) : '采样中' }}</strong></div>
        </div>
        <div class="faka-action-row hme-action-row">
          <el-button :icon="Setting" @click="sourceDialogVisible = true">主账号管理</el-button>
          <el-button type="primary" :icon="Setting" @click="openAutomationSettings">自动补货设置</el-button>
          <el-button :icon="CopyDocument" @click="copySelected">复制邮箱</el-button>
          <el-button type="primary" plain :icon="Link" :loading="receiveKeyBusy" :disabled="!selectedRows.length && !filteredAliases.length" @click="copyLatestReceiveURLs">复制最新取件 URL</el-button>
          <el-button :icon="Files" :loading="receiveKeyBusy" :disabled="!selectedRows.length && !filteredAliases.length" @click="copyLatestReceiveURLs('email-url')">复制邮箱---取件 URL</el-button>
          <el-button :icon="View" :disabled="selectedRows.length !== 1" @click="openSelectedMail">查看邮件</el-button>
          <el-button :icon="FolderOpened" :disabled="!selectedRows.length" @click="openMove">移动分组</el-button>
          <el-dropdown @command="markInventory">
            <el-button :disabled="!selectedRows.length">库存状态<el-icon class="el-icon--right"><More /></el-icon></el-button>
            <template #dropdown><el-dropdown-menu><el-dropdown-item command="available">恢复可售</el-dropdown-item><el-dropdown-item command="reserved">标记预留</el-dropdown-item><el-dropdown-item command="sold">标记已售</el-dropdown-item></el-dropdown-menu></template>
          </el-dropdown>
          <el-dropdown @command="handleToolbarCommand">
            <el-button :loading="gptScanBusy || receiveKeyBusy || busy">更多操作<el-icon class="el-icon--right"><More /></el-icon></el-button>
            <template #dropdown><el-dropdown-menu>
              <el-dropdown-item command="automation-events" :icon="Document">自动补货运行记录</el-dropdown-item>
              <el-dropdown-item command="scan-gpt" :icon="Refresh">扫描 GPT 状态</el-dropdown-item>
              <el-dropdown-item command="generate-receive-keys" :icon="Key" :disabled="!selectedRows.length">生成收件密钥</el-dropdown-item>
              <el-dropdown-item command="export-receive-keys" :icon="Document">导出收件密钥</el-dropdown-item>
              <el-dropdown-item command="export-txt" divided>导出 TXT</el-dropdown-item>
              <el-dropdown-item command="export-csv">导出 CSV</el-dropdown-item>
              <el-dropdown-item command="deactivate" divided :disabled="!selectedRows.length">Apple 停用</el-dropdown-item>
              <el-dropdown-item command="reactivate" :disabled="!selectedRows.length">Apple 恢复</el-dropdown-item>
            </el-dropdown-menu></template>
          </el-dropdown>
        </div>
        <div class="hme-filter-row">
          <el-select v-model="sourceFilter" style="width:190px"><el-option label="全部主账号" value="all" /><el-option v-for="source in store.sources" :key="source.id" :label="source.name" :value="source.id" /></el-select>
          <el-select v-model="statusFilter" style="width:160px"><el-option label="全部状态" value="all" /><el-option label="已启用" value="active" /><el-option label="已停用" value="inactive" /><el-option label="已永久删除" value="deleted" /><el-option label="状态未知" value="unknown" /></el-select>
          <el-select v-model="gptStatusFilter" style="width:150px"><el-option label="全部 GPT 状态" value="all" /><el-option label="未开通" value="unregistered" /><el-option label="PLUS" value="plus" /><el-option label="已封号" value="deactivated" /></el-select>
          <span>{{ filteredAliases.length }} 个结果</span>
        </div>
        <div class="account-selection-hint" :class="{ active: selectedRows.length }"><strong>已选 {{ selectedRows.length }} 个隐藏邮箱</strong><span>复制邮箱、取件 URL、邮箱---取件 URL、停用、恢复和移动优先作用于已选项；永久删除仅支持单个</span></div>
        <el-table v-loading="store.loading || busy || receiveKeyBusy" :data="pagedAliases" row-key="email" class="faka-account-table" height="calc(100vh - 282px)" @selection-change="handleSelection">
          <el-table-column type="selection" width="52" align="center" />
          <el-table-column label="#" width="64" align="center"><template #default="{ $index }"><span class="row-number">{{ (page - 1) * pageSize + $index + 1 }}</span></template></el-table-column>
          <el-table-column label="隐藏邮箱" min-width="260" show-overflow-tooltip><template #default="{ row }"><div class="copy-cell" :class="{ copied: copiedValues.has(row.email) }"><span>{{ row.email }}</span><el-button link :icon="CopyDocument" @click.stop="copyValue(row.email)" /></div></template></el-table-column>
          <el-table-column prop="sourceAccountName" label="Apple 主账号" min-width="145" show-overflow-tooltip />
          <el-table-column prop="label" label="Apple 标签" min-width="165" show-overflow-tooltip />
          <el-table-column label="GPT账号" width="220"><template #default="{ row }"><div class="hme-gpt-status-cell"><div><el-tag :type="gptStatusType(row.gptStatus)" effect="light">{{ gptStatusText(row.gptStatus) }}</el-tag></div><span v-if="row.gptPlusActivatedAt">开通 {{ formatDateTime(row.gptPlusActivatedAt) }}</span><span v-if="row.gptDeactivatedAt">封号 {{ formatDateTime(row.gptDeactivatedAt) }}</span><strong v-if="gptSurvivalText(row)">存活 {{ gptSurvivalText(row) }}</strong><small v-if="row.gptScanError" class="danger-text">{{ row.gptScanError }}</small></div></template></el-table-column>
          <el-table-column prop="group" label="分组" width="125" />
          <el-table-column label="备注" min-width="160" show-overflow-tooltip><template #default="{ row }"><div class="remark-cell"><span :class="{ muted: !row.remark }">{{ row.remark || '无备注' }}</span><el-button link :icon="EditPen" @click.stop="editRemark(row)" /></div></template></el-table-column>
          <el-table-column label="收件密钥" width="220" align="center"><template #default="{ row }"><div class="hme-receive-key-cell"><el-tag :type="row.receiveKeyConfigured ? 'success' : 'info'" effect="plain">{{ row.receiveKeyConfigured ? '已配置' : '未配置' }}</el-tag><el-button link :icon="Link" :loading="receiveURLBusyEmails.has(row.email)" :disabled="row.appleStatus !== 'active'" @click.stop="copyLatestReceiveURL(row)">复制 URL</el-button><el-button link :icon="Key" @click.stop="openReceiveKey(row)">{{ row.receiveKeyConfigured ? '密钥' : '生成' }}</el-button></div></template></el-table-column>
          <el-table-column label="接码验证码" width="230" align="center"><template #default="{ row }">
            <div v-if="boundSMSBinding(row)" class="hme-sms-cell">
              <div class="hme-sms-number">
                <span>{{ boundSMSBinding(row)?.account.phone }}</span>
                <el-tag
                  v-if="boundSMSBinding(row)?.account.status === 'invalid'"
                  size="small"
                  type="danger"
                >
                  已失效
                </el-tag>
                <el-button
                  link
                  :icon="CopyDocument"
                  title="复制手机号"
                  @click.stop="copyValue(boundSMSBinding(row)!.account.phone, '手机号')"
                />
              </div>
              <small>绑定 {{ formatDateTime(boundSMSBinding(row)!.binding.boundAt) }}</small>
              <div>
                <el-button
                  link
                  type="primary"
                  :icon="Message"
                  :disabled="boundSMSBinding(row)?.account.status === 'invalid'"
                  @click.stop="openSMSCode(row)"
                >
                  查看验证码
                </el-button>
                <el-button link @click.stop="openSMSBinding(row)">更换</el-button>
                <el-button
                  link
                  :type="boundSMSBinding(row)?.account.status === 'active' ? 'danger' : 'success'"
                  :loading="smsStatusUpdatingPhone === boundSMSBinding(row)?.account.phone"
                  @click.stop="toggleSMSStatus(boundSMSBinding(row)!.account)"
                >
                  {{ boundSMSBinding(row)?.account.status === 'active' ? '失效' : '恢复' }}
                </el-button>
              </div>
            </div>
            <el-button v-else link type="primary" :icon="Link" @click.stop="openSMSBinding(row)">绑定接码</el-button>
          </template></el-table-column>
          <el-table-column label="库存" width="90" align="center"><template #default="{ row }"><el-tag :type="inventoryType(row.inventoryStatus)" effect="plain">{{ inventoryText(row.inventoryStatus) }}</el-tag></template></el-table-column>
          <el-table-column label="Apple 状态" width="130" align="center"><template #default="{ row }"><el-tag :type="aliasStatusType(row.appleStatus)" effect="light">{{ aliasStatusText(row.appleStatus) }}</el-tag></template></el-table-column>
          <el-table-column label="最后同步" width="155"><template #default="{ row }">{{ row.lastSyncedAt ? formatDateTime(row.lastSyncedAt) : '未同步' }}</template></el-table-column>
          <el-table-column label="操作" width="190" fixed="right" align="center"><template #default="{ row }">
            <el-button size="small" type="primary" :icon="View" :disabled="row.appleStatus === 'deleted'" @click.stop="openMail(row)">查看邮件</el-button>
            <el-dropdown trigger="click" @command="handleRowCommand($event, row)"><el-button size="small" :icon="More" />
              <template #dropdown><el-dropdown-menu>
                <el-dropdown-item command="receive-key" :icon="Key">收件密钥与 API</el-dropdown-item>
                <el-dropdown-item v-if="row.appleStatus === 'active'" command="deactivate">Apple 停用</el-dropdown-item>
                <el-dropdown-item v-if="row.appleStatus === 'inactive'" command="reactivate">Apple 恢复</el-dropdown-item>
                <el-dropdown-item command="delete" divided :icon="Delete">永久删除</el-dropdown-item>
              </el-dropdown-menu></template>
            </el-dropdown>
          </template></el-table-column>
        </el-table>
        <div class="faka-pagination"><span>Total {{ filteredAliases.length }}</span><el-pagination v-model:current-page="page" v-model:page-size="pageSize" size="small" layout="sizes, prev, pager, next" :total="filteredAliases.length" :page-sizes="[20, 50, 100]" /></div>
      </section>
    </main>
    <el-dialog v-model="automationDialogVisible" title="自动补货设置" width="560px">
      <el-form label-position="top">
        <el-alert title="系统从 5–7 分钟开始探测；连续成功后逐步缩短到 3–5、2–3 分钟。遇到 Apple 限流后只做单次探测，按 5/8/12/20/30/45 分钟逐级等待。" type="info" :closable="false" />
        <el-form-item label="自动补货"><el-switch v-model="automationForm.enabled" active-text="启用" inactive-text="暂停" /></el-form-item>
        <el-form-item label="目标可售库存"><el-input-number v-model="automationForm.targetAvailableCount" :min="0" :max="10000" /></el-form-item>
        <el-form-item label="新邮箱分组"><el-select v-model="automationForm.targetGroup" filterable allow-create style="width:100%"><el-option v-for="group in store.groups" :key="group.id" :label="group.name" :value="group.name" /></el-select></el-form-item>
        <el-form-item label="Apple 标签前缀"><el-input v-model="automationForm.labelPrefix" maxlength="80" show-word-limit /></el-form-item>
      </el-form>
      <template #footer><el-button @click="automationDialogVisible = false">取消</el-button><el-button type="primary" :loading="automationSaving" @click="saveAutomation">保存</el-button></template>
    </el-dialog>

    <el-dialog v-model="automationEventsVisible" title="自动补货运行记录" width="960px">
      <el-table :data="automationEvents" max-height="520">
        <el-table-column label="时间" width="170"><template #default="{ row }">{{ formatDateTime(row.createdAt) }}</template></el-table-column>
        <el-table-column prop="sourceName" label="主账号" width="145"><template #default="{ row }">{{ row.sourceName || '系统' }}</template></el-table-column>
        <el-table-column label="结果" width="110"><template #default="{ row }"><el-tag :type="row.result === 'success' ? 'success' : row.result === 'deferred' ? 'warning' : row.result === 'failed' ? 'danger' : 'info'">{{ row.result }}</el-tag></template></el-table-column>
        <el-table-column label="探测区间" width="125"><template #default="{ row }">{{ eventProbeRange(row) }}</template></el-table-column>
        <el-table-column label="实际间隔" width="115"><template #default="{ row }">{{ formatProbeDuration(row.intervalSeconds) }}</template></el-table-column>
        <el-table-column label="恢复耗时" width="115"><template #default="{ row }">{{ formatProbeDuration(row.recoverySeconds) }}</template></el-table-column>
        <el-table-column prop="errorCode" label="分类" min-width="165" show-overflow-tooltip />
        <el-table-column prop="message" label="说明" min-width="210" show-overflow-tooltip />
        <el-table-column label="下次执行" width="170"><template #default="{ row }">{{ row.nextAttemptAt ? formatDateTime(row.nextAttemptAt) : '—' }}</template></el-table-column>
      </el-table>
    </el-dialog>

    <el-dialog v-model="sourceDialogVisible" title="Apple 主账号管理" width="1180px" class="hme-source-dialog">
      <div class="hme-source-toolbar">
        <div class="hme-source-create"><el-input v-model="sourceForm.name" placeholder="账号名称" /><el-input v-model="sourceForm.appleIdEmail" placeholder="Apple ID" /><el-input v-model="sourceForm.icloudEmail" placeholder="实际 iCloud 邮箱" /><el-select v-model="sourceForm.host"><el-option label="国际区" value="icloud.com" /><el-option label="中国区" value="icloud.com.cn" /></el-select><el-button type="primary" :loading="sourceCreating" @click="createSource">添加</el-button></div>
        <div class="hme-source-batch"><el-button :loading="sourceBatchBusy" @click="validateAllSources">验证全部</el-button><el-button :loading="sourceBatchBusy" @click="syncAllSources">同步全部</el-button></div>
      </div>
      <el-table :data="store.sources" max-height="460">
        <el-table-column prop="name" label="名称" min-width="105" />
        <el-table-column prop="appleIdEmail" label="Apple ID" min-width="175" show-overflow-tooltip />
        <el-table-column prop="icloudEmail" label="收件邮箱" min-width="175" show-overflow-tooltip />
        <el-table-column label="健康" width="135"><template #default="{ row }"><el-tag :type="sourceStatusType(row.status)">{{ sourceStatusText(row.status) }}</el-tag><small v-if="row.statusReason" class="hme-source-error">{{ row.statusReason }}</small></template></el-table-column>
        <el-table-column label="iCloud+" width="105" align="center"><template #default="{ row }"><el-tag :type="row.status === 'active' ? 'success' : row.status === 'icloud_plus_required' ? 'danger' : 'info'">{{ row.status === 'active' ? '已验证' : row.status === 'icloud_plus_required' ? '未开通' : '未验证' }}</el-tag></template></el-table-column>
        <el-table-column label="别名" width="65" align="center"><template #default="{ row }">{{ row.aliasTotal }}</template></el-table-column>
        <el-table-column label="最近活动" min-width="170"><template #default="{ row }"><div class="hme-source-dates"><span>验证 {{ row.lastValidatedAt ? formatDateTime(row.lastValidatedAt) : '无' }}</span><span>同步 {{ row.lastSyncedAt ? formatDateTime(row.lastSyncedAt) : '无' }}</span><span>创建 {{ row.lastCreatedAt ? formatDateTime(row.lastCreatedAt) : '无' }}</span><span v-if="row.lastErrorAt">异常 {{ formatDateTime(row.lastErrorAt) }}</span></div></template></el-table-column>
        <el-table-column label="自动补货" min-width="235"><template #default="{ row }"><div class="hme-source-dates">
          <span>{{ row.automationEnabled ? (row.status === 'cooldown' ? '冷却中' : row.probeRecoveryMode ? '恢复验证中' : '可参与') : '已暂停' }}</span>
          <span>当前 {{ probeRangeText(row.probeStage) }} · {{ row.probeSuccessStreak }}/{{ row.probeSuccessTarget }} 次</span>
          <span>暂定稳定 {{ probeStableText(row.probeStableStage) }}</span>
          <span v-if="row.probeLastIntervalSeconds">上次间隔 {{ formatProbeDuration(row.probeLastIntervalSeconds) }}</span>
          <span v-if="row.probeLastRecoverySeconds">上次恢复 {{ formatProbeDuration(row.probeLastRecoverySeconds) }}</span>
          <span v-if="row.nextCreateAt">下次 {{ formatDateTime(row.nextCreateAt) }}</span>
          <span v-if="row.lastLimitAt">限流 {{ formatDateTime(row.lastLimitAt) }}</span>
        </div></template></el-table-column>
        <el-table-column label="操作" width="390" fixed="right"><template #default="{ row }">
          <el-button size="small" :icon="Link" @click="openCredential(row, 'cookies')">Cookie</el-button>
          <el-button size="small" @click="openCredential(row, 'login')">登录</el-button>
          <el-button size="small" :icon="Key" @click="openCredential(row, 'appPassword')">收件密码</el-button>
          <el-button size="small" :icon="Check" :loading="sourceActionId === row.id" :disabled="!row.cookieConfigured" @click="validateSource(row)">验证</el-button>
          <el-button size="small" :icon="Refresh" :loading="sourceActionId === row.id" :disabled="!row.cookieConfigured" @click="syncSource(row)">同步</el-button>
          <el-button size="small" type="danger" link :icon="Delete" @click="deleteSource(row)" />
        </template></el-table-column>
      </el-table>
    </el-dialog>

    <el-dialog v-model="credentialDialogVisible" @closed="clearCredentialForm" :title="credentialSource ? '配置 · ' + credentialSource.name : '配置主账号'" width="520px">
      <el-form label-position="top">
        <template v-if="credentialMode === 'cookies'"><el-form-item label="Cookie JSON 或 Cookie Header"><el-input v-model="credentialForm.cookies" type="textarea" :rows="8" /></el-form-item></template>
        <template v-else-if="credentialMode === 'login'">
          <el-alert title="Apple 密码和验证码只保存在本次登录内存中，不会入库或写日志。" type="info" :closable="false" />
          <el-form-item v-if="loginStep === 'password'" label="Apple 密码"><el-input v-model="credentialForm.password" type="password" show-password @keyup.enter="saveCredential" /></el-form-item>
          <el-form-item v-else label="双重认证验证码"><el-input v-model="credentialForm.otp" maxlength="8" @keyup.enter="saveCredential" /></el-form-item>
        </template>
        <template v-else><el-alert title="使用 Apple 账户生成的 App 专用密码供 IMAP 收件。" type="info" :closable="false" /><el-form-item label="App 专用密码"><el-input v-model="credentialForm.appPassword" type="password" show-password /></el-form-item></template>
      </el-form>
      <template #footer><el-button @click="credentialDialogVisible = false">取消</el-button><el-button type="primary" :loading="credentialSaving" @click="saveCredential">{{ credentialMode === 'login' && loginStep === 'otp' ? '验证并完成' : credentialMode === 'appPassword' ? '保存' : '继续' }}</el-button></template>
    </el-dialog>

    <el-dialog v-model="jobsDialogVisible" title="隐藏邮箱创建任务" width="1000px" class="hme-jobs-dialog">
      <div class="hme-jobs-layout">
        <el-table :data="store.jobs" max-height="430" highlight-current-row @current-change="selectJob">
          <el-table-column label="任务" min-width="170"><template #default="{ row }"><strong>{{ row.labelPrefix }}</strong><small>{{ row.mode === 'pool' ? '健康账号池' : '固定主账号' }} · {{ row.requestedCount }} 个</small></template></el-table-column>
          <el-table-column label="状态" width="110"><template #default="{ row }"><el-tag :type="jobStatusType(row.status)">{{ jobStatusText(row.status) }}</el-tag></template></el-table-column>
          <el-table-column label="进度" min-width="190"><template #default="{ row }"><el-progress :percentage="jobProgress(row)" :stroke-width="8" /><small>成功 {{ row.completedCount }} · 失败 {{ row.failedCount }} · 取消 {{ row.cancelledCount }}</small></template></el-table-column>
          <el-table-column label="创建时间" width="165"><template #default="{ row }">{{ formatDateTime(row.createdAt) }}</template></el-table-column>
          <el-table-column label="操作" width="130"><template #default="{ row }"><el-button v-if="['pending','running'].includes(row.status)" link :loading="jobActionId === row.id" @click.stop="cancelJob(row)">取消</el-button><el-button v-if="row.failedCount" link type="primary" :loading="jobActionId === row.id" @click.stop="retryJob(row)">重试失败项</el-button></template></el-table-column>
        </el-table>
        <section v-if="selectedJob" class="hme-job-detail">
          <div class="sidebar-panel-head"><span>{{ selectedJob.labelPrefix }} 明细</span><strong>{{ selectedJob.items?.length || 0 }}</strong></div>
          <el-scrollbar height="390px"><div v-for="item in selectedJob.items" :key="item.id" class="hme-job-item"><span>#{{ item.sequence }}</span><strong>{{ item.email || item.label }}</strong><el-tag size="small" :type="item.status === 'completed' ? 'success' : item.status === 'failed' ? 'danger' : 'info'">{{ item.status }}</el-tag><small v-if="item.errorMessage">{{ item.errorMessage }}</small></div></el-scrollbar>
        </section>
      </div>
    </el-dialog>
    <el-dialog v-model="moveDialogVisible" title="移动隐藏邮箱分组" width="420px">
      <el-select v-model="targetGroup" filterable allow-create style="width:100%"><el-option v-for="group in store.groups" :key="group.id" :label="group.name" :value="group.name" /></el-select>
      <template #footer><el-button @click="moveDialogVisible = false">取消</el-button><el-button type="primary" :loading="busy" @click="submitMove">确定</el-button></template>
    </el-dialog>

    <el-dialog v-model="receiveKeyDialogVisible" title="隐藏邮箱收件密钥" width="660px" class="hme-receive-key-dialog" @closed="clearReceiveKey">
      <template v-if="receiveKeyRecord">
        <el-alert title="该密钥仅用于 MailBox 公开收件 API，不是 Apple 邮箱密码。请仅交付给对应购买者。" type="warning" :closable="false" />
        <div class="hme-receive-key-block">
          <span>隐藏邮箱</span>
          <div><code>{{ receiveKeyRecord.email }}</code><el-button link :icon="CopyDocument" @click="copyValue(receiveKeyRecord.email)">复制</el-button></div>
        </div>
        <div class="hme-receive-key-block">
          <span>收件密钥</span>
          <div><code>{{ receiveKeyRecord.key }}</code><el-button link :icon="CopyDocument" @click="copyValue(receiveKeyRecord.key, '收件密钥')">复制</el-button></div>
        </div>
        <div class="hme-receive-key-links">
          <el-button type="primary" :icon="View" @click="openMailFromReceiveKey">后台查看邮件</el-button>
          <el-button :icon="Link" @click="copyReceiveURL('latest')">复制最新邮件 URL</el-button>
          <el-button :icon="Link" @click="copyReceiveURL('history')">复制历史邮件 URL</el-button>
        </div>
        <small class="hme-receive-key-time">更新于 {{ formatDateTime(receiveKeyRecord.updatedAt) }}</small>
      </template>
      <template #footer><el-button type="danger" plain :loading="receiveKeyBusy" @click="resetReceiveKey">重置密钥</el-button><el-button type="primary" @click="receiveKeyDialogVisible = false">完成</el-button></template>
    </el-dialog>

    <el-dialog v-model="mailVisible" width="1120px" class="hme-mail-dialog">
      <template #header><div class="icloud-dialog-heading"><span>iCloud 隐藏邮箱 · {{ mailAlias?.email }}</span><h2>邮件历史</h2></div></template>
      <div class="hme-mail-toolbar">
        <el-input v-model="mailKeyword" clearable placeholder="本地搜索主题、发件人或验证码" />
        <el-button :icon="Refresh" :loading="mailListLoading" @click="refreshMailList(false)">刷新邮件</el-button>
        <el-button type="primary" :loading="mailCodeLoading" @click="refreshCode">刷新验证码</el-button>
        <div v-if="verificationCode" class="hme-code"><span>验证码</span><strong>{{ verificationCode }}</strong><el-button link :icon="CopyDocument" @click="copyValue(verificationCode, '验证码')" /></div>
      </div>
      <div class="hme-mail-layout">
        <section class="hme-mail-list">
          <el-scrollbar>
            <button v-for="message in filteredMailMessages" :key="message.id" class="hme-mail-row" :class="{ active: selectedMail?.id === message.id }" @click="selectMail(message)">
              <div><strong>{{ formatAddress(message.from) }}</strong><time>{{ formatDateTime(message.receivedAt) }}</time></div>
              <h3>{{ message.subject || '(无主题)' }}</h3>
              <span v-if="message.verificationCode">验证码 {{ message.verificationCode }}</span>
            </button>
            <el-empty v-if="!mailListLoading && !filteredMailMessages.length" description="暂无邮件" />
          </el-scrollbar>
          <el-button v-if="mailNextCursor" link :loading="mailListLoading" @click="refreshMailList(true)">加载更多</el-button>
        </section>
        <section class="hme-mail-reader" v-loading="mailBodyLoading">
          <template v-if="mailDetail">
            <header><h2>{{ mailDetail.subject || '(无主题)' }}</h2><p><strong>{{ formatAddress(mailDetail.from) }}</strong><span>收件人 {{ formatAddress(mailDetail.to) }}</span><time>{{ formatDateTime(mailDetail.receivedAt) }}</time></p></header>
            <div class="icloud-reader-panel">
              <iframe v-if="mailHtml" class="icloud-mail-frame" :srcdoc="mailHtml" sandbox="allow-popups allow-popups-to-escape-sandbox" title="隐藏邮箱邮件正文" />
              <div v-else class="mail-body plain plain-mail-paragraphs icloud-mail-body"><p v-for="(block, index) in mailBlocks" :key="index" :class="{ 'plain-mail-heading': block.kind === 'heading' }">{{ block.text }}</p><p v-if="!mailBlocks.length" class="plain-mail-empty">暂无正文内容</p></div>
            </div>
          </template>
          <el-empty v-else description="选择一封邮件查看正文" />
        </section>
      </div>
    </el-dialog>

    <el-dialog v-model="smsCodeVisible" title="隐藏邮箱短信验证码" width="620px" class="sms-code-dialog">
      <div class="sms-code-head">
        <div><span>隐藏邮箱</span><strong>{{ smsCodeAlias?.email }}</strong></div>
        <el-tag :type="smsCodePolling ? 'success' : 'info'">{{ smsCodePolling ? '每 5 秒自动刷新' : '已停止自动刷新' }}</el-tag>
      </div>
      <div class="hme-sms-phone">
        <span>接码号码</span>
        <div>
          <strong>{{ smsCodeAccount?.phone }}</strong>
          <el-button
            link
            :icon="CopyDocument"
            title="复制手机号"
            @click="copyValue(smsCodeAccount?.phone || '', '手机号')"
          />
        </div>
      </div>
      <div class="sms-code-result" :class="{ available: smsCodeResult?.code }">
        <span>最新验证码</span>
        <strong>{{ smsCodeResult?.code || '等待短信' }}</strong>
        <el-button v-if="smsCodeResult?.code" type="primary" :icon="CopyDocument" @click="copySMSCode">复制验证码</el-button>
      </div>
      <div class="sms-message-panel">
        <span>最新短信</span>
        <p>{{ smsCodeResult?.message || '正在查询最新短信…' }}</p>
        <small v-if="smsCodeResult?.checkedAt">最后刷新 {{ formatDateTime(smsCodeResult.checkedAt) }}</small>
      </div>
      <template #footer>
        <el-button v-if="smsCodePolling" @click="stopSMSCodePolling">停止刷新</el-button>
        <el-button v-else :icon="Refresh" @click="startSMSCodePolling">开始实时刷新</el-button>
        <el-button type="primary" :icon="Refresh" :loading="smsCodeLoading" @click="refreshSMSCode">立即刷新</el-button>
      </template>
    </el-dialog>

    <el-dialog v-model="smsBindingVisible" title="选择接码账号" width="760px" class="hme-sms-binding-dialog">
      <div class="hme-sms-binding-head">
        <div><span>隐藏邮箱</span><strong>{{ smsBindingAlias?.email }}</strong></div>
        <el-input v-model="smsBindingKeyword" clearable placeholder="搜索手机号、绑定邮箱或备注" />
      </div>
      <div class="hme-sms-picker-list">
        <div
          v-for="account in sortedSMSAccounts"
          :key="account.phone"
          role="button"
          :tabindex="smsAccountSelectable(account) ? 0 : -1"
          class="hme-sms-picker-item"
          :class="{ selected: smsBindingPhone === account.phone, full: !smsAccountSelectable(account), invalid: account.status === 'invalid' }"
          @click="smsAccountSelectable(account) && (smsBindingPhone = account.phone)"
          @keydown.enter="smsAccountSelectable(account) && (smsBindingPhone = account.phone)"
        >
          <header>
            <strong>{{ account.phone }}</strong>
            <div>
              <el-tag v-if="account.status === 'invalid'" size="small" type="danger">
                已失效{{ account.invalidAt ? ' · ' + formatDateTime(account.invalidAt) : '' }}
              </el-tag>
              <el-tag
                size="small"
                :type="account.linkedMailboxEmails.length >= 3 ? 'danger' : account.linkedMailboxEmails.length ? 'warning' : 'success'"
              >
                已绑 {{ account.linkedMailboxEmails.length }}/3
              </el-tag>
              <el-button
                link
                :type="account.status === 'active' ? 'danger' : 'success'"
                :loading="smsStatusUpdatingPhone === account.phone"
                @click.stop="toggleSMSStatus(account)"
              >
                {{ account.status === 'active' ? '设为失效' : '恢复' }}
              </el-button>
            </div>
          </header>
          <div v-if="account.linkedMailboxes.length" class="hme-sms-picker-bindings">
            <div v-for="binding in account.linkedMailboxes" :key="binding.email">
              <span>{{ binding.email }}</span>
              <time>{{ formatDateTime(binding.boundAt) }}</time>
            </div>
          </div>
          <span v-else class="hme-sms-picker-empty">暂无绑定邮箱，优先推荐</span>
          <small v-if="account.remark">{{ account.remark }}</small>
        </div>
        <el-empty v-if="!sortedSMSAccounts.length" description="暂无可用接码账号" />
      </div>
      <template #footer>
        <el-button
          v-if="smsBindingCurrentAccount"
          type="danger"
          plain
          :loading="smsBindingSaving"
          @click="saveSMSBinding('')"
        >
          解除绑定
        </el-button>
        <el-button @click="smsBindingVisible = false">取消</el-button>
        <el-button type="primary" :loading="smsBindingSaving" :disabled="!smsBindingPhone" @click="saveSMSBinding()">
          保存绑定
        </el-button>
      </template>
    </el-dialog>
  </section>
</template>
