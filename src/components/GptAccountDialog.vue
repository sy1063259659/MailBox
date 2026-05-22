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
const verificationLoading = ref(false)
const verificationMessages = ref<ImapMessageSummary[]>([])
const verificationCode = ref('')
const verificationSource = ref<ImapMessageSummary>()
const lastVerificationRefreshAt = ref('')
const verificationStatusText = ref('刷新后会自动从最新邮件里提取验证码')

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
    mailFolder.value = 'inbox'
    verificationMessages.value = []
    verificationCode.value = ''
    verificationSource.value = undefined
    lastVerificationRefreshAt.value = ''
    verificationStatusText.value = '正在查找最新验证码...'
    if (props.account) {
      void refreshVerificationMessages({ silent: true })
    }
  }
})

watch(mailFolder, () => {
  if (visible.value && props.account) {
    void refreshVerificationMessages({ silent: true })
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

async function refreshVerificationMessages(options: { silent?: boolean } = {}) {
  if (!props.account || verificationLoading.value) {
    return
  }
  verificationLoading.value = true
  verificationCode.value = ''
  verificationSource.value = undefined
  verificationStatusText.value = '正在查找最新验证码...'
  try {
    const result = await listImapMessages({
      credentials: { email: props.account.email },
      folder: mailFolder.value,
      limit: 12,
    })
    verificationMessages.value = [...(result.messages ?? [])].sort((left, right) =>
      right.receivedAt.localeCompare(left.receivedAt),
    )
    const recentMessages = verificationMessages.value.slice(0, 5)
    for (const message of recentMessages) {
      const codeFromSummary = extractVerificationCode(message.subject)
      if (codeFromSummary) {
        verificationCode.value = codeFromSummary
        verificationSource.value = message
        break
      }

      try {
        const detail = await getImapMessage({
          credentials: { email: props.account.email },
          folder: mailFolder.value,
          messageId: message.id,
        })
        const code = extractVerificationCode(`${detail.message.subject || message.subject}\n${detail.body.content || detail.message.content || ''}`)
        if (code) {
          verificationCode.value = code
          verificationSource.value = {
            ...message,
            subject: detail.message.subject || message.subject,
            receivedAt: detail.message.receivedAt || message.receivedAt,
            from: detail.message.from?.length ? detail.message.from : message.from,
          }
          break
        }
      } catch {
        // Try the next recent message; one unreadable email should not block binding.
      }
    }

    lastVerificationRefreshAt.value = new Date().toLocaleTimeString()
    verificationStatusText.value = verificationCode.value
      ? '已识别到最新验证码'
      : '未识别到验证码，可稍后刷新或切换垃圾箱查看'
    if (!options.silent) {
      ElMessage.success(verificationCode.value ? '已刷新并识别验证码' : '已刷新，未识别到验证码')
    }
  } catch (error) {
    verificationStatusText.value = '刷新验证码邮件失败'
    ElMessage.error(error instanceof Error ? error.message : '刷新验证码邮件失败')
  } finally {
    verificationLoading.value = false
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

function sourceText(message?: ImapMessageSummary): string {
  if (!message) {
    return '暂无来源邮件'
  }
  return `${senderText(message)} · ${message.subject || '无主题'} · ${formatShortTime(message.receivedAt)}`
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
            <span v-if="lastVerificationRefreshAt">上次刷新 {{ lastVerificationRefreshAt }}</span>
          </div>
          <el-button
            size="small"
            :icon="Refresh"
            :loading="verificationLoading"
            @click="refreshVerificationMessages"
          >
            刷新验证码
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
        <div v-loading="verificationLoading" class="gpt-verification-body">
          <div class="verification-copy-row">
            <el-button
              size="small"
              :icon="CopyDocument"
              :disabled="!verificationCode"
              @click="copyValue(verificationCode, '验证码')"
            >
              复制验证码
            </el-button>
          </div>
          <p v-if="verificationCode" class="verification-code">{{ verificationCode }}</p>
          <el-empty v-else :image-size="46" description="未识别到验证码" />
          <p class="verification-body-text">{{ verificationStatusText }}</p>
          <p class="verification-source">{{ sourceText(verificationSource) }}</p>
        </div>
        <div class="gpt-verification-list">
          <el-empty v-if="verificationMessages.length === 0" :image-size="56" description="暂无最近邮件" />
          <div
            v-for="message in verificationMessages.slice(0, 5)"
            v-else
            :key="message.id"
            class="gpt-verification-item"
            :class="{ active: verificationSource?.id === message.id }"
          >
            <span>{{ senderText(message) }}</span>
            <strong>{{ message.subject || '无主题' }}</strong>
            <small>{{ formatShortTime(message.receivedAt) }}</small>
          </div>
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
