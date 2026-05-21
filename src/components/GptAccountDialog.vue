<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { ElMessage } from 'element-plus'
import { Link, Promotion, Refresh, Tickets } from '@element-plus/icons-vue'
import { useGptAccountStore } from '@/stores/gptAccount'
import type { MailAccount } from '@/types'

const visible = defineModel<boolean>({ required: true })
const props = defineProps<{
  account?: MailAccount
}>()

const emit = defineEmits<{
  bound: []
}>()

const gptAccountStore = useGptAccountStore()
const bindMode = ref<'tokenJson' | 'callbackUrl'>('tokenJson')
const tokenJson = ref('')
const callbackUrl = ref('')
const binding = ref(false)
const startingOAuth = ref(false)

const title = computed(() => props.account ? `绑定 GPT/Codex：${props.account.email}` : '绑定 GPT/Codex')
const canSubmit = computed(() => {
  if (!props.account || binding.value) {
    return false
  }
  return bindMode.value === 'tokenJson'
    ? tokenJson.value.trim().length > 0
    : Boolean(currentAuthUrl.value) && callbackUrl.value.trim().length > 0
})
const currentAuthUrl = computed(() => props.account
  ? gptAccountStore.oauthAuthUrlByEmail[props.account.email.toLowerCase()]
  : '',
)

watch(visible, (isVisible) => {
  if (isVisible) {
    bindMode.value = 'tokenJson'
    tokenJson.value = ''
    callbackUrl.value = ''
  }
})

async function startOAuth() {
  if (!props.account || startingOAuth.value) {
    return
  }
  startingOAuth.value = true
  try {
    const authUrl = await gptAccountStore.startOAuth(props.account.email)
    window.open(authUrl, '_blank', 'noopener,noreferrer')
    ElMessage.success('已打开授权页面，授权后请粘贴回调 URL')
  } catch (error) {
    ElMessage.error(error instanceof Error ? error.message : '发起 OAuth 授权失败')
  } finally {
    startingOAuth.value = false
  }
}

async function submit() {
  if (!props.account || !canSubmit.value) {
    return
  }

  binding.value = true
  try {
    if (bindMode.value === 'tokenJson') {
      await gptAccountStore.bindByTokenJson(props.account.email, tokenJson.value.trim())
    } else {
      await gptAccountStore.completeOAuth(props.account.email, callbackUrl.value.trim())
    }
    ElMessage.success('GPT/Codex 账号已绑定')
    visible.value = false
    emit('bound')
  } catch (error) {
    ElMessage.error(error instanceof Error ? error.message : '绑定 GPT/Codex 账号失败')
  } finally {
    binding.value = false
  }
}
</script>

<template>
  <el-dialog v-model="visible" :title="title" width="560px" class="gpt-account-dialog">
    <el-form label-position="top">
      <el-form-item label="绑定方式">
        <el-segmented
          v-model="bindMode"
          :options="[
            { label: 'Token JSON', value: 'tokenJson' },
            { label: 'OAuth 回调 URL', value: 'callbackUrl' },
          ]"
        />
      </el-form-item>

      <Transition name="content-fade" mode="out-in">
        <el-form-item v-if="bindMode === 'tokenJson'" key="token-json" label="完整 Token JSON">
          <el-input
            v-model="tokenJson"
            type="textarea"
            :rows="8"
            resize="vertical"
            placeholder='{"tokens":{"id_token":"...","access_token":"...","refresh_token":"...","account_id":"..."}}'
          />
        </el-form-item>
        <div v-else key="callback-url">
          <el-form-item label="OAuth 授权">
            <div class="gpt-oauth-actions">
              <el-button :icon="Promotion" :loading="startingOAuth" @click="startOAuth">
                打开授权链接
              </el-button>
              <el-link v-if="currentAuthUrl" :href="currentAuthUrl" target="_blank" type="primary">
                重新打开
              </el-link>
            </div>
          </el-form-item>
          <el-form-item label="粘贴授权后的回调 URL">
            <el-input
              v-model="callbackUrl"
              type="textarea"
              :rows="5"
              resize="vertical"
              placeholder="http://localhost:1455/auth/callback?code=...&state=..."
            />
          </el-form-item>
        </div>
      </Transition>

      <div class="gpt-bind-mode-hints">
        <div>
          <el-icon><Tickets /></el-icon>
          <span>Token JSON 会原样提交给后端解析和保存。</span>
        </div>
        <div>
          <el-icon><Link /></el-icon>
          <span>OAuth 会先生成授权链接，授权后粘贴回调 URL 完成绑定。</span>
        </div>
      </div>
    </el-form>

    <template #footer>
      <el-button @click="visible = false">取消</el-button>
      <el-button
        type="primary"
        :icon="Refresh"
        :loading="binding"
        :disabled="!canSubmit"
        @click="submit"
      >
        绑定
      </el-button>
    </template>
  </el-dialog>
</template>
