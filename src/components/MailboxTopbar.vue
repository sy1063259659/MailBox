<script setup lang="ts">
import { Search } from '@element-plus/icons-vue'

type WorkspaceMode = 'accounts' | 'mail'

defineProps<{
  searchValue: string
  workspaceMode: WorkspaceMode
}>()

const emit = defineEmits<{
  searchInput: [value: string]
  backToAccounts: []
}>()
</script>

<template>
  <header class="faka-topbar">
    <div class="topbar-left">
      <el-input
        :model-value="searchValue"
        class="faka-search"
        :prefix-icon="Search"
        clearable
        :placeholder="workspaceMode === 'mail' ? '搜索主题/发件人...' : '搜索邮件或账号...'"
        @update:model-value="emit('searchInput', $event)"
      />
    </div>
    <div class="topbar-actions">
      <el-button v-if="workspaceMode === 'mail'" plain @click="emit('backToAccounts')">返回账号</el-button>
    </div>
  </header>
</template>
