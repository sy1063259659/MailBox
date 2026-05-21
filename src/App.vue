<script setup lang="ts">
import { defineAsyncComponent, onMounted, ref, watch } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import LoginView from '@/views/LoginView.vue'
import { getImapHealth } from '@/services/imapApi'
import { useAccountStore } from '@/stores/account'
import { useAuthStore } from '@/stores/auth'
import { useGptAccountStore } from '@/stores/gptAccount'
import { useMailStore } from '@/stores/mail'

const AccountWorkspaceView = defineAsyncComponent(() => import('@/views/AccountWorkspaceView.vue'))
const ImportAccountsDialog = defineAsyncComponent(() => import('@/components/ImportAccountsDialog.vue'))

const authStore = useAuthStore()
const accountStore = useAccountStore()
const gptAccountStore = useGptAccountStore()
const mailStore = useMailStore()
const importVisible = ref(false)
const backendOnline = ref<boolean | undefined>(undefined)
const clearingData = ref(false)
let workspaceLoadPromise: Promise<void> | undefined

onMounted(async () => {
  window.addEventListener('mailbox:unauthorized', handleUnauthorized)
  await Promise.all([authStore.checkSession(), checkBackend()])
  await loadWorkspaceData()
})

watch(
  () => authStore.isAuthenticated,
  async (isAuthenticated, wasAuthenticated) => {
    if (isAuthenticated && !wasAuthenticated) {
      await loadWorkspaceData()
    }
  },
)

async function loadWorkspaceData() {
  if (!authStore.isAuthenticated) {
    return
  }

  workspaceLoadPromise ??= (async () => {
    await Promise.all([accountStore.loadAccounts(), accountStore.refreshStats(), gptAccountStore.loadAccounts()])
    await mailStore.loadMessages()
  })().finally(() => {
    workspaceLoadPromise = undefined
  })

  await workspaceLoadPromise
}

function handleUnauthorized() {
  authStore.markLoggedOut()
}

async function checkBackend() {
  try {
    const result = await getImapHealth()
    backendOnline.value = result.ok
  } catch {
    backendOnline.value = false
  }
}

async function clearData() {
  await ElMessageBox.confirm('这只会删除浏览器本地保存的邮件列表、正文缓存和同步状态，不会删除数据库账号。', '清空本地缓存', {
    confirmButtonText: '清空',
    cancelButtonText: '取消',
    type: 'warning',
  })
  clearingData.value = true
  try {
    await accountStore.clearLocalMailCache()
    await mailStore.loadMessages()
    ElMessage.success('本地邮件缓存已清空')
  } finally {
    clearingData.value = false
  }
}
</script>

<template>
  <el-container class="app-shell">
    <el-main class="app-main">
      <Transition name="app-view" mode="out-in">
        <LoginView v-if="authStore.checked && !authStore.isAuthenticated" key="login" />
        <AccountWorkspaceView
          v-else-if="authStore.checked"
          key="workspace"
          :clearing-data="clearingData"
          @import-accounts="importVisible = true"
          @clear-data="clearData"
        />
        <div v-else key="loading" class="app-loading">正在检查登录状态...</div>
      </Transition>
    </el-main>

    <ImportAccountsDialog v-model="importVisible" />
  </el-container>
</template>
