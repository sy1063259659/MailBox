<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import {
  Reading,
  Refresh,
  Sort,
} from '@element-plus/icons-vue'
import MailboxAccountPanel from '@/components/MailboxAccountPanel.vue'
import MailboxSidebar from '@/components/MailboxSidebar.vue'
import MailboxTopbar from '@/components/MailboxTopbar.vue'
import { useLocalUiState } from '@/composables/useLocalUiState'
import { useAccountStore } from '@/stores/account'
import { useMailStore } from '@/stores/mail'
import type {
  AccountStatus,
  MailAccount,
  MailAddress,
  MailFolder,
  MailMessage,
} from '@/types'
import type { MailGroup } from '@/services/accountApi'
import { formatDateTime } from '@/utils/dateTime'

type WorkspaceMode = 'accounts' | 'mail'

const accountStore = useAccountStore()
const mailStore = useMailStore()
const selectedAccountRows = ref<MailAccount[]>([])
const accountPage = ref(1)
const accountPageSize = ref(20)
const topSearchDraft = ref('')
const globalKeyword = useLocalUiState('gptbox.ui.globalKeyword', '', {
  validate: isString,
  legacyKey: 'mailbox.ui.globalKeyword',
})
const groupDialogVisible = ref(false)
const targetGroupName = ref('')
const currentViewedAccount = ref<MailAccount>()
const currentViewedAccountEmail = useLocalUiState('gptbox.ui.currentViewedAccount.email', '', {
  validate: isString,
  legacyKey: 'mailbox.ui.currentViewedAccount.email',
})
const workspaceMode = useLocalUiState<WorkspaceMode>('gptbox.ui.workspaceMode', 'accounts', {
  validate: isWorkspaceMode,
  legacyKey: 'mailbox.ui.workspaceMode',
})
const mailSortDesc = useLocalUiState('gptbox.ui.mailSortDesc', true, {
  validate: (value): value is boolean => typeof value === 'boolean',
  legacyKey: 'mailbox.ui.mailSortDesc',
})
const lastBatchFailedResults = ref<{ accountEmail: string; folder: MailFolder }[]>([])
const viewingEmail = ref('')
const splittingEmail = ref('')
const deleting = ref(false)
const movingGroup = ref(false)
const refreshingFolder = ref(false)
const copying = ref(false)
const exportingAccounts = ref(false)
const copiedValues = ref<Set<string>>(new Set())
const editingRemarkEmail = ref('')
const draggingGroupId = ref<number>()
const deletingGroupId = ref<number>()
const renamingGroupId = ref<number>()

defineProps<{
  clearingData?: boolean
}>()

const emit = defineEmits<{
  importAccounts: []
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

function isWorkspaceMode(value: unknown): value is WorkspaceMode {
  return value === 'accounts' || value === 'mail'
}

function isString(value: unknown): value is string {
  return typeof value === 'string'
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

const pagedFlatAccounts = computed(() => {
  const start = (accountPage.value - 1) * accountPageSize.value
  return visibleFlatAccounts.value.slice(start, start + accountPageSize.value)
})

const selectionScopeText = computed(() =>
  selectedAccountRows.value.length > 0
    ? '批量收信、复制、导出、移动分组、删除会优先作用于已选账号'
    : '未勾选时，批量收信、复制、导出会作用于当前筛选范围',
)

const sidebarRootAccounts = computed(() => accountTree.value)


const accountByEmail = computed(() => {
  const map = new Map<string, MailAccount>()
  for (const account of accountStore.accounts) {
    map.set(account.email.toLowerCase(), account)
    for (const child of account.children ?? []) {
      map.set(child.email.toLowerCase(), child)
    }
  }
  return map
})

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

function flattenAccounts(accounts: MailAccount[]): MailAccount[] {
  return accounts.flatMap((account) => [account, ...(account.children ?? [])])
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

function setGroup(group: string) {
  accountStore.setSelectedGroup(group)
  mailStore.setFilter({ group, accountEmail: '' })
  workspaceMode.value = 'accounts'
  accountPage.value = 1
}

function canDeleteGroup(group: MailGroup): boolean {
  return group.name !== '默认分组' && (groupCountByName.value.get(group.name) ?? 0) === 0
}

function canRenameGroup(group: MailGroup): boolean {
  return group.name !== '默认分组'
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

async function renameGroup(group: MailGroup) {
  if (!canRenameGroup(group) || renamingGroupId.value) {
    return
  }
  const result = await ElMessageBox.prompt('请输入新的分组名称。', `重命名分组：${group.name}`, {
    confirmButtonText: '保存',
    cancelButtonText: '取消',
    inputValue: group.name,
    inputValidator: (value) => {
      if (!value.trim()) {
        return '分组名称不能为空'
      }
      return true
    },
  })
  const nextName = result.value.trim()
  if (nextName === group.name) {
    return
  }

  renamingGroupId.value = group.id
  try {
    await accountStore.renameGroup(group.id, group.name, nextName)
    if (mailStore.filter.group === group.name) {
      mailStore.setFilter({ group: nextName, accountEmail: '' })
    }
    ElMessage.success('分组已重命名')
  } catch (error) {
    ElMessage.error(error instanceof Error ? error.message : '重命名分组失败')
  } finally {
    renamingGroupId.value = undefined
  }
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

let topSearchDebounceTimer: number | undefined

function handleTopSearchInput(value: string) {
  topSearchDraft.value = value
  if (topSearchDebounceTimer) {
    window.clearTimeout(topSearchDebounceTimer)
    topSearchDebounceTimer = undefined
  }

  if (workspaceMode.value === 'mail') {
    const applyMailSearch = () => {
      mailStore.setFilter({ query: value })
      void mailStore.loadMessages()
    }
    if (!value.trim()) {
      applyMailSearch()
      return
    }
    topSearchDebounceTimer = window.setTimeout(applyMailSearch, 250)
    return
  }

  const applyAccountSearch = () => {
    globalKeyword.value = value
    accountPage.value = 1
  }
  if (!value.trim()) {
    applyAccountSearch()
    return
  }
  topSearchDebounceTimer = window.setTimeout(applyAccountSearch, 250)
}

function handleAccountPageChange(page: number) {
  accountPage.value = page
  selectedAccountRows.value = []
}

function handleAccountPageSizeChange(size: number) {
  accountPageSize.value = size
  accountPage.value = 1
  selectedAccountRows.value = []
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

function exportTargets(): MailAccount[] {
  return selectedAccountRows.value.length > 0 ? selectedAccountRows.value : visibleFlatAccounts.value
}

async function copyExportText() {
  if (exportingAccounts.value) {
    return
  }
  const targets = exportTargets()
  if (targets.length === 0) {
    ElMessage.warning('没有可导出的账号')
    return
  }

  exportingAccounts.value = true
  try {
    const text = await accountStore.exportData(targets.map((account) => account.email))
    if (!text.trim()) {
      ElMessage.warning('没有可导出的账号')
      return
    }
    await navigator.clipboard.writeText(text)
    ElMessage.success(`已复制 ${targets.length} 个账号导出文本`)
  } catch {
    ElMessage.error('复制导出文本失败')
  } finally {
    exportingAccounts.value = false
  }
}

async function downloadExportText() {
  const targets = exportTargets()
  if (targets.length === 0) {
    ElMessage.warning('没有可导出的账号')
    return
  }

  exportingAccounts.value = true
  try {
    const text = await accountStore.exportData(targets.map((account) => account.email))
    if (!text.trim()) {
      ElMessage.warning('没有可导出的账号')
      return
    }
    const blob = new Blob([text], {
      type: 'text/plain;charset=utf-8',
    })
    const url = URL.createObjectURL(blob)
    const anchor = document.createElement('a')
    anchor.href = url
    anchor.download = `mailbox-accounts-${new Date().toISOString().slice(0, 10)}.txt`
    anchor.click()
    URL.revokeObjectURL(url)
    ElMessage.success(`已导出 ${targets.length} 个账号`)
  } finally {
    exportingAccounts.value = false
  }
}

async function viewAccountInbox(account: MailAccount) {
  const targetAccount = resolveMailboxAccount(account)
  viewingEmail.value = targetAccount.email
  try {
    currentViewedAccount.value = targetAccount
    currentViewedAccountEmail.value = targetAccount.email
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
    accountPage.value = 1
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
    accountPage.value = 1
    groupDialogVisible.value = false
    ElMessage.success(`已移动 ${selectedAccountRows.value.length} 个账号到 ${group}`)
  } finally {
    movingGroup.value = false
  }
}

function backToAccounts() {
  workspaceMode.value = 'accounts'
  currentViewedAccount.value = undefined
  currentViewedAccountEmail.value = ''
  mailStore.setFilter({ accountEmail: '', query: '' })
}

watch(
  topSearchValue,
  (value) => {
    topSearchDraft.value = value
  },
  { immediate: true },
)

watch(
  () => [accountStore.selectedGroup, visibleFlatAccounts.value.length],
  ([, visibleCount], [, previousVisibleCount]) => {
    if (visibleCount !== previousVisibleCount) {
      accountPage.value = 1
    }
    const maxPage = Math.max(1, Math.ceil(visibleFlatAccounts.value.length / accountPageSize.value))
    if (accountPage.value > maxPage) {
      accountPage.value = maxPage
    }
  },
)

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

watch(
  [accountByEmail, currentViewedAccountEmail],
  ([accounts, email]) => {
    if (!email) {
      if (workspaceMode.value === 'mail') {
        workspaceMode.value = 'accounts'
      }
      currentViewedAccount.value = undefined
      return
    }

    const restoredAccount = accounts.get(email.toLowerCase())
    if (!restoredAccount && accounts.size === 0) {
      return
    }

    if (!restoredAccount) {
      workspaceMode.value = 'accounts'
      currentViewedAccount.value = undefined
      currentViewedAccountEmail.value = ''
      if (mailStore.filter.accountEmail) {
        mailStore.setFilter({ accountEmail: '', query: '' })
      }
      return
    }

    const targetAccount = resolveMailboxAccount(restoredAccount)
    currentViewedAccount.value = targetAccount
    if (workspaceMode.value === 'mail' && mailStore.filter.accountEmail !== targetAccount.email) {
      mailStore.setFilter({
        accountEmail: targetAccount.email,
        group: '',
        folder: 'inbox',
        query: '',
        isRead: undefined,
        hasAttachments: undefined,
      })
    }
  },
  { immediate: true },
)

onBeforeUnmount(() => {
  if (topSearchDebounceTimer) {
    window.clearTimeout(topSearchDebounceTimer)
  }
})
</script>

<template>
  <section class="faka-shell">
    <MailboxSidebar
      :remote-groups="accountStore.remoteGroups"
      :selected-group="accountStore.selectedGroup"
      :workspace-mode="workspaceMode"
      :sidebar-root-accounts="sidebarRootAccounts"
      :current-viewed-account-email="currentViewedAccount?.email"
      :viewing-email="viewingEmail"
      :account-count="accountStore.accounts.length"
      :group-counts="groupCountByName"
      :dragging-group-id="draggingGroupId"
      :deleting-group-id="deletingGroupId"
      :renaming-group-id="renamingGroupId"
      @back-to-accounts="backToAccounts"
      @set-group="setGroup"
      @view-account="viewAccountInbox"
      @group-drag-start="handleGroupDragStart"
      @group-drag-over="handleGroupDragOver"
      @group-drop="handleGroupDrop"
      @group-drag-end="handleGroupDragEnd"
      @rename-group="renameGroup"
      @delete-group="deleteEmptyGroup"
    />

    <main class="faka-main">
      <MailboxTopbar
        :search-value="topSearchDraft"
        :workspace-mode="workspaceMode"
        @search-input="handleTopSearchInput"
        @back-to-accounts="backToAccounts"
      />

      <MailboxAccountPanel
        v-if="workspaceMode === 'accounts'"
        :clearing-data="clearingData"
        :loading="accountStore.loading"
        :deleting="deleting"
        :moving-group="movingGroup"
        :copying="copying"
        :exporting-accounts="exportingAccounts"
        :selected-count="selectedAccountRows.length"
        :selection-scope-text="selectionScopeText"
        :batch-sync-running="mailStore.batchSyncRunning"
        :batch-sync-results-count="mailStore.batchSyncResults.length"
        :batch-sync-done="mailStore.batchSyncDone"
        :batch-sync-total="mailStore.batchSyncTotal"
        :batch-sync-success="mailStore.batchSyncSuccess"
        :batch-sync-failed="mailStore.batchSyncFailed"
        :batch-progress-percent="batchProgressPercent"
        :batch-failed-results="batchFailedResults"
        :paged-accounts="pagedFlatAccounts"
        :visible-total="visibleFlatAccounts.length"
        :account-page="accountPage"
        :account-page-size="accountPageSize"
        :copied-values="copiedValues"
        :message-count-by-email="messageCountByEmail"
        :account-by-email="accountByEmail"
        :parent-row-index-map="parentRowIndexMap"
        :editing-remark-email="editingRemarkEmail"
        :viewing-email="viewingEmail"
        :splitting-email="splittingEmail"
        :status-type="statusType"
        :status-text="statusText"
        @import-accounts="emit('importAccounts')"
        @sync-accounts="syncVisibleOrSelectedAccounts"
        @copy-accounts="copyAccounts"
        @open-move-group-dialog="openMoveGroupDialog"
        @batch-delete-selected="batchDeleteSelected"
        @copy-export-text="copyExportText"
        @download-export-text="downloadExportText"
        @retry-failed-accounts="retryFailedAccounts"
        @selection-change="handleAccountSelection"
        @copy-text="copyText"
        @edit-account-remark="editAccountRemark"
        @view-account-inbox="viewAccountInbox"
        @split-hotmail="splitHotmail"
        @update-account-page="handleAccountPageChange"
        @update-account-page-size="handleAccountPageSizeChange"
      />

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
