<script setup lang="ts">
import { computed, defineAsyncComponent, nextTick, ref, watch } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import type { AppRouteName } from '@/composables/useAppRoute'
import { useLocalUiState } from '@/composables/useLocalUiState'
import { getIntegrationAPIKey, resetIntegrationAPIKey } from '@/services/integrationSettingsApi'
import { useAuthStore } from '@/stores/auth'

type MailboxSection = 'outlook' | 'icloud' | 'icloudHme' | 'sms' | 'cards'

const MailboxManagementView = defineAsyncComponent(() => import('@/views/AccountWorkspaceView.vue'))
const ICloudAccountManagementView = defineAsyncComponent(() => import('@/views/ICloudAccountManagementView.vue'))
const ICloudHMEManagementView = defineAsyncComponent(() => import('@/views/ICloudHMEManagementView.vue'))
const SMSManagementView = defineAsyncComponent(() => import('@/views/SMSManagementView.vue'))
const PaymentCardManagementView = defineAsyncComponent(() => import('@/views/PaymentCardManagementView.vue'))

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
const settingsVisible = ref(false)
const settingsLoading = ref(false)
const resettingAPIKey = ref(false)
const integrationAPIKey = ref('')
const activeSection = useLocalUiState<MailboxSection>('mailbox.ui.mailboxSection', 'outlook', {
  validate: (value): value is MailboxSection => value === 'outlook' || value === 'icloud' || value === 'icloudHme' || value === 'sms' || value === 'cards',
})
const workspaceTitle = computed(() => activeSection.value === 'sms' ? '接码管理' : activeSection.value === 'cards' ? '卡管理' : '邮箱管理')

watch(activeSection, async () => {
  await nextTick()
  document.querySelector('.mailbox-section-switch .el-segmented__item.is-selected')
    ?.scrollIntoView({ behavior: 'smooth', block: 'nearest', inline: 'nearest' })
}, { immediate: true })

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

async function openSettings() {
  settingsVisible.value = true
  settingsLoading.value = true
  try {
    integrationAPIKey.value = await getIntegrationAPIKey()
  } catch (error) {
    ElMessage.error(error instanceof Error ? error.message : '加载 API Key 失败')
  } finally {
    settingsLoading.value = false
  }
}

async function copyAPIKey() {
  if (!integrationAPIKey.value) return
  await navigator.clipboard.writeText(integrationAPIKey.value)
  ElMessage.success('API Key 已复制')
}

async function resetAPIKey() {
  await ElMessageBox.confirm(
    '重置后旧 API Key 将立即失效，使用 MailBox 的本地项目需要更新为新 Key。确认继续？',
    '重置 Integration API Key',
    { type: 'warning', confirmButtonText: '确认重置', cancelButtonText: '取消' },
  )
  resettingAPIKey.value = true
  try {
    integrationAPIKey.value = await resetIntegrationAPIKey()
    ElMessage.success('API Key 已重置，请复制并更新调用方')
  } finally {
    resettingAPIKey.value = false
  }
}
</script>

<template>
  <section class="workspace-shell">
    <header class="workspace-topbar">
      <div class="topbar-left">
        <strong class="workspace-title">{{ workspaceTitle }}</strong>
        <el-segmented
          v-model="activeSection"
          :options="[
            { label: 'Outlook / Hotmail', value: 'outlook' },
            { label: 'iCloud', value: 'icloud' },
            { label: 'iCloud隐藏邮箱', value: 'icloudHme' },
            { label: '接码管理', value: 'sms' },
            { label: '卡管理', value: 'cards' },
          ]"
          class="mailbox-section-switch"
        />
      </div>
      <div class="topbar-actions">
        <el-button v-if="activeSection === 'outlook'" plain @click="emit('clearData')">清空本地缓存</el-button>
        <el-button plain @click="openSettings">设置</el-button>
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
      <ICloudHMEManagementView v-else-if="activeSection === 'icloudHme'" />
      <SMSManagementView v-else-if="activeSection === 'sms'" />
      <PaymentCardManagementView v-else />
    </main>

    <el-dialog v-model="settingsVisible" title="设置" width="560px">
      <div v-loading="settingsLoading" class="integration-key-settings">
        <div class="integration-key-heading">
          <div>
            <strong>Integration API Key</strong>
            <p>供外部任务程序连接 MailBox 使用。重置后旧 Key 会立即失效。</p>
          </div>
          <el-button type="danger" plain :loading="resettingAPIKey" @click="resetAPIKey">重置</el-button>
        </div>
        <div class="integration-key-value">
          <el-input v-model="integrationAPIKey" readonly type="password" show-password />
          <el-button type="primary" :disabled="!integrationAPIKey" @click="copyAPIKey">复制</el-button>
        </div>
      </div>
    </el-dialog>
  </section>
</template>

<style scoped>
.integration-key-settings{min-height:100px}.integration-key-heading{display:flex;align-items:flex-start;justify-content:space-between;gap:20px}.integration-key-heading strong{font-size:16px}.integration-key-heading p{margin:8px 0 16px;color:var(--el-text-color-secondary);font-size:13px;line-height:1.6}.integration-key-value{display:flex;gap:10px}.integration-key-value .el-input{flex:1}
</style>
