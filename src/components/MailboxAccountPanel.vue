<script setup lang="ts">
import {
  CopyDocument,
  Delete,
  Download,
  EditPen,
  FolderOpened,
  Refresh,
  UploadFilled,
} from '@element-plus/icons-vue'
import type { AccountStatus, MailAccount } from '@/types'
import { formatDateTime } from '@/utils/dateTime'

const props = defineProps<{
  clearingData?: boolean
  loading: boolean
  deleting: boolean
  movingGroup: boolean
  copying: boolean
  exportingAccounts: boolean
  selectedCount: number
  selectionScopeText: string
  batchSyncRunning: boolean
  batchSyncResultsCount: number
  batchSyncDone: number
  batchSyncTotal: number
  batchSyncSuccess: number
  batchSyncFailed: number
  batchProgressPercent: number
  batchFailedResults: Array<{ accountEmail: string; folder: string; error?: string }>
  pagedAccounts: MailAccount[]
  visibleTotal: number
  accountPage: number
  accountPageSize: number
  copiedValues: ReadonlySet<string>
  messageCountByEmail: ReadonlyMap<string, number>
  accountByEmail: ReadonlyMap<string, MailAccount>
  parentRowIndexMap: ReadonlyMap<string, number>
  editingRemarkEmail: string
  viewingEmail: string
  splittingEmail: string
  statusType: Record<AccountStatus, 'info' | 'primary' | 'success' | 'warning' | 'danger'>
  statusText: Record<AccountStatus, string>
}>()

const emit = defineEmits<{
  importAccounts: []
  syncAccounts: []
  copyAccounts: [format: 'email' | 'password' | 'emailPassword']
  openMoveGroupDialog: []
  batchDeleteSelected: []
  copyExportText: []
  downloadExportText: []
  retryFailedAccounts: []
  selectionChange: [rows: MailAccount[]]
  copyText: [value: string, label: string]
  editAccountRemark: [account: MailAccount]
  viewAccountInbox: [account: MailAccount]
  splitHotmail: [account: MailAccount]
  updateAccountPage: [page: number]
  updateAccountPageSize: [size: number]
}>()

defineSlots<{
  default?: never
}>()

const tableTreeProps = { children: 'tableChildren' }

function rowClassName({ row }: { row: MailAccount }) {
  return isSplitAccount(row) ? 'split-account-row' : 'parent-account-row'
}

function resolveMailboxAccount(account: MailAccount): MailAccount {
  return account.parentEmail ? props.accountByEmail.get(account.parentEmail.toLowerCase()) ?? account : account
}


function splitCount(account: MailAccount): number {
  return account.children?.length ?? 0
}

function splitLabel(account: MailAccount): string {
  const parent = resolveMailboxAccount(account)
  const splitIndex = account.splitIndex
    ?? ((parent.children?.findIndex((child) => child.email === account.email) ?? -1) + 1)
  return String(splitIndex)
}

function accountRowLabel(account: MailAccount): string {
  if (account.parentEmail) {
    return ''
  }
  const index = props.parentRowIndexMap.get(account.email)
  return index ? String(index) : ''
}

function canSplitHotmail(account: MailAccount): boolean {
  return !account.parentEmail && /^[^+@\s]+@hotmail\.com$/i.test(account.email) && splitCount(account) === 0
}

function isSplitAccount(account: MailAccount): boolean {
  return Boolean(account.parentEmail)
}

function sortByDateTime(left: MailAccount, right: MailAccount, key: 'createdAt' | 'updatedAt' | 'lastSyncAt'): number {
  return (left[key] ?? '').localeCompare(right[key] ?? '')
}

</script>

<template>
  <section class="faka-card">
    <div class="faka-action-row">
      <el-button type="primary" :icon="UploadFilled" @click="emit('importAccounts')">导入账号</el-button>
      <el-button
        :icon="Refresh"
        :loading="batchSyncRunning"
        :disabled="batchSyncRunning"
        @click="emit('syncAccounts')"
      >
        收信
      </el-button>
      <el-dropdown trigger="click">
        <el-button :icon="CopyDocument" :loading="copying" :disabled="copying">复制</el-button>
        <template #dropdown>
          <el-dropdown-menu>
            <el-dropdown-item @click="emit('copyAccounts', 'email')">仅复制邮箱</el-dropdown-item>
            <el-dropdown-item divided @click="emit('copyAccounts', 'emailPassword')">邮箱----密码</el-dropdown-item>
          </el-dropdown-menu>
        </template>
      </el-dropdown>
      <el-button :icon="FolderOpened" :disabled="movingGroup || deleting" @click="emit('openMoveGroupDialog')">分组</el-button>
      <el-button type="danger" :icon="Delete" :loading="deleting" :disabled="deleting" @click="emit('batchDeleteSelected')">删除</el-button>
      <el-dropdown trigger="click">
        <el-button :icon="Download" :loading="exportingAccounts" :disabled="exportingAccounts">导出</el-button>
        <template #dropdown>
          <el-dropdown-menu>
            <el-dropdown-item @click="emit('copyExportText')">复制导出文本</el-dropdown-item>
            <el-dropdown-item divided @click="emit('downloadExportText')">下载 TXT 文件</el-dropdown-item>
          </el-dropdown-menu>
        </template>
      </el-dropdown>
    </div>

    <div class="account-selection-hint" :class="{ active: selectedCount > 0 }">
      <strong>已选 {{ selectedCount }} 个账号</strong>
      <span>{{ selectionScopeText }}</span>
    </div>

    <Transition name="panel-slide">
      <div
        v-if="batchSyncRunning || batchSyncResultsCount > 0"
        class="batch-sync-panel"
      >
        <div class="batch-sync-head">
          <span>
            收信：
            {{ batchSyncDone }} / {{ batchSyncTotal }}
          </span>
          <strong>
            成功 {{ batchSyncSuccess }}，
            失败 {{ batchSyncFailed }}
          </strong>
        </div>
        <el-progress
          :percentage="batchProgressPercent"
          :status="batchSyncFailed > 0 && !batchSyncRunning ? 'exception' : undefined"
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
              :loading="batchSyncRunning"
              @click="emit('retryFailedAccounts')"
            >
              重试失败账号
            </el-button>
          </div>
        </Transition>
      </div>
    </Transition>

    <el-table
      v-loading="loading || deleting || movingGroup || clearingData"
      :data="pagedAccounts"
      row-key="email"
      class="faka-account-table"
      height="calc(100vh - 232px)"
      :default-expand-all="false"
      :tree-props="tableTreeProps"
      :row-class-name="rowClassName"
      @selection-change="emit('selectionChange', $event)"
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
              <el-button link :icon="CopyDocument" @click.stop="emit('copyText', row.email, '邮箱')" />
            </el-tooltip>
          </div>
        </template>
      </el-table-column>
      <el-table-column label="密码" min-width="170" show-overflow-tooltip align="center" header-align="center">
        <template #default="{ row }">
          <div class="copy-cell" :class="{ copied: copiedValues.has(row.password) }">
            <span>{{ row.password }}</span>
            <el-tooltip content="复制" placement="top">
              <el-button link :icon="CopyDocument" @click.stop="emit('copyText', row.password, '密码')" />
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
                @click.stop="emit('editAccountRemark', row)"
              />
            </el-tooltip>
          </div>
        </template>
      </el-table-column>
      <el-table-column
        label="导入"
        width="170"
        sortable
        :sort-method="sortByDateTime"
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
                :loading="viewingEmail === resolveMailboxAccount(row).email"
                :disabled="viewingEmail === resolveMailboxAccount(row).email"
                @click="emit('viewAccountInbox', row)"
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
                  @click="emit('splitHotmail', row)"
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
      <span>Total {{ visibleTotal }}</span>
      <el-pagination
        :current-page="accountPage"
        :page-size="accountPageSize"
        size="small"
        layout="sizes, prev, pager, next"
        :total="visibleTotal"
        :page-sizes="[20, 50, 100]"
        @current-change="emit('updateAccountPage', $event)"
        @size-change="emit('updateAccountPageSize', $event)"
      />
    </div>
  </section>
</template>
