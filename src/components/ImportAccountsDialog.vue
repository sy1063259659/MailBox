<script setup lang="ts">
import { ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { UploadFilled } from '@element-plus/icons-vue'
import { useAccountStore } from '@/stores/account'
import { useMailStore } from '@/stores/mail'

const visible = defineModel<boolean>({ required: true })
const accountStore = useAccountStore()
const mailStore = useMailStore()
const text = ref('')
const importing = ref(false)
const lastResult = ref<{ imported: number; updated: number; errors: string[] }>()
const fileInput = ref<HTMLInputElement>()

async function submit(mode: 'append' | 'overwrite') {
  if (mode === 'overwrite') {
    await ElMessageBox.confirm('覆盖导入会先清空数据库中的邮箱账号，并清空本地邮件缓存，再导入当前内容。', '覆盖导入', {
      confirmButtonText: '覆盖导入',
      cancelButtonText: '取消',
      type: 'warning',
    })
  }

  importing.value = true
  try {
    const result = mode === 'overwrite'
      ? await accountStore.overwriteAccountsFromText(text.value)
      : await accountStore.importAccountsFromText(text.value)
    lastResult.value = result
    await mailStore.loadMessages()

    if (result.errors.length > 0) {
      ElMessage.warning(`导入 ${result.imported} 个，更新 ${result.updated} 个，失败 ${result.errors.length} 行`)
    } else {
      ElMessage.success(`导入 ${result.imported} 个，更新 ${result.updated} 个`)
      text.value = ''
      visible.value = false
    }
  } finally {
    importing.value = false
  }
}

function openFilePicker() {
  fileInput.value?.click()
}

async function handleFileChange(event: Event) {
  const input = event.target as HTMLInputElement
  const file = input.files?.[0]
  if (!file) {
    return
  }
  text.value = await file.text()
  input.value = ''
}

async function readDroppedFile(event: DragEvent) {
  const file = event.dataTransfer?.files?.[0]
  if (!file) {
    return
  }
  text.value = await file.text()
}
</script>

<template>
  <el-dialog v-model="visible" title="导入邮箱账号" width="760px" class="faka-import-dialog">
    <div class="import-layout">
      <el-alert type="info" show-icon :closable="false" class="dialog-alert">
        <template #title>导入提示</template>
        <div>每行一个账号；支持空格分隔或 "----" 分隔。</div>
        <div>格式不正确的行会被忽略；分组默认写入 "默认分组"。</div>
        <div>Hotmail/Outlook 加号别名会自动使用主邮箱取信。</div>
        <div>覆盖导入 会先清空数据库账号，并清空浏览器邮件缓存。</div>
      </el-alert>

      <div class="format-box">
        <strong>支持格式</strong>
        <code># Outlook/其他邮箱（使用OAuth2）</code>
        <code>邮箱 密码 ClientID 刷新令牌</code>
        <code>邮箱----密码----ClientID----刷新令牌</code>
        <p>当前版本使用本地 Go 后端通过 IMAP XOAUTH2 拉取 Outlook/Hotmail 邮件。</p>
      </div>

      <div class="import-label">账号内容</div>
      <button
        class="upload-drop"
        type="button"
        @click="openFilePicker"
        @dragover.prevent
        @drop.prevent="readDroppedFile"
      >
        <el-icon><UploadFilled /></el-icon>
        <span>拖拽 TXT 文件到此处 或 点击选择</span>
        <small>支持 .txt 格式</small>
      </button>
      <input ref="fileInput" type="file" accept=".txt,text/plain" class="hidden-file" @change="handleFileChange" />
    </div>

    <el-input
      v-model="text"
      type="textarea"
      :rows="12"
      resize="vertical"
      placeholder="在此处粘贴账号数据（每行一个）..."
    />

    <div v-if="lastResult" class="import-result">
      <el-tag type="success">新增 {{ lastResult.imported }}</el-tag>
      <el-tag>更新 {{ lastResult.updated }}</el-tag>
      <el-tag v-if="lastResult.errors.length" type="danger">失败 {{ lastResult.errors.length }}</el-tag>
    </div>
    <el-scrollbar v-if="lastResult?.errors.length" max-height="140px" class="error-list">
      <p v-for="error in lastResult.errors" :key="error">{{ error }}</p>
    </el-scrollbar>

    <template #footer>
      <el-button @click="visible = false">取消</el-button>
      <el-button :loading="importing" :disabled="!text.trim()" @click="submit('overwrite')">
        覆盖导入
      </el-button>
      <el-button type="primary" :loading="importing" :disabled="!text.trim()" @click="submit('append')">
        追加导入
      </el-button>
    </template>
  </el-dialog>
</template>
