<script setup lang="ts">
import { computed, defineAsyncComponent, onMounted, ref, watch } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import LoginView from '@/views/LoginView.vue'
import { getImapHealth } from '@/services/imapApi'
import { useAccountStore } from '@/stores/account'
import { useAuthStore } from '@/stores/auth'
import { useMailStore } from '@/stores/mail'
import { navigateToAppRoute, useAppRoute, type AppRouteName } from '@/composables/useAppRoute'

const AppShellView = defineAsyncComponent(() => import('@/views/AppShellView.vue'))
const ImportAccountsDialog = defineAsyncComponent(() => import('@/components/ImportAccountsDialog.vue'))

const authStore = useAuthStore()
const accountStore = useAccountStore()
const mailStore = useMailStore()
const appRoute = useAppRoute()
const importVisible = ref(false)
const backendOnline = ref<boolean | undefined>(undefined)
const clearingData = ref(false)
let workspaceLoadPromise: Promise<void> | undefined

const isAuthenticated = computed(() => authStore.checked && authStore.isAuthenticated)

onMounted(async () => {
  window.addEventListener('gptbox:unauthorized', handleUnauthorized)
  window.addEventListener('mailbox:unauthorized', handleUnauthorized)
  await Promise.all([authStore.checkSession(), checkBackend()])
  if (authStore.isAuthenticated) {
    await loadWorkspaceData()
    ensureRouteWhenAuthenticated()
  } else {
    navigateToAppRoute('login')
  }
})

watch(
  () => authStore.isAuthenticated,
  async (isAuthenticated, wasAuthenticated) => {
    if (isAuthenticated && !wasAuthenticated) {
      await loadWorkspaceData()
      ensureRouteWhenAuthenticated()
    }
  },
)

function ensureRouteWhenAuthenticated() {
  if (appRoute.value === 'login') {
    navigateToAppRoute('mailboxes')
  }
}

async function loadWorkspaceData() {
  if (!authStore.isAuthenticated) {
    return
  }

  workspaceLoadPromise ??= (async () => {
    await Promise.all([accountStore.loadAccounts(), accountStore.refreshStats()])
    await mailStore.loadMessages()
  })().finally(() => {
    workspaceLoadPromise = undefined
  })

  await workspaceLoadPromise
}

function handleUnauthorized() {
  authStore.markLoggedOut()
  navigateToAppRoute('login')
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

function handleNavigate(route: AppRouteName) {
  navigateToAppRoute(route)
}

</script>

<template>
  <el-container class="app-shell">
    <el-main class="app-main">
      <Transition name="app-view" mode="out-in">
        <LoginView
          v-if="appRoute === 'login' && authStore.checked && !authStore.isAuthenticated"
          key="login"
          :backend-online="backendOnline"
          @navigate-app="handleNavigate"
        />
        <AppShellView
          v-else-if="authStore.checked && isAuthenticated"
          key="workspace"
          :clearing-data="clearingData"
          @import-accounts="importVisible = true"
          @clear-data="clearData"
          @navigate-app="handleNavigate"
        />
        <div v-else key="loading" class="app-loading">正在检查登录状态...</div>
      </Transition>
    </el-main>

    <ImportAccountsDialog v-model="importVisible" />
  </el-container>
</template>
