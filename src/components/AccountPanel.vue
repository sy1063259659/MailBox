<script setup lang="ts">
import { computed } from 'vue'
import { Delete, Refresh } from '@element-plus/icons-vue'
import type { AccountStatus, MailAccount } from '../types'

const props = defineProps<{
  accounts: MailAccount[]
  groups: string[]
  selectedGroup: string
  loading: boolean
}>()

const emit = defineEmits<{
  'update:selectedGroup': [value: string]
  deleteAccount: [email: string]
  refresh: []
}>()

const groupOptions = computed(() => ['全部分组', ...props.groups])
const filteredAccounts = computed(() => {
  if (!props.selectedGroup || props.selectedGroup === '全部分组') {
    return props.accounts
  }
  return props.accounts.filter((account) => account.group === props.selectedGroup)
})

const statusMeta: Record<AccountStatus, { label: string; type: 'info' | 'success' | 'warning' | 'danger' | 'primary' }> = {
  idle: { label: '待同步', type: 'info' },
  syncing: { label: '同步中', type: 'primary' },
  success: { label: '正常', type: 'success' },
  error: { label: '错误', type: 'danger' },
  token_expired: { label: '令牌过期', type: 'warning' },
  rate_limited: { label: '限流', type: 'warning' },
}

const formatDateTime = (value?: string): string => {
  if (!value) {
    return '未同步'
  }
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) {
    return value
  }
  return date.toLocaleString('zh-CN', { hour12: false })
}
</script>

<template>
  <section class="account-panel">
    <div class="panel-header">
      <div>
        <h2>账号与分组</h2>
        <p>共 {{ accounts.length }} 个账号，{{ groups.length }} 个分组</p>
      </div>
      <div class="panel-actions">
        <el-select
          :model-value="selectedGroup || '全部分组'"
          class="group-select"
          :disabled="loading"
          @update:model-value="emit('update:selectedGroup', $event)"
        >
          <el-option v-for="group in groupOptions" :key="group" :label="group" :value="group" />
        </el-select>
        <el-button :icon="Refresh" :loading="loading" @click="emit('refresh')">刷新</el-button>
      </div>
    </div>

    <el-empty v-if="filteredAccounts.length === 0" description="暂无账号">
      <el-text type="info">通过顶部“导入账号”添加本地账号。</el-text>
    </el-empty>

    <div v-else class="account-list">
      <article v-for="account in filteredAccounts" :key="account.email" class="account-item">
        <div class="account-main">
          <div class="account-title-line">
            <strong>{{ account.displayName || account.email }}</strong>
            <el-tag :type="statusMeta[account.status].type" effect="light" size="small">
              {{ statusMeta[account.status].label }}
            </el-tag>
          </div>
          <el-text class="account-email" type="info">{{ account.email }}</el-text>
          <div class="account-meta">
            <el-tag size="small">{{ account.group || '未分组' }}</el-tag>
            <span>最近同步：{{ formatDateTime(account.lastSyncAt) }}</span>
            <span>更新：{{ formatDateTime(account.updatedAt) }}</span>
          </div>
          <el-alert
            v-if="account.errorMessage"
            class="account-error"
            :title="account.errorMessage"
            type="error"
            show-icon
            :closable="false"
          />
        </div>

        <el-button
          type="danger"
          plain
          :icon="Delete"
          :disabled="loading || account.status === 'syncing'"
          @click="emit('deleteAccount', account.email)"
        >
          删除
        </el-button>
      </article>
    </div>
  </section>
</template>
