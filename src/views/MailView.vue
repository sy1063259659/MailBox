<script setup lang="ts">
import { computed, watch } from 'vue'
import { ElMessage } from 'element-plus'
import { Refresh, Search } from '@element-plus/icons-vue'
import { useAccountStore } from '@/stores/account'
import { useMailStore } from '@/stores/mail'
import type { MailFolder, MailMessage } from '@/types'

const accountStore = useAccountStore()
const mailStore = useMailStore()

const accountOptions = computed(() =>
  accountStore.accounts
    .filter((account) => !mailStore.filter.group || account.group === mailStore.filter.group)
    .map((account) => ({ label: account.email, value: account.email })),
)

const selectedHtml = computed(() => {
  const content = mailStore.selectedBody?.content ?? ''
  if (!looksLikeHtml(content)) {
    return ''
  }

  return `
<!doctype html>
<html>
  <head>
    <meta charset="utf-8" />
    <base target="_blank" />
    <style>
      html, body { margin: 0; padding: 0; background: #fff; color: #18212f; font-family: Arial, sans-serif; line-height: 1.55; }
      body { padding: 12px; overflow-wrap: anywhere; }
      img { max-width: 100%; height: auto; }
      table { max-width: 100%; }
      a { color: #1d4ed8; }
    </style>
  </head>
  <body>${content}</body>
</html>`
})

function looksLikeHtml(value: string): boolean {
  return /<(?:!doctype|html|head|body|div|table|style|meta|title|p|br|span)\b/i.test(value)
}

watch(
  () => ({ ...mailStore.filter }),
  () => {
    void mailStore.loadMessages()
  },
  { deep: true },
)

function setFolder(folder: MailFolder) {
  mailStore.setFilter({ folder })
}

async function syncCurrent() {
  const results = await mailStore.syncSelectedAccounts([mailStore.filter.folder])
  const failed = results.filter((result) => result.error)
  const synced = results.reduce((sum, result) => sum + result.synced, 0)
  await accountStore.refreshStats()

  if (failed.length) {
    ElMessage.warning(`取件完成，新增/更新 ${synced} 封，失败 ${failed.length} 个任务`)
  } else {
    ElMessage.success(`取件完成，新增/更新 ${synced} 封`)
  }
}

async function selectMessage(message: MailMessage) {
  await mailStore.selectMessage(message)
}
</script>

<template>
  <section class="mail-layout">
    <aside class="mail-sidebar panel">
      <h2>筛选</h2>
      <el-form label-position="top">
        <el-form-item label="分组">
          <el-select
            v-model="mailStore.filter.group"
            clearable
            placeholder="全部分组"
            @change="mailStore.setFilter({ accountEmail: '' })"
          >
            <el-option v-for="group in accountStore.groups" :key="group" :label="group" :value="group" />
          </el-select>
        </el-form-item>
        <el-form-item label="账号">
          <el-select v-model="mailStore.filter.accountEmail" clearable filterable placeholder="全部账号">
            <el-option v-for="account in accountOptions" :key="account.value" :label="account.label" :value="account.value" />
          </el-select>
        </el-form-item>
        <el-form-item label="搜索">
          <el-input v-model="mailStore.filter.query" :prefix-icon="Search" placeholder="主题、发件人、收件人" />
        </el-form-item>
      </el-form>
      <el-segmented
        :model-value="mailStore.filter.folder"
        :options="[
          { label: '收件箱', value: 'inbox' },
          { label: '垃圾箱', value: 'junkemail' },
        ]"
        block
        @update:model-value="setFolder($event as MailFolder)"
      />
      <el-button
        class="wide-button"
        type="primary"
        :icon="Refresh"
        :loading="Object.values(mailStore.syncingAccounts).some(Boolean)"
        @click="syncCurrent"
      >
        取当前范围
      </el-button>
      <el-alert
        v-if="mailStore.errorMessage"
        :title="mailStore.errorMessage"
        description="请确认 Go 后端已启动，且账号 refresh_token 授权了 IMAP.AccessAsUser.All。"
        type="error"
        show-icon
        :closable="false"
      />
    </aside>

    <main class="message-list panel">
      <div class="panel-toolbar">
        <div>
          <h2>{{ mailStore.filter.folder === 'inbox' ? '收件箱' : '垃圾箱' }}</h2>
          <p>{{ mailStore.messages.length }} 封本地邮件</p>
        </div>
        <el-button :disabled="!mailStore.hasMore" @click="mailStore.loadMore()">加载更多</el-button>
      </div>

      <el-empty v-if="!mailStore.loading && mailStore.messages.length === 0" description="没有邮件数据" />
      <el-scrollbar v-else v-loading="mailStore.loading" height="calc(100vh - 265px)">
        <button
          v-for="message in mailStore.messages"
          :key="`${message.accountEmail}-${message.folder}-${message.messageId}`"
          class="message-row"
          :class="{ active: mailStore.selectedMessage?.messageId === message.messageId }"
          @click="selectMessage(message)"
        >
          <span class="message-subject">{{ message.subject || '无主题' }}</span>
          <span class="message-meta">{{ message.from?.email || '未知发件人' }}</span>
          <span class="message-preview">{{ message.preview }}</span>
          <span class="message-time">{{ message.receivedAt }}</span>
        </button>
      </el-scrollbar>
    </main>

    <aside class="message-detail panel">
      <el-empty v-if="!mailStore.selectedMessage" description="选择一封邮件查看详情" />
      <template v-else>
        <h2>{{ mailStore.selectedMessage.subject || '无主题' }}</h2>
        <div class="detail-meta">
          <p>发件人：{{ mailStore.selectedMessage.from?.email || '未知' }}</p>
          <p>账号：{{ mailStore.selectedMessage.accountEmail }}</p>
          <p>时间：{{ mailStore.selectedMessage.receivedAt }}</p>
        </div>
        <el-skeleton v-if="mailStore.bodyLoading" :rows="6" animated />
        <iframe
          v-else-if="selectedHtml"
          class="mail-body-frame"
          sandbox="allow-popups allow-popups-to-escape-sandbox"
          :srcdoc="selectedHtml"
          title="邮件正文"
        />
        <pre v-else class="mail-body plain">{{ mailStore.selectedBody?.content || mailStore.selectedMessage.preview }}</pre>
      </template>
    </aside>
  </section>
</template>
