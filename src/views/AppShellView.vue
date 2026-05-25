<script setup lang="ts">
import { computed, defineAsyncComponent } from 'vue'
import { Grid, Message } from '@element-plus/icons-vue'
import type { AppRouteName } from '@/composables/useAppRoute'
import { useAccountStore } from '@/stores/account'
import { useGptAccountStore } from '@/stores/gptAccount'

const MailboxManagementView = defineAsyncComponent(() => import('@/views/AccountWorkspaceView.vue'))
const GptAccountManagementView = defineAsyncComponent(() => import('@/views/GptAccountManagementView.vue'))

const props = defineProps<{
  clearingData?: boolean
  focusEmail?: string
  route: AppRouteName
}>()

const emit = defineEmits<{
  importAccounts: []
  clearData: []
  focusGptAccount: [email: string]
  navigateApp: [route: AppRouteName]
}>()

const accountStore = useAccountStore()
const gptAccountStore = useGptAccountStore()
const activeModule = computed(() => (props.route === 'gptAccounts' ? 'gptAccounts' : 'mailboxes'))

function goToLogin() {
  emit('navigateApp', 'login')
}
</script>

<template>
  <section class="faka-shell" :class="{ 'gpt-module-active': activeModule === 'gptAccounts' }">
    <main class="faka-main">
      <header class="faka-topbar">
        <div class="topbar-left">
          <div class="module-tabs" aria-label="主菜单">
            <button class="module-tab" :class="{ active: activeModule === 'mailboxes' }" type="button" @click="emit('navigateApp', 'mailboxes')">
              <el-icon><Message /></el-icon>
              <span>邮箱管理</span>
              <strong>{{ accountStore.accounts.length }}</strong>
            </button>
            <button class="module-tab" :class="{ active: activeModule === 'gptAccounts' }" type="button" @click="emit('navigateApp', 'gptAccounts')">
              <el-icon><Grid /></el-icon>
              <span>GPT账号管理</span>
              <strong>{{ gptAccountStore.accounts.length }}</strong>
            </button>
          </div>
        </div>
        <div class="topbar-actions">
          <el-button plain @click="emit('clearData')">清空本地缓存</el-button>
          <el-button plain @click="goToLogin">退出登录</el-button>
        </div>
      </header>

      <MailboxManagementView
        v-if="activeModule === 'mailboxes'"
        :clearing-data="props.clearingData"
        @focus-gpt-account="emit('focusGptAccount', $event)"
        @navigate-app="emit('navigateApp', $event)"
        @import-accounts="emit('importAccounts')"
      />

      <GptAccountManagementView
        v-else
        :focus-email="props.focusEmail"
      />
    </main>
  </section>
</template>
