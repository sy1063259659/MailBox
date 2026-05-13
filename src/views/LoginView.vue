<script setup lang="ts">
import { ref } from 'vue'
import { ElMessage } from 'element-plus'
import { Message } from '@element-plus/icons-vue'
import { useAuthStore } from '@/stores/auth'

const authStore = useAuthStore()
const username = ref('admin')
const password = ref('')

async function submitLogin() {
  if (!username.value.trim() || !password.value) {
    ElMessage.warning('请输入用户名和密码')
    return
  }

  try {
    await authStore.login(username.value.trim(), password.value)
    ElMessage.success('登录成功')
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
          <h1>MailBox</h1>
          <p>登录后管理邮箱账号与分组</p>
        </div>
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
