<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import {
  CopyDocument,
  Delete,
  Download,
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
import type { AccountStatus, MailAccount, MailFolder, MailMessage } from '@/types'

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

const emit = defineEmits<{
  importAccounts: []
  exportData: []
  clearData: []
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

const visibleAccounts = computed(() => {
  const keyword = accountSearchKeyword.value.trim().toLocaleLowerCase()
  if (!keyword) {
    return accountStore.filteredAccounts
  }
  return accountStore.filteredAccounts.filter((account) =>
    [account.email, account.password, account.group, account.status]
      .filter(Boolean)
      .some((value) => value.toLocaleLowerCase().includes(keyword)),
  )
})

const sidebarAccounts = computed(() =>
  accountStore.accounts
    .filter((account) => !accountStore.selectedGroup || account.group === accountStore.selectedGroup),
)

const messageCountByEmail = computed(() => {
  const counts = new Map<string, number>()
  for (const message of mailStore.messages) {
    counts.set(message.accountEmail, (counts.get(message.accountEmail) ?? 0) + 1)
  }
  return counts
})

const visibleMessages = computed(() => {
  const messages = [...mailStore.messages]
  messages.sort((left, right) =>
    mailSortDesc.value
      ? right.receivedAt.localeCompare(left.receivedAt)
      : left.receivedAt.localeCompare(right.receivedAt),
  )
  return messages
})

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

function setGroup(group: string) {
  accountStore.setSelectedGroup(group)
  mailStore.setFilter({ group, accountEmail: '' })
  workspaceMode.value = 'accounts'
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

function copyText(value: string, label = '内容') {
  void navigator.clipboard.writeText(value)
  ElMessage.success(`已复制${label}`)
}

async function copyAccounts(format: 'email' | 'password' | 'emailPassword') {
  const targets = selectedAccountRows.value.length > 0 ? selectedAccountRows.value : visibleAccounts.value
  if (targets.length === 0) {
    ElMessage.warning('没有可复制的账号')
    return
  }

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
}

async function viewAccountInbox(account: MailAccount) {
  currentViewedAccount.value = account
  workspaceMode.value = 'mail'
  await mailStore.viewInbox(account.email)
  await accountStore.refreshStats()
}

async function refreshCurrentFolder() {
  if (!mailStore.filter.accountEmail) {
    ElMessage.warning('请先选择账号')
    return
  }
  const result = await mailStore.syncAccountFolder(mailStore.filter.accountEmail, mailStore.filter.folder)
  await accountStore.refreshStats()
  if (result.error) {
    ElMessage.error(result.error)
    return
  }
  ElMessage.success(`已获取 ${result.synced} 封邮件`)
}

async function syncVisibleOrSelectedAccounts() {
  const targets = selectedAccountRows.value.length > 0 ? selectedAccountRows.value : visibleAccounts.value
  if (targets.length === 0) {
    ElMessage.warning('没有可收信的账号')
    return
  }

  let failed = 0
  for (const account of targets) {
    const result = await mailStore.syncAccountFolder(account.email, 'inbox')
    if (result.error) {
      failed += 1
    }
  }
  await accountStore.refreshStats()
  if (failed > 0) {
    ElMessage.warning(`已处理 ${targets.length} 个账号，失败 ${failed} 个`)
    return
  }
  ElMessage.success(`已完成 ${targets.length} 个账号收件箱刷新`)
}

async function selectMessage(message: MailMessage) {
  await mailStore.selectMessage(message)
}

async function deleteAccount(account: MailAccount) {
  await ElMessageBox.confirm(`确认删除 ${account.email} 及其本地邮件数据？`, '删除账号', {
    confirmButtonText: '删除',
    cancelButtonText: '取消',
    type: 'warning',
  })
  await accountStore.deleteAccount(account.email)
  await mailStore.loadMessages()
  if (currentViewedAccount.value?.email === account.email) {
    currentViewedAccount.value = undefined
    workspaceMode.value = 'accounts'
  }
  if (globalKeyword.value && !visibleAccounts.value.some((item) => item.email.includes(globalKeyword.value))) {
    globalKeyword.value = ''
  }
  ElMessage.success('账号已删除')
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
  for (const account of selectedAccountRows.value) {
    await accountStore.deleteAccount(account.email)
  }
  await mailStore.loadMessages()
  selectedAccountRows.value = []
  ElMessage.success('已删除选中账号')
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
  await accountStore.moveAccountsToGroup(
    selectedAccountRows.value.map((account) => account.email),
    group,
  )
  accountStore.setSelectedGroup(group)
  mailStore.setFilter({ group, accountEmail: '' })
  groupDialogVisible.value = false
  ElMessage.success(`已移动 ${selectedAccountRows.value.length} 个账号到 ${group}`)
}

async function logout() {
  await authStore.logout()
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
          <span>账号总览面板</span>
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
          v-for="group in accountStore.groups"
          :key="group"
          class="faka-nav-item"
          :class="{ active: accountStore.selectedGroup === group }"
          type="button"
          @click="setGroup(group)"
        >
          <el-icon><FolderOpened /></el-icon>
          <span>{{ group }}</span>
        </button>
        <div class="sidebar-account-list">
          <button
            v-for="account in sidebarAccounts"
            :key="account.email"
            class="faka-nav-item account-shortcut"
            type="button"
            @click="viewAccountInbox(account)"
          >
            <el-icon><Message /></el-icon>
            <span>{{ account.email }}</span>
          </button>
        </div>
      </nav>

      <div class="faka-total-card">
        <el-icon><Files /></el-icon>
        <span>总邮箱数</span>
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
          <el-button plain @click="logout">退出登录</el-button>
        </div>
      </header>

      <section v-if="workspaceMode === 'accounts'" class="faka-card">
        <div class="faka-action-row">
          <el-button type="primary" :icon="UploadFilled" @click="emit('importAccounts')">导入账号</el-button>
          <el-button :icon="Refresh" @click="syncVisibleOrSelectedAccounts">批量收信</el-button>
          <el-dropdown trigger="click">
            <el-button :icon="CopyDocument">复制信息</el-button>
            <template #dropdown>
              <el-dropdown-menu>
                <el-dropdown-item @click="copyAccounts('email')">仅复制邮箱</el-dropdown-item>
                <el-dropdown-item @click="copyAccounts('password')">仅复制密码</el-dropdown-item>
                <el-dropdown-item divided @click="copyAccounts('emailPassword')">复制 邮箱----密码</el-dropdown-item>
              </el-dropdown-menu>
            </template>
          </el-dropdown>
          <el-button :icon="FolderOpened" @click="openMoveGroupDialog">批量分组</el-button>
          <el-button type="danger" :icon="Delete" @click="batchDeleteSelected">批量删除</el-button>
          <el-button :icon="Download" @click="emit('exportData')">导出数据</el-button>
          <el-button :icon="Delete" plain @click="emit('clearData')">清空本地</el-button>
        </div>

        <el-table
          :data="visibleAccounts"
          row-key="email"
          class="faka-account-table"
          height="calc(100vh - 232px)"
          @selection-change="handleAccountSelection"
        >
          <el-table-column type="selection" width="52" align="center" header-align="center" />
          <el-table-column type="index" label="#" width="64" align="center" header-align="center" />
          <el-table-column label="邮箱地址" min-width="260" show-overflow-tooltip align="center" header-align="center">
            <template #default="{ row }">
              <div class="copy-cell">
                <span>{{ row.email }}</span>
                <el-tooltip content="复制" placement="top">
                  <el-button link :icon="CopyDocument" @click.stop="copyText(row.email, '邮箱')" />
                </el-tooltip>
              </div>
            </template>
          </el-table-column>
          <el-table-column label="密码" min-width="170" show-overflow-tooltip align="center" header-align="center">
            <template #default="{ row }">
              <div class="copy-cell">
                <span>{{ row.password }}</span>
                <el-tooltip content="复制" placement="top">
                  <el-button link :icon="CopyDocument" @click.stop="copyText(row.password, '密码')" />
                </el-tooltip>
              </div>
            </template>
          </el-table-column>
          <el-table-column label="邮件数" width="95" align="center" header-align="center">
            <template #default="{ row }">
              <span class="mail-count">{{ messageCountByEmail.get(row.email) ?? 0 }}</span>
            </template>
          </el-table-column>
          <el-table-column label="状态" width="110" align="center" header-align="center">
            <template #default="{ row }">
              <el-tag :type="statusType[row.status as AccountStatus]" size="small" effect="light">
                {{ statusText[row.status as AccountStatus] }}
              </el-tag>
            </template>
          </el-table-column>
          <el-table-column prop="group" label="分组" width="140" align="center" header-align="center" />
          <el-table-column label="操作" width="170" fixed="right" align="center" header-align="center">
            <template #default="{ row }">
              <el-space :size="8" class="row-actions">
                <el-button size="small" type="primary" @click="viewAccountInbox(row)">查看</el-button>
                <el-button size="small" type="danger" plain @click="deleteAccount(row)">删除</el-button>
              </el-space>
            </template>
          </el-table-column>
        </el-table>
        <div class="faka-pagination">
          <span>Total {{ visibleAccounts.length }}</span>
          <el-pagination size="small" layout="prev, pager, next" :total="visibleAccounts.length" :page-size="20" />
        </div>
      </section>

      <section v-else class="faka-mail-workspace">
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
              <el-button :icon="Refresh" type="primary" @click="refreshCurrentFolder">获取新邮件</el-button>
              <el-button :icon="Sort" @click="mailSortDesc = !mailSortDesc">排序</el-button>
            </div>
          </div>
          <el-empty v-if="!mailStore.loading && visibleMessages.length === 0" description="暂无邮件" />
          <el-scrollbar v-else v-loading="mailStore.loading" class="mail-list-scrollbar">
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
              <p>{{ message.preview || '暂无预览' }}</p>
            </button>
            <div class="load-more-row">
              <el-button :disabled="!mailStore.hasMore" @click="mailStore.loadMore()">加载更多</el-button>
            </div>
          </el-scrollbar>
        </section>

        <section class="faka-reader">
          <el-empty v-if="!mailStore.selectedMessage" description="选择一封邮件开始阅读">
            <el-icon class="reader-empty-icon"><Reading /></el-icon>
          </el-empty>
          <template v-else>
            <div class="reader-head">
              <div>
                <h2>{{ mailStore.selectedMessage.subject || '无主题' }}</h2>
                <p>发件人：{{ mailStore.selectedMessage.from?.email || '未知' }}</p>
                <p>收件账号：{{ mailStore.selectedMessage.accountEmail }}</p>
                <p>时间：{{ formatDateTime(mailStore.selectedMessage.receivedAt) }}</p>
              </div>
              <el-tag :type="mailStore.selectedMessage.isRead ? 'info' : 'success'">
                {{ mailStore.selectedMessage.isRead ? '已读' : '未读' }}
              </el-tag>
            </div>
            <el-skeleton v-if="mailStore.bodyLoading" :rows="6" animated />
            <iframe
              v-else-if="selectedHtml"
              class="mail-body-frame"
              sandbox="allow-popups allow-popups-to-escape-sandbox"
              :srcdoc="selectedHtml"
              title="邮件正文"
            />
            <pre v-else class="mail-body plain">{{ mailStore.selectedBody?.content || mailStore.selectedMessage.preview }}</pre>
          </template>
        </section>
      </section>

      <el-dialog v-model="groupDialogVisible" title="批量设置分组" width="420px">
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
          <el-button type="primary" :disabled="!targetGroupName.trim()" @click="submitMoveGroup">
            确定
          </el-button>
        </template>
      </el-dialog>
    </main>
  </section>
</template>
