<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import {
  CopyDocument,
  Delete,
  EditPen,
  Files,
  FolderOpened,
  Message,
  Refresh,
  UploadFilled,
  View,
} from '@element-plus/icons-vue'
import ICloudImportDialog from '@/components/ICloudImportDialog.vue'
import MailboxTopbar from '@/components/MailboxTopbar.vue'
import { useICloudAccountStore, ICLOUD_DEFAULT_GROUP } from '@/stores/iCloudAccount'
import {
  getLatestICloudMail,
  type ICloudAccount,
  type ICloudGroup,
  type ICloudLatestMailResponse,
} from '@/services/iCloudAccountApi'
import { formatDateTime } from '@/utils/dateTime'
import { plainMailBlocks } from '@/utils/mailBody'

const iCloudReaderEnhancements = [
  '<meta charset="utf-8" />',
  '<meta name="viewport" content="width=device-width, initial-scale=1" />',
  '<base target="_blank" />',
  '<style>',
  'html { min-height: 100%; background: #f4f6f9; color-scheme: light; }',
  '*, *::before, *::after { box-sizing: border-box; }',
  'body { max-width: 760px !important; min-height: 100vh; margin: 0 auto !important; padding: 28px !important; background: #ffffff; color: #1f2937; overflow-wrap: anywhere; }',
  'img { max-width: 100% !important; height: auto !important; }',
  'img[src*="cdn.mcauto-images-production.sendgrid.net"], img[src*="/wf/open?"] { display: none !important; }',
  'table { max-width: 100% !important; }',
  'a { color: #2563eb; }',
  '@media (max-width: 680px) { body { padding: 18px !important; } }',
  '</style>',
].join('')

function buildICloudReaderHtml(content: string): string {
  if (/<\/head>/i.test(content)) {
    return content.replace(/<\/head>/i, iCloudReaderEnhancements + '</head>')
  }
  if (/<head(?:\s[^>]*)?>/i.test(content)) {
    return content.replace(/<head([^>]*)>/i, '<head$1>' + iCloudReaderEnhancements)
  }
  if (/<html(?:\s[^>]*)?>/i.test(content)) {
    return content.replace(/<html([^>]*)>/i, '<html$1><head>' + iCloudReaderEnhancements + '</head>')
  }
  return '<!doctype html><html><head>' + iCloudReaderEnhancements + '</head><body>' + content + '</body></html>'
}
const store = useICloudAccountStore()
const keyword = ref('')
const page = ref(1)
const pageSize = ref(20)
const selectedRows = ref<ICloudAccount[]>([])
const importVisible = ref(false)
const moveDialogVisible = ref(false)
const targetGroup = ref('')
const moving = ref(false)
const deleting = ref(false)
const copying = ref(false)
const editingRemarkEmail = ref('')
const draggingGroupId = ref<number>()
const deletingGroupId = ref<number>()
const renamingGroupId = ref<number>()
const copiedValues = ref<Set<string>>(new Set())
const latestMailVisible = ref(false)
const latestMailLoading = ref(false)
const latestMailAccount = ref('')
const latestMail = ref<ICloudLatestMailResponse>()
const latestMailHtml = computed(() => {
  const html = latestMail.value?.email?.html?.trim() ?? ''
  return html ? buildICloudReaderHtml(html) : ''
})
const latestMailBlocks = computed(() => plainMailBlocks(latestMail.value?.email?.text ?? ''))

const filteredAccounts = computed(() => {
  const normalizedKeyword = keyword.value.trim().toLowerCase()
  return store.accounts.filter((account) => {
    if (store.selectedGroup && account.group !== store.selectedGroup) {
      return false
    }
    if (!normalizedKeyword) {
      return true
    }
    return [account.email, account.key, account.group, account.remark]
      .some((value) => value.toLowerCase().includes(normalizedKeyword))
  })
})

const pagedAccounts = computed(() => {
  const start = (page.value - 1) * pageSize.value
  return filteredAccounts.value.slice(start, start + pageSize.value)
})

const groupCounts = computed(() => {
  const counts = new Map<string, number>()
  for (const account of store.accounts) {
    counts.set(account.group, (counts.get(account.group) ?? 0) + 1)
  }
  return counts
})

watch([keyword, () => store.selectedGroup], () => {
  page.value = 1
  selectedRows.value = []
})

onMounted(async () => {
  try {
    await store.load()
  } catch (error) {
    showError(error, '加载 iCloud 账号失败')
  }
})

function setGroup(group: string) {
  store.setSelectedGroup(group)
}

function handleSelection(rows: ICloudAccount[]) {
  selectedRows.value = rows
}

async function copyAccounts(format: 'email' | 'key' | 'emailKey') {
  if (copying.value) {
    return
  }
  const targets = selectedRows.value.length > 0 ? selectedRows.value : filteredAccounts.value
  if (targets.length === 0) {
    ElMessage.warning('没有可复制的 iCloud 账号')
    return
  }

  copying.value = true
  try {
    const text = targets.map((account) => {
      if (format === 'email') return account.email
      if (format === 'key') return account.key
      return `${account.email}----${account.key}`
    }).join('\n')
    await navigator.clipboard.writeText(text)
    ElMessage.success(`已复制 ${targets.length} 个账号`)
  } catch {
    ElMessage.error('复制账号失败')
  } finally {
    copying.value = false
  }
}

async function copyValue(value: string, label: string) {
  try {
    await navigator.clipboard.writeText(value)
    copiedValues.value = new Set([...copiedValues.value, value])
    window.setTimeout(() => {
      const next = new Set(copiedValues.value)
      next.delete(value)
      copiedValues.value = next
    }, 1200)
    ElMessage.success(`已复制${label}`)
  } catch {
    ElMessage.error(`复制${label}失败`)
  }
}

async function openLatestMail(account: ICloudAccount) {
  latestMailAccount.value = account.email
  latestMail.value = undefined
  latestMailVisible.value = true
  await refreshLatestMail()
}

async function refreshLatestMail() {
  if (!latestMailAccount.value || latestMailLoading.value) {
    return
  }
  latestMailLoading.value = true
  try {
    latestMail.value = await getLatestICloudMail(latestMailAccount.value)
  } catch (error) {
    latestMail.value = undefined
    showError(error, '获取最新邮件失败')
  } finally {
    latestMailLoading.value = false
  }
}

async function copyLatestValue(value: string, label: string) {
  if (!value) {
    return
  }
  await copyValue(value, label)
}
function openMoveDialog() {
  if (selectedRows.value.length === 0) {
    ElMessage.warning('请先勾选要移动的账号')
    return
  }
  targetGroup.value = store.selectedGroup || ICLOUD_DEFAULT_GROUP
  moveDialogVisible.value = true
}

async function submitMove() {
  if (!targetGroup.value.trim() || selectedRows.value.length === 0) {
    return
  }
  moving.value = true
  try {
    await store.moveToGroup(selectedRows.value.map((account) => account.email), targetGroup.value)
    selectedRows.value = []
    moveDialogVisible.value = false
    ElMessage.success('分组已更新')
  } catch (error) {
    showError(error, '移动分组失败')
  } finally {
    moving.value = false
  }
}

async function editRemark(account: ICloudAccount) {
  const result = await ElMessageBox.prompt('备注最多 500 个字符，留空表示清除备注。', `编辑备注 · ${account.email}`, {
    confirmButtonText: '保存',
    cancelButtonText: '取消',
    inputValue: account.remark,
    inputType: 'textarea',
    inputValidator: (value) => Array.from(value.trim()).length <= 500 || '备注最多 500 个字符',
  })
  editingRemarkEmail.value = account.email
  try {
    await store.updateRemark(account.email, result.value)
    ElMessage.success('备注已保存')
  } catch (error) {
    showError(error, '保存备注失败')
  } finally {
    editingRemarkEmail.value = ''
  }
}

async function deleteOne(account: ICloudAccount) {
  await ElMessageBox.confirm(`确定删除 ${account.email}？`, '删除 iCloud 账号', {
    confirmButtonText: '删除',
    cancelButtonText: '取消',
    type: 'warning',
  })
  try {
    await store.deleteAccount(account.email)
    selectedRows.value = selectedRows.value.filter((row) => row.email !== account.email)
    ElMessage.success('账号已删除')
  } catch (error) {
    showError(error, '删除账号失败')
  }
}

async function deleteSelected() {
  if (selectedRows.value.length === 0) {
    ElMessage.warning('请先勾选要删除的账号')
    return
  }
  const emails = selectedRows.value.map((account) => account.email)
  await ElMessageBox.confirm(`确定删除已选中的 ${emails.length} 个 iCloud 账号？`, '批量删除', {
    confirmButtonText: '删除',
    cancelButtonText: '取消',
    type: 'warning',
  })
  deleting.value = true
  try {
    await Promise.all(emails.map((email) => store.deleteAccount(email)))
    selectedRows.value = []
    ElMessage.success(`已删除 ${emails.length} 个账号`)
  } catch (error) {
    await store.load()
    selectedRows.value = []
    showError(error, '批量删除失败')
  } finally {
    deleting.value = false
  }
}

async function renameGroup(group: ICloudGroup) {
  const result = await ElMessageBox.prompt('请输入新的分组名称', '重命名 iCloud 分组', {
    confirmButtonText: '保存',
    cancelButtonText: '取消',
    inputValue: group.name,
    inputValidator: (value) => Boolean(value.trim()) || '分组名称不能为空',
  })
  if (result.value.trim() === group.name) {
    return
  }
  renamingGroupId.value = group.id
  try {
    await store.renameGroup(group.id, group.name, result.value)
    ElMessage.success('分组已重命名')
  } catch (error) {
    showError(error, '重命名分组失败')
  } finally {
    renamingGroupId.value = undefined
  }
}

async function deleteGroup(group: ICloudGroup) {
  await ElMessageBox.confirm(`确定删除空分组“${group.name}”？`, '删除 iCloud 分组', {
    confirmButtonText: '删除',
    cancelButtonText: '取消',
    type: 'warning',
  })
  deletingGroupId.value = group.id
  try {
    await store.deleteGroup(group.id, group.name)
    ElMessage.success('分组已删除')
  } catch (error) {
    showError(error, '删除分组失败')
  } finally {
    deletingGroupId.value = undefined
  }
}

function handleDragStart(group: ICloudGroup, event: DragEvent) {
  draggingGroupId.value = group.id
  if (event.dataTransfer) {
    event.dataTransfer.effectAllowed = 'move'
    event.dataTransfer.setData('text/plain', String(group.id))
  }
}

function handleDragOver(event: DragEvent) {
  event.preventDefault()
  if (event.dataTransfer) {
    event.dataTransfer.dropEffect = 'move'
  }
}

async function handleDrop(target: ICloudGroup, event: DragEvent) {
  event.preventDefault()
  const sourceID = draggingGroupId.value ?? Number(event.dataTransfer?.getData('text/plain'))
  draggingGroupId.value = undefined
  if (!sourceID || sourceID === target.id) {
    return
  }
  const groups = [...store.groups]
  const sourceIndex = groups.findIndex((group) => group.id === sourceID)
  const targetIndex = groups.findIndex((group) => group.id === target.id)
  if (sourceIndex < 0 || targetIndex < 0) {
    return
  }
  const [source] = groups.splice(sourceIndex, 1)
  if (!source) {
    return
  }
  groups.splice(targetIndex, 0, source)
  try {
    await store.reorderGroups(groups.map((group) => group.id))
  } catch (error) {
    await store.load()
    showError(error, '分组排序失败')
  }
}

function showError(error: unknown, fallback: string) {
  ElMessage.error(error instanceof Error ? error.message : fallback)
}

function groupCount(group: ICloudGroup): number {
  return groupCounts.value.get(group.name) ?? 0
}

function canDeleteGroup(group: ICloudGroup): boolean {
  return group.name !== ICLOUD_DEFAULT_GROUP && groupCount(group) === 0
}
</script>

<template>
  <section class="faka-shell icloud-management-shell">
    <aside class="faka-sidebar">
      <div class="faka-brand">
        <el-icon><Message /></el-icon>
        <span>MailBox</span>
      </div>

      <nav class="faka-nav">
        <section class="sidebar-panel group-panel icloud-group-panel">
        <div class="sidebar-panel-head">
          <span>
            <el-icon><FolderOpened /></el-icon>
            iCloud 分组
          </span>
          <strong>{{ store.groups.length }}</strong>
        </div>
        <div class="sidebar-panel-body group-panel-body">
          <button
            class="faka-nav-item sidebar-list-row pinned-row"
            :class="{ active: !store.selectedGroup }"
            type="button"
            @click="setGroup('')"
          >
            <el-icon><FolderOpened /></el-icon>
            <span>全部账号</span>
            <small>{{ store.accounts.length }}</small>
          </button>
          <button
            v-for="group in store.groups"
            :key="group.id"
            class="faka-nav-item sidebar-list-row group-nav-item"
            :class="{ active: store.selectedGroup === group.name, dragging: draggingGroupId === group.id }"
            type="button"
            draggable="true"
            @click="setGroup(group.name)"
            @dragstart="handleDragStart(group, $event)"
            @dragover="handleDragOver"
            @drop="handleDrop(group, $event)"
            @dragend="draggingGroupId = undefined"
          >
            <span class="drag-handle" aria-hidden="true">⋮⋮</span>
            <el-icon><FolderOpened /></el-icon>
            <span>{{ group.name }}</span>
            <div class="group-actions">
              <small>{{ groupCount(group) }}</small>
              <el-button
                v-if="group.name !== ICLOUD_DEFAULT_GROUP"
                class="group-action-button"
                link
                :icon="EditPen"
                :loading="renamingGroupId === group.id"
                @click.stop="renameGroup(group)"
              />
              <el-button
                v-if="canDeleteGroup(group)"
                class="group-action-button"
                link
                :icon="Delete"
                :loading="deletingGroupId === group.id"
                @click.stop="deleteGroup(group)"
              />
            </div>
          </button>
        </div>
        </section>
      </nav>

      <div class="faka-total-card">
        <el-icon><Files /></el-icon>
        <span>iCloud 账号</span>
        <strong>{{ store.accounts.length }}</strong>
      </div>
    </aside>

    <main class="faka-main">
      <MailboxTopbar
        :search-value="keyword"
        workspace-mode="accounts"
        placeholder="搜索 iCloud 邮箱、密钥、分组或备注..."
        @search-input="keyword = $event"
      />

      <section class="faka-card">
        <div class="faka-action-row">
          <el-button type="primary" :icon="UploadFilled" @click="importVisible = true">导入账号</el-button>
          <el-dropdown trigger="click">
            <el-button :icon="CopyDocument" :loading="copying">复制</el-button>
            <template #dropdown>
              <el-dropdown-menu>
                <el-dropdown-item @click="copyAccounts('email')">仅复制邮箱</el-dropdown-item>
                <el-dropdown-item @click="copyAccounts('key')">仅复制密钥</el-dropdown-item>
                <el-dropdown-item divided @click="copyAccounts('emailKey')">邮箱----密钥</el-dropdown-item>
              </el-dropdown-menu>
            </template>
          </el-dropdown>
          <el-button :icon="FolderOpened" :disabled="selectedRows.length === 0 || moving" @click="openMoveDialog">
            移动分组
          </el-button>
          <el-button
            type="danger"
            :icon="Delete"
            :loading="deleting"
            :disabled="selectedRows.length === 0 || deleting"
            @click="deleteSelected"
          >
            删除
          </el-button>
        </div>

      <div class="account-selection-hint" :class="{ active: selectedRows.length > 0 }">
        <strong>已选 {{ selectedRows.length }} 个账号</strong>
        <span>复制优先作用于已选账号；未勾选时复制当前筛选范围</span>
      </div>

      <el-table
        v-loading="store.loading || moving || deleting"
        :data="pagedAccounts"
        row-key="email"
        class="faka-account-table"
        height="calc(100vh - 232px)"
        @selection-change="handleSelection"
      >
        <el-table-column type="selection" width="52" align="center" header-align="center" />
        <el-table-column label="#" width="64" align="center" header-align="center">
          <template #default="{ $index }">
            <span class="row-number">{{ (page - 1) * pageSize + $index + 1 }}</span>
          </template>
        </el-table-column>
        <el-table-column label="邮箱" min-width="280" show-overflow-tooltip align="center" header-align="center">
          <template #default="{ row }">
            <div class="copy-cell" :class="{ copied: copiedValues.has(row.email) }">
              <span>{{ row.email }}</span>
              <el-tooltip content="复制邮箱" placement="top">
                <el-button link :icon="CopyDocument" @click.stop="copyValue(row.email, '邮箱')" />
              </el-tooltip>
            </div>
          </template>
        </el-table-column>
        <el-table-column label="密钥" min-width="190" show-overflow-tooltip align="center" header-align="center">
          <template #default="{ row }">
            <div class="copy-cell" :class="{ copied: copiedValues.has(row.key) }">
              <span>{{ row.key }}</span>
              <el-tooltip content="复制密钥" placement="top">
                <el-button link :icon="CopyDocument" @click.stop="copyValue(row.key, '密钥')" />
              </el-tooltip>
            </div>
          </template>
        </el-table-column>
        <el-table-column prop="group" label="分组" width="150" align="center" header-align="center" />
        <el-table-column label="备注" min-width="220" show-overflow-tooltip align="center" header-align="center">
          <template #default="{ row }">
            <div class="remark-cell">
              <span :class="{ muted: !row.remark }">{{ row.remark || '无备注' }}</span>
              <el-tooltip content="编辑备注" placement="top">
                <el-button
                  link
                  :icon="EditPen"
                  :loading="editingRemarkEmail === row.email"
                  @click.stop="editRemark(row)"
                />
              </el-tooltip>
            </div>
          </template>
        </el-table-column>
        <el-table-column label="导入" width="170" align="center" header-align="center">
          <template #default="{ row }">{{ formatDateTime(row.createdAt) }}</template>
        </el-table-column>
        <el-table-column label="操作" width="180" fixed="right" align="center" header-align="center">
          <template #default="{ row }">
            <el-space :size="8" class="row-actions">
              <el-button size="small" type="primary" :icon="View" @click.stop="openLatestMail(row)">查看</el-button>
              <el-button size="small" type="danger" plain :icon="Delete" @click.stop="deleteOne(row)">删除</el-button>
            </el-space>
          </template>
        </el-table-column>
      </el-table>

      <div class="faka-pagination">
        <span>Total {{ filteredAccounts.length }}</span>
        <el-pagination
          v-model:current-page="page"
          v-model:page-size="pageSize"
          size="small"
          layout="sizes, prev, pager, next"
          :total="filteredAccounts.length"
          :page-sizes="[20, 50, 100]"
          @current-change="selectedRows = []"
          @size-change="page = 1; selectedRows = []"
        />
      </div>
      </section>
    </main>

    <ICloudImportDialog v-model="importVisible" />

    <el-dialog v-model="latestMailVisible" width="840px" class="icloud-latest-dialog">
      <template #header>
        <div class="icloud-dialog-heading">
          <span>iCloud 邮件</span>
          <h2>{{ latestMail?.email?.subject || '邮件详情' }}</h2>
        </div>
      </template>

      <div v-loading="latestMailLoading" class="icloud-latest-content">
        <template v-if="latestMail?.email">
          <div class="icloud-message-meta">
            <div class="reader-sender-avatar" aria-hidden="true">
              {{ latestMail.email.from?.charAt(0).toUpperCase() || '?' }}
            </div>
            <div class="icloud-message-parties">
              <strong>{{ latestMail.email.from || '未知发件人' }}</strong>
              <span>发送至 {{ latestMail.email.to || latestMailAccount }}</span>
            </div>
            <time>{{ formatDateTime(latestMail.email.received_at || latestMail.email.created_at) }}</time>
          </div>

          <div v-if="latestMail.email.verification_code" class="icloud-verification-code">
            <span>验证码</span>
            <strong>{{ latestMail.email.verification_code }}</strong>
            <el-button
              size="small"
              :icon="CopyDocument"
              @click="copyLatestValue(latestMail.email.verification_code, '验证码')"
            >
              复制
            </el-button>
          </div>

          <section class="icloud-reader-panel" aria-label="iCloud 邮件正文">
            <iframe
              v-if="latestMailHtml"
              class="icloud-mail-frame"
              :srcdoc="latestMailHtml"
              sandbox="allow-popups allow-popups-to-escape-sandbox"
              title="iCloud 邮件正文"
            />
            <div v-else class="mail-body plain plain-mail-paragraphs icloud-mail-body">
              <template v-if="latestMailBlocks.length">
                <p
                  v-for="(block, index) in latestMailBlocks"
                  :key="index"
                  :class="{ 'plain-mail-heading': block.kind === 'heading' }"
                >
                  {{ block.text }}
                </p>
              </template>
              <p v-else class="plain-mail-empty">暂无正文内容</p>
            </div>
          </section>

          <div v-if="latestMail.email.invite_link && !latestMailHtml" class="icloud-invite-link">
            <span>邀请链接</span>
            <code>{{ latestMail.email.invite_link }}</code>
            <el-button
              size="small"
              :icon="CopyDocument"
              @click="copyLatestValue(latestMail.email.invite_link, '邀请链接')"
            >
              复制
            </el-button>
          </div>
        </template>
        <el-empty v-else-if="!latestMailLoading" description="暂无最新邮件" :image-size="72" />
      </div>
      <template #footer>
        <el-button @click="latestMailVisible = false">关闭</el-button>
        <el-button
          v-if="latestMail?.email?.text"
          :icon="CopyDocument"
          @click="copyLatestValue(latestMail.email.text, '正文')"
        >
          复制正文
        </el-button>
        <el-button type="primary" :icon="Refresh" :loading="latestMailLoading" @click="refreshLatestMail">
          刷新最新邮件
        </el-button>
      </template>
    </el-dialog>
    <el-dialog v-model="moveDialogVisible" title="移动 iCloud 分组" width="420px">
      <el-form label-position="top">
        <el-form-item label="选择或输入目标分组">
          <el-select
            v-model="targetGroup"
            filterable
            allow-create
            default-first-option
            placeholder="目标分组"
            style="width: 100%"
          >
            <el-option v-for="group in store.groups" :key="group.id" :label="group.name" :value="group.name" />
          </el-select>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="moveDialogVisible = false">取消</el-button>
        <el-button type="primary" :loading="moving" :disabled="!targetGroup.trim()" @click="submitMove">确定</el-button>
      </template>
    </el-dialog>
  </section>
</template>