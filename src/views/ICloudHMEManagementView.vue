<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import {
  Check,
  CopyDocument,
  Delete,
  EditPen,
  Files,
  FolderOpened,
  Key,
  Link,
  Plus,
  Refresh,
  Setting,
  View,
} from '@element-plus/icons-vue'
import MailboxTopbar from '@/components/MailboxTopbar.vue'
import {
  getLatestICloudHMEMail,
  loginICloudHMESource,
  saveICloudHMEAppPassword,
  saveICloudHMECookies,
  validateICloudHMESource,
  type ICloudHMEAlias,
  type ICloudHMEGroup,
  type ICloudHMEMail,
  type ICloudHMESourceAccount,
} from '@/services/iCloudHmeApi'
import { ICLOUD_HME_DEFAULT_GROUP, useICloudHmeStore } from '@/stores/iCloudHme'
import { formatDateTime } from '@/utils/dateTime'
import { plainMailBlocks } from '@/utils/mailBody'

const store = useICloudHmeStore()
const keyword = ref('')
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
const credentialDialogVisible = ref(false)
const credentialMode = ref<'cookies' | 'login' | 'appPassword'>('cookies')
const credentialSource = ref<ICloudHMESourceAccount>()
const credentialForm = ref({ cookies: '', password: '', otp: '', appPassword: '' })
const credentialSaving = ref(false)

const createDialogVisible = ref(false)
const createForm = ref({ sourceId: 0, label: '', group: ICLOUD_HME_DEFAULT_GROUP })
const creatingAlias = ref(false)
const syncDialogVisible = ref(false)
const syncSourceId = ref(0)
const syncing = ref(false)
const moveDialogVisible = ref(false)
const targetGroup = ref(ICLOUD_HME_DEFAULT_GROUP)

const mailVisible = ref(false)
const mailLoading = ref(false)
const mailAlias = ref<ICloudHMEAlias>()
const latestMail = ref<ICloudHMEMail>()
const latestMailHtml = computed(() => {
  const content = latestMail.value?.content?.trim() ?? ''
  if (!content || latestMail.value?.contentType !== 'text/html') return ''
  const styles = '<meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><base target="_blank"><style>html{background:#f4f6f9;color-scheme:light}*{box-sizing:border-box}body{max-width:760px;margin:0 auto!important;padding:28px!important;background:#fff;color:#1f2937;overflow-wrap:anywhere}img{max-width:100%!important;height:auto!important}table{max-width:100%!important}a{color:#2563eb}@media(max-width:680px){body{padding:18px!important}}</style>'
  if (/<\/head>/i.test(content)) return content.replace(/<\/head>/i, styles + '</head>')
  if (/<html(?:\s[^>]*)?>/i.test(content)) return content.replace(/<html([^>]*)>/i, '<html$1><head>' + styles + '</head>')
  return '<!doctype html><html><head>' + styles + '</head><body>' + content + '</body></html>'
})
const latestMailBlocks = computed(() => plainMailBlocks(latestMail.value?.contentType === 'text/plain' ? latestMail.value.content : ''))

const filteredAliases = computed(() => {
  const query = keyword.value.trim().toLowerCase()
  return store.aliases.filter((alias) => {
    if (store.selectedGroup && alias.group !== store.selectedGroup) return false
    if (!query) return true
    return [alias.email, alias.sourceAccountName, alias.label, alias.group, alias.remark]
      .some((value) => value.toLowerCase().includes(query))
  })
})
const pagedAliases = computed(() => {
  const start = (page.value - 1) * pageSize.value
  return filteredAliases.value.slice(start, start + pageSize.value)
})
const groupCounts = computed(() => {
  const counts = new Map<string, number>()
  store.aliases.forEach((alias) => counts.set(alias.group, (counts.get(alias.group) ?? 0) + 1))
  return counts
})
const activeSources = computed(() => store.sources.filter((source) => source.cookieConfigured && source.status === 'active'))

watch([keyword, () => store.selectedGroup], () => {
  page.value = 1
  selectedRows.value = []
})

onMounted(async () => {
  try { await store.load() } catch (error) { showError(error, '加载隐藏邮箱失败') }
})

function showError(error: unknown, fallback: string) {
  ElMessage.error(error instanceof Error ? error.message : fallback)
}
function handleSelection(rows: ICloudHMEAlias[]) { selectedRows.value = rows }
function groupCount(group: ICloudHMEGroup) { return groupCounts.value.get(group.name) ?? 0 }
function canDeleteGroup(group: ICloudHMEGroup) { return group.name !== ICLOUD_HME_DEFAULT_GROUP && groupCount(group) === 0 }
function sourceStatusType(status: string) {
  if (status === 'active') return 'success'
  if (status === 'reauth_required' || status === 'icloud_plus_required') return 'warning'
  if (status === 'error') return 'danger'
  return 'info'
}
function sourceStatusText(status: string) {
  return ({ active: '正常', pending: '待配置', reauth_required: '需重新登录', icloud_plus_required: '需要 iCloud+', error: '异常' } as Record<string, string>)[status] ?? '未知'
}
function formatAddress(addresses: ICloudHMEMail['from']) {
  return addresses.map((item) => item.name ? `${item.name} <${item.email ?? ''}>` : item.email).filter(Boolean).join(', ') || '未知发件人'
}

async function copyValue(value: string, label = '邮箱') {
  try {
    await navigator.clipboard.writeText(value)
    copiedValues.value = new Set([...copiedValues.value, value])
    window.setTimeout(() => { const next = new Set(copiedValues.value); next.delete(value); copiedValues.value = next }, 1200)
    ElMessage.success(`已复制${label}`)
  } catch { ElMessage.error(`复制${label}失败`) }
}
async function copySelected() {
  const targets = selectedRows.value.length ? selectedRows.value : filteredAliases.value
  if (!targets.length) { ElMessage.warning('没有可复制的隐藏邮箱'); return }
  await copyValue(targets.map((item) => item.email).join('\n'), `${targets.length} 个邮箱`)
}

async function createSource() {
  if (!sourceForm.value.name.trim() || !sourceForm.value.appleIdEmail.trim() || !sourceForm.value.icloudEmail.trim()) {
    ElMessage.warning('请填写主账号名称、Apple ID 和实际 iCloud 邮箱')
    return
  }
  sourceCreating.value = true
  try {
    await store.createSource({ ...sourceForm.value })
    sourceForm.value = { name: '', appleIdEmail: '', icloudEmail: '', host: 'icloud.com' }
    ElMessage.success('主账号已添加')
  } catch (error) { showError(error, '添加主账号失败') } finally { sourceCreating.value = false }
}
function openCredential(source: ICloudHMESourceAccount, mode: 'cookies' | 'login' | 'appPassword') {
  credentialSource.value = source
  credentialMode.value = mode
  credentialForm.value = { cookies: '', password: '', otp: '', appPassword: '' }
  credentialDialogVisible.value = true
}
function clearCredentialForm() {
  credentialForm.value = { cookies: '', password: '', otp: '', appPassword: '' }
  credentialSource.value = undefined
}
async function saveCredential() {
  const source = credentialSource.value
  if (!source) return
  credentialSaving.value = true
  try {
    if (credentialMode.value === 'cookies') await saveICloudHMECookies(source.id, credentialForm.value.cookies)
    if (credentialMode.value === 'login') await loginICloudHMESource(source.id, credentialForm.value.password, credentialForm.value.otp)
    if (credentialMode.value === 'appPassword') await saveICloudHMEAppPassword(source.id, credentialForm.value.appPassword)
    await store.load()
    credentialDialogVisible.value = false
    ElMessage.success(credentialMode.value === 'appPassword' ? 'App 专用密码已保存' : 'Apple 会话已更新')
  } catch (error) { showError(error, '保存配置失败') } finally { credentialSaving.value = false }
}
async function validateSource(source: ICloudHMESourceAccount) {
  busy.value = true
  try { await validateICloudHMESource(source.id); await store.load(); ElMessage.success('Apple 会话有效') }
  catch (error) { await store.load(); showError(error, '会话验证失败') }
  finally { busy.value = false }
}
async function deleteSource(source: ICloudHMESourceAccount) {
  await ElMessageBox.confirm(`确定删除主账号“${source.name}”？只有没有隐藏邮箱时才能删除。`, '删除主账号', { type: 'warning' })
  try { await store.deleteSource(source.id); ElMessage.success('主账号已删除') } catch (error) { showError(error, '删除主账号失败') }
}

function openCreateAlias() {
  const first = activeSources.value[0]
  if (!first) { sourceDialogVisible.value = true; ElMessage.warning('请先添加并配置 Apple 主账号'); return }
  createForm.value = { sourceId: first.id, label: '', group: store.selectedGroup || ICLOUD_HME_DEFAULT_GROUP }
  createDialogVisible.value = true
}
async function submitCreateAlias() {
  if (!createForm.value.sourceId) return
  creatingAlias.value = true
  try {
    const email = await store.createAlias(createForm.value.sourceId, createForm.value.label, createForm.value.group)
    createDialogVisible.value = false
    await copyValue(email, '新隐藏邮箱')
    ElMessage.success('隐藏邮箱已创建并复制')
  } catch (error) { showError(error, '创建隐藏邮箱失败') } finally { creatingAlias.value = false }
}
function openSync() {
  syncSourceId.value = activeSources.value[0]?.id ?? 0
  if (!syncSourceId.value) { sourceDialogVisible.value = true; ElMessage.warning('请先配置并验证 Apple 主账号'); return }
  syncDialogVisible.value = true
}
async function submitSync() {
  if (!syncSourceId.value) return
  syncing.value = true
  try {
    const result = await store.syncAliases(syncSourceId.value)
    syncDialogVisible.value = false
    ElMessage.success(`同步完成：新增 ${result.imported}，更新 ${result.updated}`)
  } catch (error) { showError(error, '同步隐藏邮箱失败') } finally { syncing.value = false }
}

async function editRemark(alias: ICloudHMEAlias) {
  const result = await ElMessageBox.prompt('备注最多 500 个字符，留空表示清除。', `编辑备注 · ${alias.email}`, {
    inputValue: alias.remark, inputType: 'textarea', inputValidator: (value) => Array.from(value.trim()).length <= 500 || '备注最多 500 个字符',
  })
  try { await store.updateRemark(alias.email, result.value); ElMessage.success('备注已保存') } catch (error) { showError(error, '保存备注失败') }
}
function openMove() {
  if (!selectedRows.value.length) { ElMessage.warning('请先勾选隐藏邮箱'); return }
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
async function deleteAlias(alias: ICloudHMEAlias) {
  await ElMessageBox.confirm(`只删除本地记录，不会停用 Apple 侧别名。确定删除 ${alias.email}？`, '删除本地记录', { type: 'warning' })
  try { await store.deleteAlias(alias.email); ElMessage.success('本地记录已删除') } catch (error) { showError(error, '删除失败') }
}
async function deleteSelected() {
  if (!selectedRows.value.length) { ElMessage.warning('请先勾选隐藏邮箱'); return }
  await ElMessageBox.confirm(`只删除 ${selectedRows.value.length} 条本地记录，不影响 Apple 侧别名。`, '批量删除', { type: 'warning' })
  busy.value = true
  try {
    await Promise.all(selectedRows.value.map((item) => store.deleteAlias(item.email)))
    selectedRows.value = []
    ElMessage.success('本地记录已删除')
  } catch (error) { await store.load(); showError(error, '批量删除失败') } finally { busy.value = false }
}

function configureMail(alias: ICloudHMEAlias) {
  const source = store.sources.find((item) => item.id === alias.sourceAccountId)
  if (!source) {
    sourceDialogVisible.value = true
    return
  }
  openCredential(source, 'appPassword')
}

async function openMail(alias: ICloudHMEAlias) {
  mailAlias.value = alias
  latestMail.value = undefined
  mailVisible.value = true
  await refreshMail()
}
async function refreshMail() {
  if (!mailAlias.value || mailLoading.value) return
  mailLoading.value = true
  try { latestMail.value = await getLatestICloudHMEMail(mailAlias.value.email) }
  catch (error) { latestMail.value = undefined; showError(error, '获取最新邮件失败') }
  finally { mailLoading.value = false }
}

async function renameGroup(group: ICloudHMEGroup) {
  const result = await ElMessageBox.prompt('请输入新的分组名称', '重命名隐藏邮箱分组', { inputValue: group.name, inputValidator: (value) => Boolean(value.trim()) || '分组名称不能为空' })
  if (result.value.trim() === group.name) return
  renamingGroupId.value = group.id
  try { await store.renameGroup(group.id, group.name, result.value.trim()); ElMessage.success('分组已重命名') }
  catch (error) { showError(error, '重命名分组失败') } finally { renamingGroupId.value = undefined }
}
async function deleteGroup(group: ICloudHMEGroup) {
  await ElMessageBox.confirm(`确定删除空分组“${group.name}”？`, '删除隐藏邮箱分组', { type: 'warning' })
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
            <button v-for="group in store.groups" :key="group.id" class="faka-nav-item sidebar-list-row group-nav-item" :class="{ active: store.selectedGroup === group.name, dragging: draggingGroupId === group.id }" draggable="true" @click="store.selectedGroup = group.name" @dragstart="dragStart(group, $event)" @dragover.prevent @drop="dropGroup(group, $event)" @dragend="draggingGroupId = undefined">
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
        <div class="faka-action-row">
          <el-button :icon="Setting" @click="sourceDialogVisible = true">主账号管理</el-button>
          <el-button type="primary" :icon="Plus" @click="openCreateAlias">创建隐藏邮箱</el-button>
          <el-button :icon="Refresh" @click="openSync">同步别名</el-button>
          <el-button :icon="CopyDocument" @click="copySelected">复制邮箱</el-button>
          <el-button :icon="FolderOpened" :disabled="!selectedRows.length" @click="openMove">移动分组</el-button>
          <el-button type="danger" :icon="Delete" :disabled="!selectedRows.length" @click="deleteSelected">删除本地记录</el-button>
        </div>
        <div class="account-selection-hint" :class="{ active: selectedRows.length }"><strong>已选 {{ selectedRows.length }} 个隐藏邮箱</strong><span>复制优先作用于已选项；删除只清理本地记录</span></div>
        <el-table v-loading="store.loading || busy" :data="pagedAliases" row-key="email" class="faka-account-table" height="calc(100vh - 232px)" @selection-change="handleSelection">
          <el-table-column type="selection" width="52" align="center" />
          <el-table-column label="#" width="64" align="center"><template #default="{ $index }"><span class="row-number">{{ (page - 1) * pageSize + $index + 1 }}</span></template></el-table-column>
          <el-table-column label="隐藏邮箱" min-width="270" show-overflow-tooltip><template #default="{ row }"><div class="copy-cell" :class="{ copied: copiedValues.has(row.email) }"><span>{{ row.email }}</span><el-button link :icon="CopyDocument" @click.stop="copyValue(row.email)" /></div></template></el-table-column>
          <el-table-column prop="sourceAccountName" label="Apple 主账号" min-width="150" show-overflow-tooltip />
          <el-table-column prop="label" label="Apple 标签" min-width="170" show-overflow-tooltip><template #default="{ row }"><span :class="{ muted: !row.label }">{{ row.label || '未设置' }}</span></template></el-table-column>
          <el-table-column prop="group" label="分组" width="140" />
          <el-table-column label="备注" min-width="190" show-overflow-tooltip><template #default="{ row }"><div class="remark-cell"><span :class="{ muted: !row.remark }">{{ row.remark || '无备注' }}</span><el-button link :icon="EditPen" @click.stop="editRemark(row)" /></div></template></el-table-column>
          <el-table-column label="状态" width="120" align="center"><template #default="{ row }"><el-tag :type="row.active ? 'success' : 'info'" effect="light">{{ row.active ? '已启用' : '已停用' }}</el-tag></template></el-table-column>
          <el-table-column label="创建时间" width="170"><template #default="{ row }">{{ formatDateTime(row.createdAt) }}</template></el-table-column>
          <el-table-column label="操作" width="180" fixed="right" align="center"><template #default="{ row }"><el-button size="small" type="primary" :icon="row.mailReady ? View : Key" @click.stop="row.mailReady ? openMail(row) : configureMail(row)">{{ row.mailReady ? '查看邮件' : '配置收件' }}</el-button><el-button size="small" type="danger" plain :icon="Delete" @click.stop="deleteAlias(row)" /></template></el-table-column>
        </el-table>
        <div class="faka-pagination"><span>Total {{ filteredAliases.length }}</span><el-pagination v-model:current-page="page" v-model:page-size="pageSize" size="small" layout="sizes, prev, pager, next" :total="filteredAliases.length" :page-sizes="[20, 50, 100]" /></div>
      </section>
    </main>

    <el-dialog v-model="sourceDialogVisible" title="Apple 主账号管理" width="900px" class="hme-source-dialog">
      <div class="hme-source-create">
        <el-input v-model="sourceForm.name" placeholder="账号名称" />
        <el-input v-model="sourceForm.appleIdEmail" placeholder="Apple ID" />
        <el-input v-model="sourceForm.icloudEmail" placeholder="实际 iCloud 邮箱" />
        <el-select v-model="sourceForm.host"><el-option label="国际区" value="icloud.com" /><el-option label="中国区" value="icloud.com.cn" /></el-select>
        <el-button type="primary" :loading="sourceCreating" @click="createSource">添加</el-button>
      </div>
      <el-table :data="store.sources" max-height="420">
        <el-table-column prop="name" label="名称" min-width="120" />
        <el-table-column prop="appleIdEmail" label="Apple ID" min-width="190" show-overflow-tooltip />
        <el-table-column prop="icloudEmail" label="收件邮箱" min-width="190" show-overflow-tooltip />
        <el-table-column label="状态" width="130"><template #default="{ row }"><el-tag :type="sourceStatusType(row.status)">{{ sourceStatusText(row.status) }}</el-tag></template></el-table-column>
        <el-table-column label="别名" width="70" align="center"><template #default="{ row }">{{ row.aliasTotal }}</template></el-table-column>
        <el-table-column label="操作" width="340" fixed="right"><template #default="{ row }">
          <el-button size="small" :icon="Link" @click="openCredential(row, 'cookies')">Cookie</el-button>
          <el-button size="small" @click="openCredential(row, 'login')">登录</el-button>
          <el-button size="small" :icon="Key" @click="openCredential(row, 'appPassword')">收件密码</el-button>
          <el-button size="small" :icon="Check" :disabled="!row.cookieConfigured" @click="validateSource(row)">验证</el-button>
          <el-button size="small" type="danger" link :icon="Delete" @click="deleteSource(row)" />
        </template></el-table-column>
      </el-table>
    </el-dialog>

    <el-dialog v-model="credentialDialogVisible" @closed="clearCredentialForm" :title="credentialSource ? `配置 · ${credentialSource.name}` : '配置主账号'" width="520px">
      <el-form label-position="top">
        <template v-if="credentialMode === 'cookies'"><el-form-item label="Cookie JSON 或 Cookie Header"><el-input v-model="credentialForm.cookies" type="textarea" :rows="8" placeholder="粘贴 Cookie JSON，或 name=value; name2=value2" /></el-form-item></template>
        <template v-else-if="credentialMode === 'login'"><el-alert title="Apple 常规密码和验证码只用于本次登录，不会保存。" type="info" :closable="false" /><el-form-item label="Apple 密码"><el-input v-model="credentialForm.password" type="password" show-password /></el-form-item><el-form-item label="双重认证验证码（需要时填写）"><el-input v-model="credentialForm.otp" maxlength="8" /></el-form-item></template>
        <template v-else><el-alert title="请使用 Apple 账户生成的 App 专用密码，供 IMAP 收件使用。" type="info" :closable="false" /><el-form-item label="App 专用密码"><el-input v-model="credentialForm.appPassword" type="password" show-password /></el-form-item></template>
      </el-form>
      <template #footer><el-button @click="credentialDialogVisible = false">取消</el-button><el-button type="primary" :loading="credentialSaving" @click="saveCredential">{{ credentialMode === 'appPassword' ? '保存' : '保存并验证' }}</el-button></template>
    </el-dialog>

    <el-dialog v-model="createDialogVisible" title="创建 iCloud 隐藏邮箱" width="480px">
      <el-form label-position="top"><el-form-item label="Apple 主账号"><el-select v-model="createForm.sourceId" style="width:100%"><el-option v-for="source in store.sources" :key="source.id" :label="`${source.name} · ${source.appleIdEmail}`" :value="source.id" :disabled="!source.cookieConfigured || source.status !== 'active'" /></el-select></el-form-item><el-form-item label="Apple 标签"><el-input v-model="createForm.label" placeholder="用于在 Apple 端识别用途" /></el-form-item><el-form-item label="本地分组"><el-select v-model="createForm.group" filterable allow-create style="width:100%"><el-option v-for="group in store.groups" :key="group.id" :label="group.name" :value="group.name" /></el-select></el-form-item></el-form>
      <template #footer><el-button @click="createDialogVisible = false">取消</el-button><el-button type="primary" :loading="creatingAlias" @click="submitCreateAlias">创建并复制</el-button></template>
    </el-dialog>

    <el-dialog v-model="syncDialogVisible" title="同步 Apple 隐藏邮箱" width="460px"><el-form label-position="top"><el-form-item label="选择主账号"><el-select v-model="syncSourceId" style="width:100%"><el-option v-for="source in store.sources" :key="source.id" :label="`${source.name} · ${source.aliasTotal} 个别名`" :value="source.id" :disabled="!source.cookieConfigured || source.status !== 'active'" /></el-select></el-form-item></el-form><template #footer><el-button @click="syncDialogVisible = false">取消</el-button><el-button type="primary" :loading="syncing" @click="submitSync">开始同步</el-button></template></el-dialog>
    <el-dialog v-model="moveDialogVisible" title="移动隐藏邮箱分组" width="420px"><el-select v-model="targetGroup" filterable allow-create style="width:100%"><el-option v-for="group in store.groups" :key="group.id" :label="group.name" :value="group.name" /></el-select><template #footer><el-button @click="moveDialogVisible = false">取消</el-button><el-button type="primary" :loading="busy" @click="submitMove">确定</el-button></template></el-dialog>

    <el-dialog v-model="mailVisible" width="840px" class="icloud-latest-dialog"><template #header><div class="icloud-dialog-heading"><span>iCloud 隐藏邮箱</span><h2>{{ latestMail?.subject || '最新邮件' }}</h2></div></template><div v-loading="mailLoading" class="icloud-latest-content"><template v-if="latestMail"><div class="icloud-message-meta"><div class="reader-sender-avatar">{{ formatAddress(latestMail.from).charAt(0).toUpperCase() }}</div><div class="icloud-message-parties"><strong>{{ formatAddress(latestMail.from) }}</strong><span>发送至 {{ mailAlias?.email }}</span></div><time>{{ formatDateTime(latestMail.receivedAt) }}</time></div><section class="icloud-reader-panel"><iframe v-if="latestMailHtml" class="icloud-mail-frame" :srcdoc="latestMailHtml" sandbox="allow-popups allow-popups-to-escape-sandbox" title="隐藏邮箱邮件正文" /><div v-else class="mail-body plain plain-mail-paragraphs icloud-mail-body"><p v-for="(block, index) in latestMailBlocks" :key="index" :class="{ 'plain-mail-heading': block.kind === 'heading' }">{{ block.text }}</p><p v-if="!latestMailBlocks.length" class="plain-mail-empty">暂无正文内容</p></div></section></template><el-empty v-else-if="!mailLoading" description="暂无邮件" /></div><template #footer><el-button @click="mailVisible = false">关闭</el-button><el-button type="primary" :icon="Refresh" :loading="mailLoading" @click="refreshMail">刷新最新邮件</el-button></template></el-dialog>
  </section>
</template>
