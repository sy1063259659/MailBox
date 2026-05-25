<script setup lang="ts">
import { computed, ref } from 'vue'
import { ElMessage } from 'element-plus'
import { Message } from '@element-plus/icons-vue'
import { useAuthStore } from '@/stores/auth'
import type { AppRouteName } from '@/composables/useAppRoute'

const authStore = useAuthStore()
const username = ref('admin')
const password = ref('')
const props = defineProps<{
  backendOnline?: boolean
}>()

const emit = defineEmits<{
  navigateApp: [route: AppRouteName]
}>()

const backendStatusText = computed(() => {
  if (props.backendOnline === true) {
    return '后端在线'
  }
  if (props.backendOnline === false) {
    return '后端离线'
  }
  return '正在检测后端'
})

async function submitLogin() {
  if (!username.value.trim() || !password.value) {
    ElMessage.warning('请输入用户名和密码')
    return
  }

  try {
    await authStore.login(username.value.trim(), password.value)
    ElMessage.success('登录成功')
    emit('navigateApp', 'mailboxes')
  } catch (error) {
    ElMessage.error(error instanceof Error ? error.message : '登录失败')
  }
}
</script>

<template>
  <main class="login-shell">
    <section class="login-card">
      <div class="login-brand">
        <el-icon><Message /></el-icon>
        <div>
          <h1>GptBox</h1>
        </div>
      </div>

      <div class="login-status-row">
        <el-tag :type="backendOnline === false ? 'danger' : 'success'" effect="plain">
          {{ backendStatusText }}
        </el-tag>
      </div>

      <el-form label-position="top" @submit.prevent="submitLogin">
        <el-form-item label="用户名">
          <el-input v-model="username" autocomplete="username" placeholder="admin" />
        </el-form-item>
        <el-form-item label="密码">
          <el-input
            v-model="password"
            type="password"
            autocomplete="current-password"
            show-password
            placeholder="请输入管理员密码"
            @keyup.enter="submitLogin"
          />
        </el-form-item>
        <el-button type="primary" size="large" class="login-submit" :loading="authStore.loading" @click="submitLogin">
          登录
        </el-button>
      </el-form>
    </section>
  </main>
</template>
