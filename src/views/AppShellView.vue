<script setup lang="ts">
import { defineAsyncComponent, ref } from 'vue'
import type { AppRouteName } from '@/composables/useAppRoute'
import { useLocalUiState } from '@/composables/useLocalUiState'
import { useAuthStore } from '@/stores/auth'

type MailboxSection = 'outlook' | 'icloud' | 'icloudHme'

const MailboxManagementView = defineAsyncComponent(() => import('@/views/AccountWorkspaceView.vue'))
const ICloudAccountManagementView = defineAsyncComponent(() => import('@/views/ICloudAccountManagementView.vue'))
const ICloudHMEManagementView = defineAsyncComponent(() => import('@/views/ICloudHMEManagementView.vue'))

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
const activeSection = useLocalUiState<MailboxSection>('mailbox.ui.mailboxSection', 'outlook', {
  validate: (value): value is MailboxSection => value === 'outlook' || value === 'icloud' || value === 'icloudHme',
})

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
    // The UI has already returned to the login page.
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
        <el-segmented
          v-model="activeSection"
          :options="[
            { label: 'Outlook / Hotmail', value: 'outlook' },
            { label: 'iCloud', value: 'icloud' },
            { label: 'iCloud隐藏邮箱', value: 'icloudHme' },
          ]"
          class="mailbox-section-switch"
        />
      </div>
      <div class="topbar-actions">
        <el-button v-if="activeSection === 'outlook'" plain @click="emit('clearData')">清空本地缓存</el-button>
        <el-button plain :loading="loggingOut" :disabled="loggingOut" @click="logout">退出登录</el-button>
      </div>
    </header>

    <main class="workspace-content">
      <MailboxManagementView
        v-if="activeSection === 'outlook'"
        :clearing-data="clearingData"
        @import-accounts="emit('importAccounts')"
      />
      <ICloudAccountManagementView v-else-if="activeSection === 'icloud'" />
      <ICloudHMEManagementView v-else />
    </main>
  </section>
</template>