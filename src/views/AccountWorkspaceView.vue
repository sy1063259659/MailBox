<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import {
  CopyDocument,
  Delete,
  Download,
  EditPen,
  Files,
  FolderOpened,
  House,
  Message,
  Reading,
  Refresh,
  Search,
  Sort,
  UploadFilled,
} from '@element-plus/icons-vue'
import { useAccountStore } from '@/stores/account'
import { useAuthStore } from '@/stores/auth'
import { useMailStore } from '@/stores/mail'
import type { AccountStatus, MailAccount, MailAddress, MailFolder, MailMessage } from '@/types'
import type { MailGroup } from '@/services/accountApi'

const accountStore = useAccountStore()
const authStore = useAuthStore()
const mailStore = useMailStore()
const selectedAccountRows = ref<MailAccount[]>([])
const globalKeyword = ref('')
const groupDialogVisible = ref(false)
const targetGroupName = ref('')
const currentViewedAccount = ref<MailAccount>()
const workspaceMode = ref<'accounts' | 'mail'>('accounts')
const mailSortDesc = ref(true)
const lastBatchFailedResults = ref<{ accountEmail: string; folder: MailFolder }[]>([])
const viewingEmail = ref('')
const splittingEmail = ref('')
const deleting = ref(false)
const movingGroup = ref(false)
const refreshingFolder = ref(false)
const loggingOut = ref(false)
const copying = ref(false)
const copiedValues = ref<Set<string>>(new Set())
const editingRemarkEmail = ref('')
const draggingGroupId = ref<number>()
const deletingGroupId = ref<number>()

defineProps<{
  exporting?: boolean
  clearingData?: boolean
}>()

const emit = defineEmits<{
  importAccounts: []
  exportData: []
}>()

const statusType: Record<AccountStatus, 'info' | 'primary' | 'success' | 'warning' | 'danger'> = {
  idle: 'info',
  syncing: 'primary',
  success: 'success',
  error: 'danger',
  token_expired: 'warning',
  rate_limited: 'warning',
}

const statusText: Record<AccountStatus, string> = {
  idle: '未同步',
  syncing: '同步中',
  success: '正常',
  error: '失败',
  token_expired: '令牌失效',
  rate_limited: '限流',
}

const selectedHtml = computed(() => {
  const content = mailStore.selectedBody?.content ?? ''
  if (!looksLikeHtml(content)) {
    return ''
  }

  return `<!doctype html>
<html>
  <head>
    <meta charset="utf-8" />
    <base target="_blank" />
    <style>
      html, body { margin: 0; padding: 0; background: #fff; color: #18212f; font-family: Arial, sans-serif; line-height: 1.55; }
      body { padding: 18px 20px; overflow-wrap: anywhere; }
      img { max-width: 100%; height: auto; }
      table { max-width: 100%; }
      a { color: #1d4ed8; }
    </style>
  </head>
  <body>${content}</body>
</html>`
})

const accountSearchKeyword = computed({
  get: () => (workspaceMode.value === 'accounts' ? globalKeyword.value : ''),
  set: (value: string) => {
    globalKeyword.value = value
  },
})

const topSearchValue = computed(() =>
  workspaceMode.value === 'mail' ? mailStore.filter.query : globalKeyword.value,
)

const accountTree = computed<MailAccount[]>(() => {
  const group = accountStore.selectedGroup
  const roots = accountStore.accountTree
  if (!group) {
    return roots
  }
  return roots
    .map((account) => ({
      ...account,
      children: account.children?.filter((child) => child.group === group),
    }))
    .filter((account) => account.group === group || (account.children?.length ?? 0) > 0)
})

const visibleAccounts = computed<MailAccount[]>(() => {
  const keyword = accountSearchKeyword.value.trim().toLocaleLowerCase()
  if (!keyword) {
    return accountTree.value
  }
  return accountTree.value
    .map((account) => {
      const children = account.children?.filter((child) => accountMatchesKeyword(child, keyword)) ?? []
      if (accountMatchesKeyword(account, keyword) || children.length > 0) {
        return { ...account, children }
      }
      return undefined
    })
    .filter((account) => account !== undefined)
})

const visibleFlatAccounts = computed(() => flattenAccounts(visibleAccounts.value))

const sidebarRootAccounts = computed(() => accountTree.value)

const groupCountByName = computed(() => {
  const counts = new Map<string, number>()
  for (const account of accountStore.accounts) {
    counts.set(account.group, (counts.get(account.group) ?? 0) + 1)
  }
  return counts
})

const parentRowIndexMap = computed(() => {
  const map = new Map<string, number>()
  visibleAccounts.value.forEach((account, index) => {
    map.set(account.email, index + 1)
  })
  return map
})

function accountMatchesKeyword(account: MailAccount, keyword: string): boolean {
  return [account.email, account.password, account.group, account.remark, account.status, account.parentEmail ?? '']
      .filter(Boolean)
      .some((value) => value.toLocaleLowerCase().includes(keyword))
}

const messageCountByEmail = computed(() => {
  const counts = new Map<string, number>()
  for (const message of mailStore.messages) {
    counts.set(message.accountEmail, (counts.get(message.accountEmail) ?? 0) + 1)
  }
  return counts
})

const batchProgressPercent = computed(() => {
  if (mailStore.batchSyncTotal === 0) {
    return 0
  }
  return Math.round((mailStore.batchSyncDone / mailStore.batchSyncTotal) * 100)
})

const batchFailedResults = computed(() =>
  mailStore.batchSyncResults.filter((result) => result.status === 'failed'),
)

const visibleMessages = computed(() => {
  const messages = [...mailStore.messages]
  messages.sort((left, right) =>
    mailSortDesc.value
      ? right.receivedAt.localeCompare(left.receivedAt)
      : left.receivedAt.localeCompare(right.receivedAt),
  )
  return messages
})

const currentSyncKey = computed(() =>
  mailStore.filter.accountEmail ? `${mailStore.filter.accountEmail}::${mailStore.filter.folder}` : '',
)

const currentAccountSyncing = computed(() =>
  currentSyncKey.value ? Boolean(mailStore.syncingAccounts[currentSyncKey.value]) : false,
)

function looksLikeHtml(value: string): boolean {
  return /<(?:!doctype|html|head|body|div|table|style|meta|title|p|br|span)\b/i.test(value)
}

function formatDateTime(value?: string): string {
  if (!value) {
    return '未同步'
  }
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) {
    return value
  }
  return date.toLocaleString('zh-CN', { hour12: false })
}

function formatAddress(address: MailAddress): string {
  if (address.name && address.email) {
    return `${address.name} <${address.email}>`
  }
  return address.email || address.name || ''
}

function formatAddressList(addresses?: MailAddress[], emptyText = '未知收件人'): string {
  const text = (addresses ?? [])
    .map(formatAddress)
    .filter(Boolean)
    .join(', ')
  return text || emptyText
}

function sortByDateTime(left: MailAccount, right: MailAccount, key: 'createdAt' | 'updatedAt' | 'lastSyncAt'): number {
  return new Date(left[key] ?? 0).getTime() - new Date(right[key] ?? 0).getTime()
}

function flattenAccounts(accounts: MailAccount[]): MailAccount[] {
  return accounts.flatMap((account) => [account, ...(account.children ?? [])])
}

function splitCount(account: MailAccount): number {
  return account.children?.length ?? 0
}

function resolveParentAccount(account: MailAccount): MailAccount {
  if (!account.parentEmail) {
    return account
  }
  return accountTree.value.find((item) => item.email === account.parentEmail)
    ?? accountStore.accounts.find((item) => item.email === account.parentEmail)
    ?? account
}

function resolveMailboxAccount(account: MailAccount): MailAccount {
  return resolveParentAccount(account)
}

function accountRowLabel(account: MailAccount): string {
  if (isSplitAccount(account)) {
    return ''
  }
  return String(parentRowIndexMap.value.get(account.email) ?? 0)
}

function splitLabel(account: MailAccount): string {
  const parent = resolveParentAccount(account)
  const splitIndex = account.splitIndex
    ?? ((parent.children?.findIndex((child) => child.email === account.email) ?? -1) + 1)
  return String(splitIndex)
}

function canSplitHotmail(account: MailAccount): boolean {
  return !account.parentEmail && /^[^+@\s]+@hotmail\.com$/i.test(account.email) && splitCount(account) === 0
}

function isSplitAccount(account: MailAccount): boolean {
  return Boolean(account.parentEmail)
}

function setGroup(group: string) {
  accountStore.setSelectedGroup(group)
  mailStore.setFilter({ group, accountEmail: '' })
  workspaceMode.value = 'accounts'
}

function groupAccountCount(group: MailGroup): number {
  return groupCountByName.value.get(group.name) ?? 0
}

function canDeleteGroup(group: MailGroup): boolean {
  return group.name !== '默认分组' && groupAccountCount(group) === 0
}

function handleGroupDragStart(group: MailGroup, event: DragEvent) {
  draggingGroupId.value = group.id
  event.dataTransfer?.setData('text/plain', String(group.id))
  if (event.dataTransfer) {
    event.dataTransfer.effectAllowed = 'move'
  }
}

function handleGroupDragOver(event: DragEvent) {
  event.preventDefault()
  if (event.dataTransfer) {
    event.dataTransfer.dropEffect = 'move'
  }
}

async function handleGroupDrop(targetGroup: MailGroup, event: DragEvent) {
  event.preventDefault()
  const sourceId = draggingGroupId.value
    ?? Number(event.dataTransfer?.getData('text/plain'))
  draggingGroupId.value = undefined
  if (!sourceId || sourceId === targetGroup.id) {
    return
  }

  const groups = [...accountStore.remoteGroups]
  const sourceIndex = groups.findIndex((group) => group.id === sourceId)
  const targetIndex = groups.findIndex((group) => group.id === targetGroup.id)
  if (sourceIndex < 0 || targetIndex < 0) {
    return
  }
  const [sourceGroup] = groups.splice(sourceIndex, 1)
  groups.splice(targetIndex, 0, sourceGroup)
  accountStore.remoteGroups = groups
  try {
    await accountStore.reorderGroups(groups.map((group) => group.id))
  } catch (error) {
    await accountStore.loadAccounts()
    ElMessage.error(error instanceof Error ? error.message : '分组排序失败')
  }
}

function handleGroupDragEnd() {
  draggingGroupId.value = undefined
}

async function deleteEmptyGroup(group: MailGroup) {
  if (!canDeleteGroup(group) || deletingGroupId.value) {
    return
  }
  await ElMessageBox.confirm(`确定删除空分组「${group.name}」吗？`, '删除分组', {
    confirmButtonText: '删除',
    cancelButtonText: '取消',
    type: 'warning',
  })
  deletingGroupId.value = group.id
  try {
    await accountStore.deleteGroup(group.id, group.name)
    ElMessage.success('分组已删除')
  } catch (error) {
    ElMessage.error(error instanceof Error ? error.message : '删除分组失败')
  } finally {
    deletingGroupId.value = undefined
  }
}

function isViewingAccount(account: MailAccount): boolean {
  return viewingEmail.value === resolveMailboxAccount(account).email
}

function markCopied(value: string) {
  copiedValues.value = new Set(copiedValues.value).add(value)
  window.setTimeout(() => {
    const next = new Set(copiedValues.value)
    next.delete(value)
    copiedValues.value = next
  }, 1200)
}

function setFolder(folder: MailFolder) {
  mailStore.setFilter({ folder })
}

function handleAccountSelection(rows: MailAccount[]) {
  selectedAccountRows.value = rows
}

function handleTopSearchInput(value: string) {
  if (workspaceMode.value === 'mail') {
    mailStore.setFilter({ query: value })
    void mailStore.loadMessages()
    return
  }

  globalKeyword.value = value
}

async function copyText(value: string, label = '内容') {
  try {
    await navigator.clipboard.writeText(value)
    markCopied(value)
    ElMessage.success(`已复制${label}`)
  } catch {
    ElMessage.error(`复制${label}失败`)
  }
}

async function copyAccounts(format: 'email' | 'password' | 'emailPassword') {
  if (copying.value) {
    return
  }
  const targets = selectedAccountRows.value.length > 0 ? selectedAccountRows.value : visibleFlatAccounts.value
  if (targets.length === 0) {
    ElMessage.warning('没有可复制的账号')
    return
  }

  copying.value = true
  try {
    const text = targets
      .map((account) => {
        if (format === 'email') {
          return account.email
        }
        if (format === 'password') {
          return account.password
        }
        return `${account.email}----${account.password}`
      })
      .join('\n')
    await navigator.clipboard.writeText(text)
    ElMessage.success(`已复制 ${targets.length} 个账号`)
  } catch {
    ElMessage.error('复制账号失败')
  } finally {
    copying.value = false
  }
}

async function viewAccountInbox(account: MailAccount) {
  const targetAccount = resolveMailboxAccount(account)
  viewingEmail.value = targetAccount.email
  try {
    currentViewedAccount.value = targetAccount
    workspaceMode.value = 'mail'
    const result = await mailStore.viewInbox(targetAccount.email)
    await accountStore.refreshStats()
    if (result?.error) {
      ElMessage.error(result.error)
      return
    }
    if (result) {
      ElMessage.success(`已获取 ${result.synced} 封邮件`)
    }
  } finally {
    viewingEmail.value = ''
  }
}

async function refreshCurrentFolder() {
  if (!mailStore.filter.accountEmail) {
    ElMessage.warning('请先选择账号')
    return
  }
  refreshingFolder.value = true
  try {
    const result = await mailStore.syncAccountFolder(mailStore.filter.accountEmail, mailStore.filter.folder)
    await accountStore.refreshStats()
    if (result.error) {
      ElMessage.error(result.error)
      return
    }
    ElMessage.success(`已获取 ${result.synced} 封邮件`)
  } finally {
    refreshingFolder.value = false
  }
}

async function syncVisibleOrSelectedAccounts() {
  if (mailStore.batchSyncRunning) {
    return
  }
  const targets = selectedAccountRows.value.length > 0 ? selectedAccountRows.value : visibleFlatAccounts.value
  if (targets.length === 0) {
    ElMessage.warning('没有可收信的账号')
    return
  }

  const results = await mailStore.syncAccountsBatch(
    Array.from(new Set(targets.map((account) => resolveMailboxAccount(account).email))),
    'inbox',
  )
  lastBatchFailedResults.value = results
    .filter((result) => result.status === 'failed')
    .map((result) => ({ accountEmail: result.accountEmail, folder: result.folder }))

  if (mailStore.batchSyncFailed > 0) {
    ElMessage.warning(`已处理 ${mailStore.batchSyncTotal} 个账号，失败 ${mailStore.batchSyncFailed} 个`)
    return
  }
  ElMessage.success(`已完成 ${mailStore.batchSyncSuccess} 个账号收件箱刷新`)
}

async function retryFailedAccounts() {
  if (lastBatchFailedResults.value.length === 0) {
    ElMessage.warning('没有可重试的失败账号')
    return
  }

  const retryEmails = lastBatchFailedResults.value
    .filter((result) => result.folder === 'inbox')
    .map((result) => result.accountEmail)
  const results = await mailStore.syncAccountsBatch(retryEmails, 'inbox')
  lastBatchFailedResults.value = results
    .filter((result) => result.status === 'failed')
    .map((result) => ({ accountEmail: result.accountEmail, folder: result.folder }))

  if (mailStore.batchSyncFailed > 0) {
    ElMessage.warning(`重试完成，仍失败 ${mailStore.batchSyncFailed} 个`)
    return
  }
  ElMessage.success('失败账号已重试完成')
}

async function selectMessage(message: MailMessage) {
  await mailStore.selectMessage(message)
}

async function splitHotmail(account: MailAccount) {
  await ElMessageBox.confirm(`将为 ${account.email} 一次生成 5 个分裂邮箱，生成后不能重复生成。`, 'Hotmail 分裂', {
    confirmButtonText: '生成 5 个',
    cancelButtonText: '取消',
    type: 'warning',
  })
  splittingEmail.value = account.email
  try {
    await accountStore.splitHotmailAccount(account.email)
    ElMessage.success('已生成 5 个分裂邮箱')
  } finally {
    splittingEmail.value = ''
  }
}

async function editAccountRemark(account: MailAccount) {
  const result = await ElMessageBox.prompt('备注最多 500 个字符，留空可清除备注。', `编辑备注：${account.email}`, {
    confirmButtonText: '保存',
    cancelButtonText: '取消',
    inputType: 'textarea',
    inputValue: account.remark ?? '',
    inputValidator: (value) => {
      if ([...value.trim()].length > 500) {
        return '备注最多 500 个字符'
      }
      return true
    },
  })
  const remark = result.value.trim()
  if (remark === (account.remark ?? '').trim()) {
    return
  }
  editingRemarkEmail.value = account.email
  try {
    await accountStore.updateAccountRemark(account.email, remark)
    ElMessage.success(remark ? '备注已更新' : '备注已清空')
  } finally {
    editingRemarkEmail.value = ''
  }
}

async function batchDeleteSelected() {
  if (selectedAccountRows.value.length === 0) {
    ElMessage.warning('请选择账号')
    return
  }
  await ElMessageBox.confirm(`确定删除 ${selectedAccountRows.value.length} 个账号吗？`, '警告', {
    confirmButtonText: 'OK',
    cancelButtonText: 'Cancel',
    type: 'warning',
  })
  deleting.value = true
  try {
    for (const account of selectedAccountRows.value) {
      await accountStore.deleteAccount(account.email)
    }
    await mailStore.loadMessages()
    selectedAccountRows.value = []
    ElMessage.success('已删除选中账号')
  } finally {
    deleting.value = false
  }
}

function openMoveGroupDialog() {
  if (selectedAccountRows.value.length === 0) {
    ElMessage.warning('请先选择账号')
    return
  }
  targetGroupName.value = selectedAccountRows.value[0]?.group || '默认分组'
  groupDialogVisible.value = true
}

async function submitMoveGroup() {
  const group = targetGroupName.value.trim()
  if (!group) {
    ElMessage.warning('请输入分组名称')
    return
  }
  movingGroup.value = true
  try {
    await accountStore.moveAccountsToGroup(
      selectedAccountRows.value.map((account) => account.email),
      group,
    )
    accountStore.setSelectedGroup(group)
    mailStore.setFilter({ group, accountEmail: '' })
    groupDialogVisible.value = false
    ElMessage.success(`已移动 ${selectedAccountRows.value.length} 个账号到 ${group}`)
  } finally {
    movingGroup.value = false
  }
}

async function logout() {
  loggingOut.value = true
  try {
    await authStore.logout()
  } finally {
    loggingOut.value = false
  }
}

function backToAccounts() {
  workspaceMode.value = 'accounts'
  currentViewedAccount.value = undefined
  mailStore.setFilter({ accountEmail: '', query: '' })
}

watch(
  () => [
    mailStore.filter.accountEmail,
    mailStore.filter.group,
    mailStore.filter.folder,
    mailStore.filter.isRead,
    mailStore.filter.hasAttachments,
  ],
  () => {
    void mailStore.loadMessages()
  },
)

onMounted(() => {
  void mailStore.loadMessages()
})
</script>

<template>
  <section class="faka-shell">
    <aside class="faka-sidebar">
      <div class="faka-brand">
        <el-icon><Message /></el-icon>
        <span>MailBox</span>
      </div>

      <nav class="faka-nav">
        <button class="faka-nav-item active" type="button" @click="backToAccounts">
          <el-icon><House /></el-icon>
          <span>账号</span>
        </button>
        <div class="faka-nav-title">
          <span>
            <el-icon><FolderOpened /></el-icon>
            分组列表
          </span>
        </div>
        <button
          class="faka-nav-item"
          :class="{ active: !accountStore.selectedGroup }"
          type="button"
          @click="setGroup('')"
        >
          <el-icon><FolderOpened /></el-icon>
          <span>全部</span>
        </button>
        <button
          v-for="group in accountStore.remoteGroups"
          :key="group.id"
          class="faka-nav-item group-nav-item"
          :class="{ active: accountStore.selectedGroup === group.name, dragging: draggingGroupId === group.id }"
          type="button"
          draggable="true"
          @click="setGroup(group.name)"
          @dragstart="handleGroupDragStart(group, $event)"
          @dragover="handleGroupDragOver"
          @drop="handleGroupDrop(group, $event)"
          @dragend="handleGroupDragEnd"
        >
          <span class="drag-handle" aria-hidden="true">⋮⋮</span>
          <el-icon><FolderOpened /></el-icon>
          <span>{{ group.name }}</span>
          <small>{{ groupAccountCount(group) }}</small>
          <el-button
            v-if="canDeleteGroup(group)"
            class="group-delete-button"
            link
            :icon="Delete"
            :loading="deletingGroupId === group.id"
            :disabled="deletingGroupId === group.id"
            @click.stop="deleteEmptyGroup(group)"
          />
        </button>
        <div class="sidebar-account-list">
          <div
            v-for="account in sidebarRootAccounts"
            :key="account.email"
            class="sidebar-account-group"
          >
            <button
              class="faka-nav-item account-shortcut parent-shortcut"
              :class="{ active: currentViewedAccount?.email === account.email && workspaceMode === 'mail' }"
              type="button"
              :disabled="isViewingAccount(account)"
              @click="viewAccountInbox(account)"
            >
              <el-icon><Message /></el-icon>
              <span class="shortcut-email">{{ account.email }}</span>
            </button>
          </div>
        </div>
      </nav>

      <div class="faka-total-card">
        <el-icon><Files /></el-icon>
        <span>账号</span>
        <strong>{{ accountStore.accounts.length }}</strong>
      </div>
    </aside>

    <main class="faka-main">
      <header class="faka-topbar">
        <el-input
          :model-value="topSearchValue"
          class="faka-search"
          :prefix-icon="Search"
          clearable
          :placeholder="workspaceMode === 'mail' ? '搜索主题/发件人...' : '搜索邮件或账号...'"
          @update:model-value="handleTopSearchInput"
        />
        <div class="topbar-actions">
          <el-button v-if="workspaceMode === 'mail'" plain @click="backToAccounts">返回账号</el-button>
          <el-button plain :loading="loggingOut" :disabled="loggingOut" @click="logout">退出登录</el-button>
        </div>
      </header>

      <Transition name="workspace-view" mode="out-in">
      <section v-if="workspaceMode === 'accounts'" key="accounts" class="faka-card">
        <div class="faka-action-row">
          <el-button type="primary" :icon="UploadFilled" @click="emit('importAccounts')">导入账号</el-button>
          <el-button
            :icon="Refresh"
            :loading="mailStore.batchSyncRunning"
            :disabled="mailStore.batchSyncRunning"
            @click="syncVisibleOrSelectedAccounts"
          >
            收信
          </el-button>
          <el-dropdown trigger="click">
            <el-button :icon="CopyDocument" :loading="copying" :disabled="copying">复制</el-button>
            <template #dropdown>
              <el-dropdown-menu>
                <el-dropdown-item @click="copyAccounts('email')">仅复制邮箱</el-dropdown-item>
                <el-dropdown-item divided @click="copyAccounts('emailPassword')">邮箱----密码</el-dropdown-item>
              </el-dropdown-menu>
            </template>
          </el-dropdown>
          <el-button :icon="FolderOpened" :disabled="movingGroup || deleting" @click="openMoveGroupDialog">分组</el-button>
          <el-button type="danger" :icon="Delete" :loading="deleting" :disabled="deleting" @click="batchDeleteSelected">删除</el-button>
          <el-button :icon="Download" :loading="exporting" :disabled="exporting" @click="emit('exportData')">导出</el-button>
        </div>

        <Transition name="panel-slide">
          <div
            v-if="mailStore.batchSyncRunning || mailStore.batchSyncResults.length > 0"
            class="batch-sync-panel"
          >
            <div class="batch-sync-head">
              <span>
                收信：
                {{ mailStore.batchSyncDone }} / {{ mailStore.batchSyncTotal }}
              </span>
              <strong>
                成功 {{ mailStore.batchSyncSuccess }}，
                失败 {{ mailStore.batchSyncFailed }}
              </strong>
            </div>
            <el-progress
              :percentage="batchProgressPercent"
              :status="mailStore.batchSyncFailed > 0 && !mailStore.batchSyncRunning ? 'exception' : undefined"
            />
            <Transition name="content-fade">
              <div v-if="batchFailedResults.length > 0" class="batch-error-list">
                <div
                  v-for="result in batchFailedResults"
                  :key="`${result.accountEmail}-${result.folder}`"
                  class="batch-error-item"
                >
                  <span>{{ result.accountEmail }}</span>
                  <small>{{ result.error }}</small>
                </div>
                <el-button
                  size="small"
                  type="warning"
                  plain
                  :loading="mailStore.batchSyncRunning"
                  @click="retryFailedAccounts"
                >
                  重试失败账号
                </el-button>
              </div>
            </Transition>
          </div>
        </Transition>

        <el-table
          v-loading="accountStore.loading || deleting || movingGroup || clearingData"
          :data="visibleAccounts"
          row-key="email"
          class="faka-account-table"
          height="calc(100vh - 232px)"
          :default-expand-all="false"
          :tree-props="{ children: 'children' }"
          :row-class-name="({ row }: { row: MailAccount }) => (isSplitAccount(row) ? 'split-account-row' : 'parent-account-row')"
          @selection-change="handleAccountSelection"
        >
          <el-table-column type="selection" width="52" align="center" header-align="center" />
          <el-table-column label="" width="68" align="center" header-align="center" class-name="split-marker-column">
            <template #default="{ row }">
              <span v-if="isSplitAccount(row)" class="split-marker">{{ splitLabel(row) }}</span>
            </template>
          </el-table-column>
          <el-table-column label="#" width="64" align="center" header-align="center">
            <template #default="{ row }">
              <span class="row-number" :class="{ split: isSplitAccount(row) }">
                {{ accountRowLabel(row) }}
              </span>
            </template>
          </el-table-column>
          <el-table-column label="邮箱" min-width="300" show-overflow-tooltip align="center" header-align="center">
            <template #default="{ row }">
              <div class="copy-cell" :class="{ copied: copiedValues.has(row.email) }">
                <span>{{ row.email }}</span>
                <el-tooltip content="复制" placement="top">
                  <el-button link :icon="CopyDocument" @click.stop="copyText(row.email, '邮箱')" />
                </el-tooltip>
              </div>
            </template>
          </el-table-column>
          <el-table-column label="密码" min-width="170" show-overflow-tooltip align="center" header-align="center">
            <template #default="{ row }">
              <div class="copy-cell" :class="{ copied: copiedValues.has(row.password) }">
                <span>{{ row.password }}</span>
                <el-tooltip content="复制" placement="top">
                  <el-button link :icon="CopyDocument" @click.stop="copyText(row.password, '密码')" />
                </el-tooltip>
              </div>
            </template>
          </el-table-column>
          <el-table-column label="邮件" width="90" align="center" header-align="center">
            <template #default="{ row }">
              <span class="mail-count">{{ messageCountByEmail.get(resolveMailboxAccount(row).email) ?? 0 }}</span>
            </template>
          </el-table-column>
          <el-table-column label="状态" width="100" align="center" header-align="center">
            <template #default="{ row }">
              <el-tag :type="statusType[row.status as AccountStatus]" size="small" effect="light">
                {{ statusText[row.status as AccountStatus] }}
              </el-tag>
            </template>
          </el-table-column>
          <el-table-column prop="group" label="分组" width="140" align="center" header-align="center" />
          <el-table-column label="备注" min-width="190" show-overflow-tooltip align="center" header-align="center">
            <template #default="{ row }">
              <div class="remark-cell">
                <span :class="{ muted: !row.remark }">{{ row.remark || '无备注' }}</span>
                <el-tooltip content="编辑备注" placement="top">
                  <el-button
                    link
                    :icon="EditPen"
                    :loading="editingRemarkEmail === row.email"
                    :disabled="editingRemarkEmail === row.email"
                    @click.stop="editAccountRemark(row)"
                  />
                </el-tooltip>
              </div>
            </template>
          </el-table-column>
          <el-table-column
            label="导入"
            width="170"
            sortable
            :sort-method="(left: MailAccount, right: MailAccount) => sortByDateTime(left, right, 'createdAt')"
            align="center"
            header-align="center"
          >
            <template #default="{ row }">
              {{ formatDateTime(row.createdAt) }}
            </template>
          </el-table-column>
          <el-table-column label="操作" width="180" fixed="right" align="center" header-align="center">
            <template #default="{ row }">
              <el-space :size="8" class="row-actions">
                <template v-if="!isSplitAccount(row)">
                  <el-button
                    size="small"
                    type="primary"
                    :loading="isViewingAccount(row)"
                    :disabled="isViewingAccount(row)"
                    @click="viewAccountInbox(row)"
                  >
                    查看
                  </el-button>
                  <el-tooltip
                    :content="splitCount(row) > 0 ? '已分裂' : '生成 5 个分裂邮箱'"
                    placement="top"
                  >
                    <el-button
                      size="small"
                      type="success"
                      plain
                      :loading="splittingEmail === row.email"
                      :disabled="!canSplitHotmail(row)"
                      @click="splitHotmail(row)"
                    >
                      分裂
                    </el-button>
                  </el-tooltip>
                </template>
              </el-space>
            </template>
          </el-table-column>
        </el-table>
        <div class="faka-pagination">
          <span>Total {{ visibleFlatAccounts.length }}</span>
          <el-pagination size="small" layout="prev, pager, next" :total="visibleFlatAccounts.length" :page-size="20" />
        </div>
      </section>

      <section v-else key="mail" class="faka-mail-workspace">
        <section class="faka-mail-list">
          <div class="mail-list-toolbar">
            <div class="mail-folder-row">
              <el-segmented
                :model-value="mailStore.filter.folder"
                :options="[
                  { label: '收件箱', value: 'inbox' },
                  { label: '垃圾箱', value: 'junkemail' },
                ]"
                @update:model-value="setFolder($event as MailFolder)"
              />
              <el-tag effect="plain">{{ visibleMessages.length }}</el-tag>
            </div>
            <div class="mail-command-row">
              <el-button
                :icon="Refresh"
                type="primary"
                :loading="refreshingFolder"
                :disabled="refreshingFolder"
                @click="refreshCurrentFolder"
              >
                获取新邮件
              </el-button>
              <el-button :icon="Sort" @click="mailSortDesc = !mailSortDesc">排序</el-button>
            </div>
          </div>
          <Transition name="content-fade">
            <div v-if="currentAccountSyncing" class="mail-sync-hint">
              正在获取新邮件，本地邮件可先查看
            </div>
          </Transition>
          <Transition name="content-fade" mode="out-in">
            <el-empty v-if="!mailStore.loading && visibleMessages.length === 0" key="empty" description="暂无邮件" />
            <el-scrollbar v-else key="list" v-loading="mailStore.loading || currentAccountSyncing" class="mail-list-scrollbar">
              <TransitionGroup name="mail-item" tag="div">
                <button
                  v-for="message in visibleMessages"
                  :key="`${message.accountEmail}-${message.folder}-${message.messageId}`"
                  class="faka-mail-item"
                  :class="{ active: mailStore.selectedMessage?.messageId === message.messageId }"
                  @click="selectMessage(message)"
                >
                  <div class="mail-item-line">
                    <strong>{{ message.from?.email || '未知发件人' }}</strong>
                    <span>{{ formatDateTime(message.receivedAt) }}</span>
                  </div>
                  <h3>{{ message.subject || '无主题' }}</h3>
                </button>
              </TransitionGroup>
              <div class="load-more-row">
                <el-button :disabled="!mailStore.hasMore" @click="mailStore.loadMore()">加载更多</el-button>
              </div>
            </el-scrollbar>
          </Transition>
        </section>

        <section class="faka-reader">
          <Transition name="content-fade" mode="out-in">
            <el-empty v-if="!mailStore.selectedMessage" key="empty-reader" description="选择一封邮件开始阅读">
              <el-icon class="reader-empty-icon"><Reading /></el-icon>
            </el-empty>
            <div v-else :key="mailStore.selectedMessage.messageId" class="reader-content">
              <div class="reader-head">
                <div>
                  <h2>{{ mailStore.selectedMessage.subject || '无主题' }}</h2>
                  <p>发件人：{{ mailStore.selectedMessage.from?.email || '未知' }}</p>
                  <p>收件人：{{ formatAddressList(mailStore.selectedMessage.to) }}</p>
                  <p>查看账号：{{ mailStore.selectedMessage.accountEmail }}</p>
                  <p>时间：{{ formatDateTime(mailStore.selectedMessage.receivedAt) }}</p>
                </div>
                <el-tag :type="mailStore.selectedMessage.isRead ? 'info' : 'success'">
                  {{ mailStore.selectedMessage.isRead ? '已读' : '未读' }}
                </el-tag>
              </div>
              <Transition name="content-fade" mode="out-in">
                <el-skeleton v-if="mailStore.bodyLoading" key="skeleton" :rows="6" animated />
                <iframe
                  v-else-if="selectedHtml"
                  key="html-body"
                  class="mail-body-frame"
                  sandbox="allow-popups allow-popups-to-escape-sandbox"
                  :srcdoc="selectedHtml"
                  title="邮件正文"
                />
                <pre v-else key="plain-body" class="mail-body plain">{{ mailStore.selectedBody?.content || '暂无正文内容' }}</pre>
              </Transition>
            </div>
          </Transition>
        </section>
      </section>
      </Transition>

      <el-dialog v-model="groupDialogVisible" title="设置分组" width="420px">
        <el-form label-position="top">
          <el-form-item label="请输入分组名称">
            <el-input
              v-model="targetGroupName"
              clearable
              placeholder="分组名称"
              @keyup.enter="submitMoveGroup"
            />
          </el-form-item>
        </el-form>
        <template #footer>
          <el-button @click="groupDialogVisible = false">取消</el-button>
          <el-button
            type="primary"
            :loading="movingGroup"
            :disabled="!targetGroupName.trim() || movingGroup"
            @click="submitMoveGroup"
          >
            确定
          </el-button>
        </template>
      </el-dialog>
    </main>
  </section>
</template>
