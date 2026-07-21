<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Check, CopyDocument, Delete, Document, EditPen, Files, FolderOpened, Key, Link, More, Plus, Refresh, Setting, View } from '@element-plus/icons-vue'
import MailboxTopbar from '@/components/MailboxTopbar.vue'
import {
  completeICloudHMELogin, getICloudHMEJob, getICloudHMEMessage, listICloudHMEMessages,
  permanentlyDeleteICloudHMEAlias, refreshICloudHMECode, saveICloudHMEAppPassword,
  saveICloudHMECookies, startICloudHMELogin, syncAllICloudHMESources, syncICloudHMEAliases,
  updateICloudHMEAliasLifecycle, validateAllICloudHMESources, validateICloudHMESource,
  type ICloudHMEAlias, type ICloudHMEGroup, type ICloudHMEJob, type ICloudHMEMail,
  type ICloudHMEMailSummary, type ICloudHMESourceAccount,
} from '@/services/iCloudHmeApi'
import {
  cacheICloudHMEBody, cacheICloudHMEMessages, deleteICloudHMECacheForAlias,
  getCachedICloudHMEBody, listCachedICloudHMEMessages,
} from '@/services/iCloudHmeStorage'
import { ICLOUD_HME_DEFAULT_GROUP, useICloudHmeStore } from '@/stores/iCloudHme'
import { formatDateTime } from '@/utils/dateTime'
import { plainMailBlocks } from '@/utils/mailBody'

const store = useICloudHmeStore()
const keyword = ref('')
const sourceFilter = ref<number | 'all'>('all')
const statusFilter = ref<'all' | ICloudHMEAlias['appleStatus']>('all')
const page = ref(1)
const pageSize = ref(20)
const selectedRows = ref<ICloudHMEAlias[]>([])
const copiedValues = ref(new Set<string>())
const draggingGroupId = ref<number>()
const renamingGroupId = ref<number>()
const deletingGroupId = ref<number>()
const busy = ref(false)

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

const createDialogVisible = ref(false)
const createForm = ref({ mode: 'fixed' as 'fixed' | 'pool', sourceId: 0, labelPrefix: 'MailBox', count: 1, group: ICLOUD_HME_DEFAULT_GROUP })
const creatingJob = ref(false)
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

const filteredAliases = computed(() => {
  const query = keyword.value.trim().toLowerCase()
  return store.aliases.filter((alias) => {
    if (store.selectedGroup && alias.group !== store.selectedGroup) return false
    if (sourceFilter.value !== 'all' && alias.sourceAccountId !== sourceFilter.value) return false
    if (statusFilter.value !== 'all' && alias.appleStatus !== statusFilter.value) return false
    return !query || [alias.email, alias.sourceAccountName, alias.label, alias.group, alias.remark]
      .some((value) => value.toLowerCase().includes(query))
  })
})
const pagedAliases = computed(() => filteredAliases.value.slice((page.value - 1) * pageSize.value, page.value * pageSize.value))
const groupCounts = computed(() => {
  const counts = new Map<string, number>()
  store.aliases.forEach((alias) => counts.set(alias.group, (counts.get(alias.group) ?? 0) + 1))
  return counts
})
const healthySources = computed(() => store.sources.filter((source) => source.cookieConfigured && source.status === 'active'))
const activeJobs = computed(() => store.jobs.filter((job) => ['pending', 'running', 'cancel_requested'].includes(job.status)))
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

let jobPollTimer: number | undefined
let jobPolling = false
watch([keyword, sourceFilter, statusFilter, () => store.selectedGroup], () => { page.value = 1; selectedRows.value = [] })
watch(() => filteredAliases.value.length, (total) => { page.value = Math.min(page.value, Math.max(1, Math.ceil(total / pageSize.value))) })
onMounted(async () => {
  try { await store.load() } catch (error) { showError(error, '加载隐藏邮箱失败') }
  jobPollTimer = window.setInterval(pollJobs, 2500)
})
onBeforeUnmount(() => { if (jobPollTimer) window.clearInterval(jobPollTimer) })

async function pollJobs() {
  if (jobPolling || (!activeJobs.value.length && !jobsDialogVisible.value)) return
  jobPolling = true
  try {
    await store.loadJobs()
    if (selectedJob.value) selectedJob.value = await getICloudHMEJob(selectedJob.value.id)
  } catch { /* Background polling stays silent. */ }
  finally { jobPolling = false }
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
function sourceStatusType(status: string) { return status === 'active' ? 'success' : status === 'pending' ? 'warning' : 'danger' }
function sourceStatusText(status: string) { return status === 'active' ? '会话正常' : status === 'pending' ? '待配置' : '需处理' }
function aliasStatusType(status: ICloudHMEAlias['appleStatus']) { return status === 'active' ? 'success' : status === 'inactive' ? 'warning' : status === 'deleted' ? 'danger' : 'info' }
function aliasStatusText(status: ICloudHMEAlias['appleStatus']) { return { active: '已启用', inactive: '已停用', deleted: '已永久删除', unknown: '状态未知' }[status] }
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
function openCreateJob() {
  if (!store.sources.length) {
    sourceDialogVisible.value = true
    return ElMessage.warning('请先添加并配置 Apple 主账号')
  }
  createForm.value = {
    mode: healthySources.value.length > 1 ? 'pool' : 'fixed',
    sourceId: healthySources.value[0]?.id ?? 0,
    labelPrefix: 'MailBox',
    count: 1,
    group: store.selectedGroup || ICLOUD_HME_DEFAULT_GROUP,
  }
  createDialogVisible.value = true
}
async function submitCreateJob() {
  if (createForm.value.mode === 'fixed' && !createForm.value.sourceId) return ElMessage.warning('请选择主账号')
  creatingJob.value = true
  try {
    const job = await store.createJob({
      mode: createForm.value.mode,
      sourceAccountId: createForm.value.mode === 'fixed' ? createForm.value.sourceId : undefined,
      labelPrefix: createForm.value.labelPrefix,
      count: createForm.value.count,
      group: createForm.value.group,
    })
    createDialogVisible.value = false
    jobsDialogVisible.value = true
    selectedJob.value = job
    ElMessage.success('创建任务已提交，页面关闭后仍会继续执行')
  } catch (error) { showError(error, '创建任务失败') } finally { creatingJob.value = false }
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
    '该操作不可恢复。请输入完整隐藏邮箱确认：' + alias.email,
    'Apple 永久删除',
    { type: 'warning', inputValidator: (value) => value.trim().toLowerCase() === alias.email.toLowerCase() || '输入的邮箱不匹配' },
  )
  busy.value = true
  try {
    await permanentlyDeleteICloudHMEAlias(alias.email, result.value)
    await store.load()
    ElMessage.success('Apple 侧隐藏邮箱已永久删除，审计记录已保留')
  } catch (error) { showError(error, '永久删除失败') } finally { busy.value = false }
}
async function deleteAlias(alias: ICloudHMEAlias) {
  await ElMessageBox.confirm('只删除本地记录，不会操作 Apple 侧别名。确定删除 ' + alias.email + '？', '删除本地记录', { type: 'warning' })
  try {
    await store.deleteAlias(alias.email)
    await deleteICloudHMECacheForAlias(alias.email)
    ElMessage.success('本地记录与邮件缓存已删除')
  } catch (error) { showError(error, '删除失败') }
}
async function deleteSelected() {
  if (!selectedRows.value.length) return ElMessage.warning('请先勾选隐藏邮箱')
  await ElMessageBox.confirm('只删除 ' + selectedRows.value.length + ' 条本地记录，不影响 Apple 侧别名。', '批量删除', { type: 'warning' })
  busy.value = true
  try {
    for (const alias of selectedRows.value) {
      await store.deleteAlias(alias.email)
      await deleteICloudHMECacheForAlias(alias.email)
    }
    selectedRows.value = []
    ElMessage.success('本地记录与缓存已删除')
  } catch (error) { await store.load(); showError(error, '批量删除失败') } finally { busy.value = false }
}
async function handleRowCommand(command: string, alias: ICloudHMEAlias) {
  if (command === 'deactivate') await lifecycleSelected('deactivate', [alias])
  if (command === 'reactivate') await lifecycleSelected('reactivate', [alias])
  if (command === 'delete-apple') await permanentlyDeleteAlias(alias)
  if (command === 'delete-local') await deleteAlias(alias)
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
    if (source) openCredential(source, 'appPassword')
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
        <div class="faka-action-row hme-action-row">
          <el-button :icon="Setting" @click="sourceDialogVisible = true">主账号管理</el-button>
          <el-button type="primary" :icon="Plus" @click="openCreateJob">创建隐藏邮箱</el-button>
          <el-button :icon="Document" @click="jobsDialogVisible = true">创建任务<span v-if="activeJobs.length">（{{ activeJobs.length }}）</span></el-button>
          <el-button :icon="CopyDocument" @click="copySelected">复制邮箱</el-button>
          <el-button :icon="FolderOpened" :disabled="!selectedRows.length" @click="openMove">移动分组</el-button>
          <el-button :disabled="!selectedRows.length" @click="lifecycleSelected('deactivate')">停用</el-button>
          <el-button :disabled="!selectedRows.length" @click="lifecycleSelected('reactivate')">恢复</el-button>
          <el-dropdown @command="exportAliases">
            <el-button>导出<el-icon class="el-icon--right"><More /></el-icon></el-button>
            <template #dropdown><el-dropdown-menu><el-dropdown-item command="txt">导出 TXT</el-dropdown-item><el-dropdown-item command="csv">导出 CSV</el-dropdown-item></el-dropdown-menu></template>
          </el-dropdown>
          <el-button type="danger" :icon="Delete" :disabled="!selectedRows.length" @click="deleteSelected">删除本地记录</el-button>
        </div>
        <div class="hme-filter-row">
          <el-select v-model="sourceFilter" style="width:190px"><el-option label="全部主账号" value="all" /><el-option v-for="source in store.sources" :key="source.id" :label="source.name" :value="source.id" /></el-select>
          <el-select v-model="statusFilter" style="width:160px"><el-option label="全部状态" value="all" /><el-option label="已启用" value="active" /><el-option label="已停用" value="inactive" /><el-option label="已永久删除" value="deleted" /><el-option label="状态未知" value="unknown" /></el-select>
          <span>{{ filteredAliases.length }} 个结果</span>
        </div>
        <div class="account-selection-hint" :class="{ active: selectedRows.length }"><strong>已选 {{ selectedRows.length }} 个隐藏邮箱</strong><span>停用、恢复、复制、移动和本地删除作用于已选项；永久删除仅支持单个</span></div>
        <el-table v-loading="store.loading || busy" :data="pagedAliases" row-key="email" class="faka-account-table" height="calc(100vh - 282px)" @selection-change="handleSelection">
          <el-table-column type="selection" width="52" align="center" />
          <el-table-column label="#" width="64" align="center"><template #default="{ $index }"><span class="row-number">{{ (page - 1) * pageSize + $index + 1 }}</span></template></el-table-column>
          <el-table-column label="隐藏邮箱" min-width="260" show-overflow-tooltip><template #default="{ row }"><div class="copy-cell" :class="{ copied: copiedValues.has(row.email) }"><span>{{ row.email }}</span><el-button link :icon="CopyDocument" @click.stop="copyValue(row.email)" /></div></template></el-table-column>
          <el-table-column prop="sourceAccountName" label="Apple 主账号" min-width="145" show-overflow-tooltip />
          <el-table-column prop="label" label="Apple 标签" min-width="165" show-overflow-tooltip />
          <el-table-column prop="group" label="分组" width="125" />
          <el-table-column label="备注" min-width="160" show-overflow-tooltip><template #default="{ row }"><div class="remark-cell"><span :class="{ muted: !row.remark }">{{ row.remark || '无备注' }}</span><el-button link :icon="EditPen" @click.stop="editRemark(row)" /></div></template></el-table-column>
          <el-table-column label="Apple 状态" width="130" align="center"><template #default="{ row }"><el-tag :type="aliasStatusType(row.appleStatus)" effect="light">{{ aliasStatusText(row.appleStatus) }}</el-tag></template></el-table-column>
          <el-table-column label="最后同步" width="155"><template #default="{ row }">{{ row.lastSyncedAt ? formatDateTime(row.lastSyncedAt) : '未同步' }}</template></el-table-column>
          <el-table-column label="操作" width="175" fixed="right" align="center"><template #default="{ row }">
            <el-button size="small" type="primary" :icon="row.mailReady ? View : Key" @click.stop="openMail(row)">{{ row.mailReady ? '邮件' : '配置收件' }}</el-button>
            <el-dropdown trigger="click" @command="handleRowCommand($event, row)"><el-button size="small" :icon="More" />
              <template #dropdown><el-dropdown-menu>
                <el-dropdown-item v-if="row.appleStatus === 'active'" command="deactivate">Apple 停用</el-dropdown-item>
                <el-dropdown-item v-if="row.appleStatus === 'inactive'" command="reactivate">Apple 恢复</el-dropdown-item>
                <el-dropdown-item v-if="row.appleStatus !== 'deleted'" command="delete-apple" divided>Apple 永久删除</el-dropdown-item>
                <el-dropdown-item command="delete-local">删除本地记录</el-dropdown-item>
              </el-dropdown-menu></template>
            </el-dropdown>
          </template></el-table-column>
        </el-table>
        <div class="faka-pagination"><span>Total {{ filteredAliases.length }}</span><el-pagination v-model:current-page="page" v-model:page-size="pageSize" size="small" layout="sizes, prev, pager, next" :total="filteredAliases.length" :page-sizes="[20, 50, 100]" /></div>
      </section>
    </main>
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

    <el-dialog v-model="createDialogVisible" title="创建 iCloud 隐藏邮箱" width="560px">
      <el-form label-position="top">
        <el-form-item label="分配方式"><el-segmented v-model="createForm.mode" :options="[{ label: '固定主账号', value: 'fixed' }, { label: '健康账号池', value: 'pool' }]" /></el-form-item>
        <el-form-item v-if="createForm.mode === 'fixed'" label="Apple 主账号"><el-select v-model="createForm.sourceId" style="width:100%"><el-option v-for="source in store.sources" :key="source.id" :label="source.name + ' · ' + source.aliasTotal + ' 个别名'" :value="source.id" :disabled="!source.cookieConfigured || source.status !== 'active'" /></el-select></el-form-item>
        <el-alert v-else title="只使用会话正常的主账号，并按当前别名数量从少到多分配。" type="info" :closable="false" />
        <el-form-item label="Apple 标签前缀"><el-input v-model="createForm.labelPrefix" maxlength="80" show-word-limit /><small>实际标签为“前缀 #序号”，用于重启恢复与防重。</small></el-form-item>
        <el-form-item label="创建数量"><el-input-number v-model="createForm.count" :min="1" :max="20" /></el-form-item>
        <el-form-item label="本地分组"><el-select v-model="createForm.group" filterable allow-create style="width:100%"><el-option v-for="group in store.groups" :key="group.id" :label="group.name" :value="group.name" /></el-select></el-form-item>
      </el-form>
      <template #footer><el-button @click="createDialogVisible = false">取消</el-button><el-button type="primary" :loading="creatingJob" @click="submitCreateJob">提交创建任务</el-button></template>
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
  </section>
</template>