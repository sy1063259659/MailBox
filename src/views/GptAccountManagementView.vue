<script setup lang="ts">
import { computed, defineAsyncComponent, onBeforeUnmount, ref, watch } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import {
  Link,
  List,
  Refresh,
  Search,
  User,
} from '@element-plus/icons-vue'
import { useLocalUiState } from '@/composables/useLocalUiState'
import { useAccountStore } from '@/stores/account'
import { useGptAccountStore } from '@/stores/gptAccount'
import type { GptAccount, GptAccountStatus, MailAccount } from '@/types'
import {
  expiryBucketType,
  formatDateTime,
  formatQuota,
  formatShortDate,
  gptStatusText,
  gptStatusType,
  planText,
  quotaPercent,
  quotaStatus,
} from '@/utils/gptDisplay'

interface GptAccountRow {
  account: GptAccount
  mailbox?: MailAccount
}

type GptViewMode = 'cards' | 'table'
type GptStatusFilter = 'all' | GptAccountStatus

const GptAccountDialog = defineAsyncComponent(() => import('@/components/GptAccountDialog.vue'))

const props = defineProps<{
  focusEmail?: string
}>()

const accountStore = useAccountStore()
const gptAccountStore = useGptAccountStore()
const gptViewMode = useLocalUiState<GptViewMode>('mailbox.ui.gptViewMode', 'cards', {
  validate: isGptViewMode,
})
const gptKeyword = useLocalUiState('mailbox.ui.gptKeyword', '', {
  validate: isString,
})
const gptStatusFilter = useLocalUiState<GptStatusFilter>('mailbox.ui.gptStatusFilter', 'all', {
  validate: isGptStatusFilter,
})
const gptPlanFilter = useLocalUiState('mailbox.ui.gptPlanFilter', 'all', {
  validate: isString,
})
const gptDialogVisible = ref(false)
const gptDialogAccount = ref<MailAccount>()
const focusedEmail = ref('')
const gptKeywordDraft = ref(gptKeyword.value)
let gptKeywordDebounceTimer: number | undefined

function isGptViewMode(value: unknown): value is GptViewMode {
  return value === 'cards' || value === 'table'
}

function isGptStatusFilter(value: unknown): value is GptStatusFilter {
  return value === 'all'
    || value === 'active'
    || value === 'expired'
    || value === 'quota_limited'
    || value === 'reauth_required'
    || value === 'banned_or_disabled'
    || value === 'error'
    || value === 'unknown'
}

function isString(value: unknown): value is string {
  return typeof value === 'string'
}

const mailboxByEmail = computed(() => {
  const map = new Map<string, MailAccount>()
  for (const account of accountStore.accounts) {
    map.set(account.email.toLowerCase(), account)
  }
  return map
})

const rows = computed<GptAccountRow[]>(() =>
  gptAccountStore.accounts.map((account) => ({
    account,
    mailbox: mailboxByEmail.value.get(account.mailAccountEmail.toLowerCase()),
  })),
)

const planOptions = computed(() => {
  const plans = new Set<string>()
  for (const row of rows.value) {
    plans.add(planText(row.account))
  }
  return Array.from(plans).sort((left, right) => left.localeCompare(right))
})

const filteredRows = computed(() => {
  const keyword = gptKeyword.value.trim().toLowerCase()
  return rows.value.filter((row) => {
    if (gptStatusFilter.value !== 'all' && row.account.status !== gptStatusFilter.value) {
      return false
    }
    if (gptPlanFilter.value !== 'all' && planText(row.account) !== gptPlanFilter.value) {
      return false
    }
    if (!keyword) {
      return true
    }
    return [
      row.account.mailAccountEmail,
      row.account.gptEmail,
      row.account.accountName ?? '',
      row.account.accountStructure ?? '',
      planText(row.account),
      gptStatusText[row.account.status],
      row.mailbox?.group ?? '',
      row.mailbox?.remark ?? '',
    ].some((value) => value.toLowerCase().includes(keyword))
  })
})

const focusExists = computed(() =>
  focusedEmail.value
    ? rows.value.some((row) => row.account.mailAccountEmail.toLowerCase() === focusedEmail.value)
    : true,
)

function statusReason(account: GptAccount): string {
  return account.reauthReason || account.quotaErrorMessage || account.statusReason || ''
}

function statusTagType(account: GptAccount): 'info' | 'success' | 'warning' | 'danger' {
  return gptStatusType[account.status]
}

function statusLabel(account: GptAccount): string {
  return gptStatusText[account.status]
}

function expiryTagType(account: GptAccount): 'info' | 'success' | 'warning' | 'danger' {
  return expiryBucketType[account.subscriptionExpiryBucket]
}

function isGptBusy(account: GptAccount): boolean {
  const email = account.mailAccountEmail.toLowerCase()
  return gptAccountStore.bindingEmails.includes(email)
    || gptAccountStore.refreshingEmails.includes(email)
    || gptAccountStore.unlinkingEmails.includes(email)
}

function openGptDialog(row: GptAccountRow) {
  const mailbox = row.mailbox ?? fallbackMailbox(row.account)
  gptDialogAccount.value = mailbox
  gptDialogVisible.value = true
}

function handleGptBound(payload: { mailAccountEmail: string }) {
  gptDialogAccount.value = undefined
  focusAccount(payload.mailAccountEmail)
}

function isFocusedRow(row: GptAccountRow): boolean {
  return Boolean(focusedEmail.value)
    && row.account.mailAccountEmail.toLowerCase() === focusedEmail.value
}

function fallbackMailbox(account: GptAccount): MailAccount {
  return {
    email: account.mailAccountEmail,
    password: '',
    clientId: '',
    refreshToken: '',
    group: '',
    remark: '',
    displayName: account.mailAccountEmail,
    status: 'idle',
    createdAt: '',
    updatedAt: '',
  }
}

async function refreshGptAccount(account: GptAccount) {
  try {
    await gptAccountStore.refreshOne(account.mailAccountEmail)
    ElMessage.success('GPT/Codex 状态已刷新')
  } catch (error) {
    ElMessage.error(error instanceof Error ? error.message : '刷新 GPT/Codex 失败')
  }
}

async function refreshAllGptAccounts() {
  try {
    await gptAccountStore.refreshAll()
    ElMessage.success('GPT/Codex 状态已全部刷新')
  } catch (error) {
    ElMessage.error(error instanceof Error ? error.message : '刷新 GPT/Codex 失败')
  }
}

async function unlinkGptAccount(account: GptAccount) {
  await ElMessageBox.confirm(`确定解除 ${account.mailAccountEmail} 绑定的 GPT/Codex 账号吗？`, '解除绑定', {
    confirmButtonText: '解除',
    cancelButtonText: '取消',
    type: 'warning',
  })
  try {
    await gptAccountStore.unlink(account.mailAccountEmail)
    ElMessage.success('GPT/Codex 账号已解除绑定')
  } catch (error) {
    ElMessage.error(error instanceof Error ? error.message : '解除 GPT/Codex 绑定失败')
  }
}

function focusAccount(email: string) {
  const normalizedEmail = email.trim().toLowerCase()
  if (!normalizedEmail) {
    return
  }
  focusedEmail.value = normalizedEmail
  gptKeyword.value = normalizedEmail
  gptKeywordDraft.value = normalizedEmail
  gptStatusFilter.value = 'all'
  gptPlanFilter.value = 'all'
}

function handleGptKeywordInput(value: string) {
  gptKeywordDraft.value = value
  if (gptKeywordDebounceTimer) {
    window.clearTimeout(gptKeywordDebounceTimer)
    gptKeywordDebounceTimer = undefined
  }
  const applyKeyword = () => {
    gptKeyword.value = value
  }
  if (!value.trim()) {
    applyKeyword()
    return
  }
  gptKeywordDebounceTimer = window.setTimeout(applyKeyword, 250)
}

watch(
  () => props.focusEmail,
  (email) => {
    if (email) {
      focusAccount(email)
    }
  },
  { immediate: true },
)

watch(gptKeyword, (value) => {
  gptKeywordDraft.value = value
})

onBeforeUnmount(() => {
  if (gptKeywordDebounceTimer) {
    window.clearTimeout(gptKeywordDebounceTimer)
  }
})
</script>

<template>
  <section class="gpt-management-view">
    <div class="gpt-management-toolbar">
      <el-input
        :model-value="gptKeywordDraft"
        class="gpt-management-search"
        :prefix-icon="Search"
        clearable
        placeholder="搜索邮箱、套餐、分组、备注..."
        @update:model-value="handleGptKeywordInput"
      />
      <el-select v-model="gptStatusFilter" class="gpt-filter-select" placeholder="状态">
        <el-option label="全部状态" value="all" />
        <el-option
          v-for="(label, status) in gptStatusText"
          :key="status"
          :label="label"
          :value="status"
        />
      </el-select>
      <el-select v-model="gptPlanFilter" class="gpt-filter-select" placeholder="套餐">
        <el-option label="全部套餐" value="all" />
        <el-option v-for="plan in planOptions" :key="plan" :label="plan" :value="plan" />
      </el-select>
      <el-segmented
        v-model="gptViewMode"
        :options="[
          { label: '卡片', value: 'cards' },
          { label: '列表', value: 'table' },
        ]"
      />
      <el-button
        type="primary"
        :icon="Refresh"
        :loading="gptAccountStore.refreshingAll"
        :disabled="gptAccountStore.refreshingAll || rows.length === 0"
        @click="refreshAllGptAccounts"
      >
        刷新全部
      </el-button>
      <el-tag v-if="focusedEmail && !focusExists" type="warning" effect="plain">
        该邮箱尚未绑定 GPT/Codex
      </el-tag>
    </div>

    <Transition name="workspace-view" mode="out-in">
      <div v-if="gptViewMode === 'cards'" key="cards" v-loading="gptAccountStore.loading" class="gpt-card-grid">
        <el-empty v-if="filteredRows.length === 0" description="暂无已绑定 GPT/Codex 账号" />
        <article
          v-for="row in filteredRows"
          v-else
          :key="row.account.mailAccountEmail"
          class="gpt-status-card"
          :class="[`status-${row.account.status}`, { focused: isFocusedRow(row) }]"
        >
          <header class="gpt-card-head">
            <div>
              <div class="gpt-card-title">
                <el-icon><User /></el-icon>
                <strong>{{ row.account.mailAccountEmail }}</strong>
              </div>
              <p>{{ row.account.gptEmail }}</p>
            </div>
            <el-tag :type="statusTagType(row.account)" effect="light">
              {{ statusLabel(row.account) }}
            </el-tag>
          </header>

          <div class="gpt-card-tags">
            <el-tag type="primary" effect="plain">{{ planText(row.account) }}</el-tag>
            <el-tag :type="expiryTagType(row.account)" effect="plain">
              到期 {{ formatShortDate(row.account.subscriptionActiveUntil) }}
            </el-tag>
            <el-tag v-if="row.mailbox?.group" effect="plain">{{ row.mailbox.group }}</el-tag>
            <el-tag v-else type="warning" effect="plain">邮箱不存在</el-tag>
          </div>

          <div class="gpt-quota-stack">
            <div class="gpt-quota-line">
              <div>
                <span>5h 剩余</span>
                <strong>{{ formatQuota(row.account.hourlyQuota) }}</strong>
              </div>
              <el-progress
                :percentage="quotaPercent(row.account.hourlyQuota)"
                :status="quotaStatus(row.account.hourlyQuota)"
              />
              <small>重置 {{ formatDateTime(row.account.hourlyQuota?.resetAt) }}</small>
            </div>
            <div class="gpt-quota-line">
              <div>
                <span>Weekly 剩余</span>
                <strong>{{ formatQuota(row.account.weeklyQuota) }}</strong>
              </div>
              <el-progress
                :percentage="quotaPercent(row.account.weeklyQuota)"
                :status="quotaStatus(row.account.weeklyQuota)"
              />
              <small>重置 {{ formatDateTime(row.account.weeklyQuota?.resetAt) }}</small>
            </div>
          </div>

          <p v-if="row.mailbox?.remark" class="gpt-card-remark">{{ row.mailbox.remark }}</p>
          <p v-if="statusReason(row.account)" class="gpt-card-error">{{ statusReason(row.account) }}</p>

          <footer class="gpt-card-footer">
            <span>最后刷新 {{ formatDateTime(row.account.lastRefreshAt) }}</span>
            <div>
              <el-button
                size="small"
                :icon="Refresh"
                :loading="gptAccountStore.refreshingEmails.includes(row.account.mailAccountEmail.toLowerCase())"
                :disabled="isGptBusy(row.account) || gptAccountStore.refreshingAll"
                @click="refreshGptAccount(row.account)"
              >
                刷新
              </el-button>
              <el-button size="small" :icon="Link" :disabled="isGptBusy(row.account)" @click="openGptDialog(row)">
                重绑
              </el-button>
              <el-button
                size="small"
                type="danger"
                plain
                :loading="gptAccountStore.unlinkingEmails.includes(row.account.mailAccountEmail.toLowerCase())"
                :disabled="isGptBusy(row.account)"
                @click="unlinkGptAccount(row.account)"
              >
                解绑
              </el-button>
            </div>
          </footer>
        </article>
      </div>

      <div v-else key="table" class="gpt-table-wrap">
        <el-table
          v-loading="gptAccountStore.loading"
          :data="filteredRows"
          height="100%"
          class="gpt-account-table"
          row-key="account.mailAccountEmail"
          :row-class-name="({ row }: { row: GptAccountRow }) => (isFocusedRow(row) ? 'focused-gpt-row' : '')"
        >
          <el-table-column label="邮箱" min-width="220" show-overflow-tooltip>
            <template #default="{ row }">{{ row.account.mailAccountEmail }}</template>
          </el-table-column>
          <el-table-column label="GPT邮箱" min-width="220" show-overflow-tooltip>
            <template #default="{ row }">{{ row.account.gptEmail }}</template>
          </el-table-column>
          <el-table-column label="分组" width="130" show-overflow-tooltip>
            <template #default="{ row }">{{ row.mailbox?.group || '邮箱不存在' }}</template>
          </el-table-column>
          <el-table-column label="备注" min-width="160" show-overflow-tooltip>
            <template #default="{ row }">{{ row.mailbox?.remark || '无备注' }}</template>
          </el-table-column>
          <el-table-column label="套餐" width="110">
            <template #default="{ row }">
              <el-tag type="primary" effect="plain">{{ planText(row.account) }}</el-tag>
            </template>
          </el-table-column>
          <el-table-column label="订阅到期" width="130">
            <template #default="{ row }">
              <el-tag :type="expiryTagType(row.account)" effect="plain">
                {{ formatShortDate(row.account.subscriptionActiveUntil) }}
              </el-tag>
            </template>
          </el-table-column>
          <el-table-column label="5h额度" width="150">
            <template #default="{ row }">
              <div class="gpt-table-quota">
                <strong>{{ formatQuota(row.account.hourlyQuota) }}</strong>
                <small>{{ formatDateTime(row.account.hourlyQuota?.resetAt) }}</small>
              </div>
            </template>
          </el-table-column>
          <el-table-column label="Weekly额度" width="150">
            <template #default="{ row }">
              <div class="gpt-table-quota">
                <strong>{{ formatQuota(row.account.weeklyQuota) }}</strong>
                <small>{{ formatDateTime(row.account.weeklyQuota?.resetAt) }}</small>
              </div>
            </template>
          </el-table-column>
          <el-table-column label="状态" width="110">
            <template #default="{ row }">
              <el-tooltip :content="statusReason(row.account)" :disabled="!statusReason(row.account)" placement="top">
                <el-tag :type="statusTagType(row.account)" effect="light">
                  {{ statusLabel(row.account) }}
                </el-tag>
              </el-tooltip>
            </template>
          </el-table-column>
          <el-table-column label="最后刷新" width="170">
            <template #default="{ row }">{{ formatDateTime(row.account.lastRefreshAt) }}</template>
          </el-table-column>
          <el-table-column label="操作" width="210" fixed="right" align="center">
            <template #header>
              <span>
                <el-icon><List /></el-icon>
                操作
              </span>
            </template>
            <template #default="{ row }">
              <el-button
                link
                size="small"
                :loading="gptAccountStore.refreshingEmails.includes(row.account.mailAccountEmail.toLowerCase())"
                :disabled="isGptBusy(row.account) || gptAccountStore.refreshingAll"
                @click="refreshGptAccount(row.account)"
              >
                刷新
              </el-button>
              <el-button link size="small" :disabled="isGptBusy(row.account)" @click="openGptDialog(row)">
                重绑
              </el-button>
              <el-button
                link
                size="small"
                type="danger"
                :loading="gptAccountStore.unlinkingEmails.includes(row.account.mailAccountEmail.toLowerCase())"
                :disabled="isGptBusy(row.account)"
                @click="unlinkGptAccount(row.account)"
              >
                解绑
              </el-button>
            </template>
          </el-table-column>
        </el-table>
      </div>
    </Transition>

    <GptAccountDialog
      v-model="gptDialogVisible"
      :account="gptDialogAccount"
      @bound="handleGptBound"
    />
  </section>
</template>
