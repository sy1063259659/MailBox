<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { ElMessage } from 'element-plus'
import { CopyDocument, Link, Promotion, Refresh, Tickets } from '@element-plus/icons-vue'
import { getImapMessage, listImapMessages, type ImapMessageSummary } from '@/services/imapApi'
import { useGptAccountStore } from '@/stores/gptAccount'
import type { MailAccount, MailFolder } from '@/types'

const visible = defineModel<boolean>({ required: true })
const props = defineProps<{
  account?: MailAccount
}>()

const emit = defineEmits<{
  bound: [payload: { mailAccountEmail: string }]
}>()

const gptAccountStore = useGptAccountStore()
const bindMode = ref<'tokenJson' | 'callbackUrl'>('tokenJson')
const tokenJson = ref('')
const callbackUrl = ref('')
const binding = ref(false)
const startingOAuth = ref(false)
const mailFolder = ref<MailFolder>('inbox')
const mailLoading = ref(false)
const bodyLoading = ref(false)
const verificationMessages = ref<ImapMessageSummary[]>([])
const selectedMessageId = ref('')
const selectedBody = ref('')
const selectedSubject = ref('')

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
const selectedCode = computed(() => extractVerificationCode(`${selectedSubject.value}\n${selectedBody.value}`))
const selectedBodyText = computed(() => cleanMailText(selectedBody.value))

watch(visible, (isVisible) => {
  if (isVisible) {
    bindMode.value = 'tokenJson'
    tokenJson.value = ''
    callbackUrl.value = ''
    mailFolder.value = 'inbox'
    verificationMessages.value = []
    selectedMessageId.value = ''
    selectedBody.value = ''
    selectedSubject.value = ''
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

  const mailAccountEmail = props.account.email
  binding.value = true
  let refreshError: unknown
  try {
    if (bindMode.value === 'tokenJson') {
      await gptAccountStore.bindByTokenJson(mailAccountEmail, tokenJson.value.trim())
    } else {
      await gptAccountStore.completeOAuth(mailAccountEmail, callbackUrl.value.trim())
    }

    try {
      await gptAccountStore.refreshOne(mailAccountEmail)
    } catch (error) {
      refreshError = error
    }

    if (refreshError) {
      ElMessage.warning(refreshError instanceof Error ? `绑定成功，但刷新状态失败：${refreshError.message}` : '绑定成功，但刷新状态失败')
    } else {
      ElMessage.success('GPT/Codex 账号已绑定并刷新状态')
    }
    visible.value = false
    emit('bound', { mailAccountEmail })
  } catch (error) {
    ElMessage.error(error instanceof Error ? error.message : '绑定 GPT/Codex 账号失败')
  } finally {
    binding.value = false
  }
}

async function refreshVerificationMessages() {
  if (!props.account || mailLoading.value) {
    return
  }
  mailLoading.value = true
  selectedMessageId.value = ''
  selectedBody.value = ''
  selectedSubject.value = ''
  try {
    const result = await listImapMessages({
      credentials: { email: props.account.email },
      folder: mailFolder.value,
      limit: 12,
    })
    verificationMessages.value = result.messages ?? []
  } catch (error) {
    ElMessage.error(error instanceof Error ? error.message : '刷新验证码邮件失败')
  } finally {
    mailLoading.value = false
  }
}

async function selectVerificationMessage(message: ImapMessageSummary) {
  if (!props.account || bodyLoading.value) {
    return
  }
  selectedMessageId.value = message.id
  selectedSubject.value = message.subject
  selectedBody.value = ''
  bodyLoading.value = true
  try {
    const result = await getImapMessage({
      credentials: { email: props.account.email },
      folder: mailFolder.value,
      messageId: message.id,
    })
    selectedSubject.value = result.message.subject || message.subject
    selectedBody.value = result.body.content || result.message.content || ''
  } catch (error) {
    ElMessage.error(error instanceof Error ? error.message : '读取邮件正文失败')
  } finally {
    bodyLoading.value = false
  }
}

async function copyValue(value: string, label: string) {
  if (!value.trim()) {
    ElMessage.warning(`没有可复制的${label}`)
    return
  }
  try {
    await navigator.clipboard.writeText(value.trim())
    ElMessage.success(`已复制${label}`)
  } catch {
    ElMessage.error(`复制${label}失败`)
  }
}

function senderText(message: ImapMessageSummary): string {
  const from = message.from?.[0]
  return from?.email || from?.name || '未知发件人'
}

function formatShortTime(value: string): string {
  if (!value) {
    return ''
  }
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) {
    return value
  }
  return date.toLocaleString()
}

function cleanMailText(value: string): string {
  return value
    .replace(/<script[\s\S]*?<\/script>/gi, ' ')
    .replace(/<style[\s\S]*?<\/style>/gi, ' ')
    .replace(/<[^>]+>/g, ' ')
    .replace(/&nbsp;/gi, ' ')
    .replace(/&amp;/gi, '&')
    .replace(/&lt;/gi, '<')
    .replace(/&gt;/gi, '>')
    .replace(/\s+/g, ' ')
    .trim()
}

function extractVerificationCode(value: string): string {
  const text = cleanMailText(value)
  const candidates = text.match(/\b[A-Z0-9]{4,8}\b/gi) ?? []
  const preferred = candidates.find((candidate) => /\d/.test(candidate))
  return preferred ?? candidates[0] ?? ''
}
</script>

<template>
  <el-dialog v-model="visible" :title="title" width="980px" class="gpt-account-dialog">
    <div class="gpt-bind-layout">
      <el-form label-position="top" class="gpt-bind-form">
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

      <aside v-if="account" class="gpt-verification-panel">
        <header>
          <div>
            <strong>验证码邮件</strong>
            <span>{{ account.email }}</span>
          </div>
          <el-button
            size="small"
            :icon="Refresh"
            :loading="mailLoading"
            @click="refreshVerificationMessages"
          >
            刷新邮件
          </el-button>
        </header>
        <el-segmented
          v-model="mailFolder"
          size="small"
          :options="[
            { label: '收件箱', value: 'inbox' },
            { label: '垃圾箱', value: 'junkemail' },
          ]"
        />
        <div v-loading="mailLoading" class="gpt-verification-list">
          <el-empty v-if="verificationMessages.length === 0" :image-size="56" description="暂无邮件" />
          <button
            v-for="message in verificationMessages"
            v-else
            :key="message.id"
            type="button"
            class="gpt-verification-item"
            :class="{ active: selectedMessageId === message.id }"
            @click="selectVerificationMessage(message)"
          >
            <span>{{ senderText(message) }}</span>
            <strong>{{ message.subject || '无主题' }}</strong>
            <small>{{ formatShortTime(message.receivedAt) }}</small>
          </button>
        </div>
        <div v-loading="bodyLoading" class="gpt-verification-body">
          <div class="verification-copy-row">
            <el-button
              size="small"
              :icon="CopyDocument"
              :disabled="!selectedCode"
              @click="copyValue(selectedCode, '验证码')"
            >
              复制验证码
            </el-button>
            <el-button
              size="small"
              :icon="CopyDocument"
              :disabled="!selectedBodyText"
              @click="copyValue(selectedBodyText, '正文')"
            >
              复制正文
            </el-button>
          </div>
          <p v-if="selectedCode" class="verification-code">{{ selectedCode }}</p>
          <p v-if="selectedBodyText" class="verification-body-text">{{ selectedBodyText }}</p>
          <el-empty v-else :image-size="46" description="选择邮件查看正文" />
        </div>
      </aside>
    </div>

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
