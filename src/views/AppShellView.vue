<script setup lang="ts">
import { defineAsyncComponent, ref } from 'vue'
import type { AppRouteName } from '@/composables/useAppRoute'
import { useAuthStore } from '@/stores/auth'

const MailboxManagementView = defineAsyncComponent(() => import('@/views/AccountWorkspaceView.vue'))

defineProps<{
  clearingData?: boolean
}>()

const emit = defineEmits<{
  importAccounts: []
  clearData: []
  navigateApp: [route: AppRouteName]
}>()

const authStore = useAuthStore()
const loggingOut = ref(false)

async function logout() {
  if (loggingOut.value) {
    return
  }
  loggingOut.value = true
  authStore.markLoggedOut()
  emit('navigateApp', 'login')
  try {
    await authStore.logout()
  } catch {
    // The UI has already returned to the login page. A failed remote logout only
    // leaves the server cookie to expire naturally or be replaced on next login.
  } finally {
    loggingOut.value = false
  }
}
</script>

<template>
  <section class="workspace-shell">
    <header class="workspace-topbar">
      <div class="topbar-left">
        <strong class="workspace-title">邮箱管理</strong>
      </div>
      <div class="topbar-actions">
        <el-button plain @click="emit('clearData')">清空本地缓存</el-button>
        <el-button plain :loading="loggingOut" :disabled="loggingOut" @click="logout">退出登录</el-button>
      </div>
    </header>

    <main class="workspace-content">
      <MailboxManagementView
        :clearing-data="clearingData"
        @import-accounts="emit('importAccounts')"
      />
    </main>
  </section>
</template>
