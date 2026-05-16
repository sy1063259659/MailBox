<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import ImportAccountsDialog from '@/components/ImportAccountsDialog.vue'
import AccountWorkspaceView from '@/views/AccountWorkspaceView.vue'
import LoginView from '@/views/LoginView.vue'
import { getImapHealth } from '@/services/imapApi'
import { useAccountStore } from '@/stores/account'
import { useAuthStore } from '@/stores/auth'
import { useMailStore } from '@/stores/mail'

const authStore = useAuthStore()
const accountStore = useAccountStore()
const mailStore = useMailStore()
const importVisible = ref(false)
const backendOnline = ref<boolean | undefined>(undefined)
const exporting = ref(false)
const clearingData = ref(false)

onMounted(async () => {
  window.addEventListener('mailbox:unauthorized', handleUnauthorized)
  await Promise.all([authStore.checkSession(), checkBackend()])
  if (!authStore.isAuthenticated) {
    return
  }
  await Promise.all([accountStore.loadAccounts(), accountStore.refreshStats()])
  await mailStore.loadMessages()
})

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

async function exportData() {
  exporting.value = true
  try {
    const text = await accountStore.exportData()
    if (!text.trim()) {
      ElMessage.warning('没有可导出的账号')
      return
    }

    const blob = new Blob([text], {
      type: 'text/plain;charset=utf-8',
    })
    const url = URL.createObjectURL(blob)
    const anchor = document.createElement('a')
    anchor.href = url
    anchor.download = `mailbox-accounts-${new Date().toISOString().slice(0, 10)}.txt`
    anchor.click()
    URL.revokeObjectURL(url)
    ElMessage.success('账号已导出')
  } finally {
    exporting.value = false
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
          :exporting="exporting"
          :clearing-data="clearingData"
          @import-accounts="importVisible = true"
          @export-data="exportData"
          @clear-data="clearData"
        />
        <div v-else key="loading" class="app-loading">正在检查登录状态...</div>
      </Transition>
    </el-main>

    <ImportAccountsDialog v-model="importVisible" />
  </el-container>
</template>
