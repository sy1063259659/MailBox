<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { ElMessage } from 'element-plus'
import { UploadFilled } from '@element-plus/icons-vue'
import { ICLOUD_DEFAULT_GROUP, useICloudAccountStore } from '@/stores/iCloudAccount'
import { confirmAction } from '@/utils/confirm'

const visible = defineModel<boolean>({ required: true })
const store = useICloudAccountStore()
const text = ref('')
const selectedGroup = ref(ICLOUD_DEFAULT_GROUP)
const importing = ref(false)
const lastResult = ref<{ imported: number; updated: number; errors: string[] }>()
const fileInput = ref<HTMLInputElement>()

const detectedAccountCount = computed(() =>
  text.value.split(/\r?\n/).filter((line) => line.trim()).length,
)

watch(
  visible,
  (isVisible) => {
    if (isVisible) {
      selectedGroup.value = store.selectedGroup || ICLOUD_DEFAULT_GROUP
      lastResult.value = undefined
    }
  },
)

async function submit(overwrite: boolean) {
  if (overwrite) {
    const confirmed = await confirmAction(
      '覆盖导入只会清空现有 iCloud 账号，Outlook/Hotmail 账号和两套分组不受影响。',
      '覆盖导入 iCloud',
      {
        confirmButtonText: '覆盖导入',
        cancelButtonText: '取消',
        type: 'warning',
      },
    )
    if (!confirmed) {
      return
    }
  }

  importing.value = true
  try {
    const result = await store.importFromText(
      text.value,
      overwrite,
      selectedGroup.value.trim() || ICLOUD_DEFAULT_GROUP,
    )
    lastResult.value = result
    if (result.errors.length > 0) {
      ElMessage.warning(`导入 ${result.imported} 个，更新 ${result.updated} 个，失败 ${result.errors.length} 行`)
      return
    }

    ElMessage.success(`导入 ${result.imported} 个，更新 ${result.updated} 个`)
    text.value = ''
    visible.value = false
  } catch (error) {
    ElMessage.error(error instanceof Error ? error.message : '导入 iCloud 账号失败')
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
  if (file) {
    await readFile(file)
  }
  input.value = ''
}

async function handleDrop(event: DragEvent) {
  const file = event.dataTransfer?.files?.[0]
  if (file) {
    await readFile(file)
  }
}

async function readFile(file: File) {
  if (!file.name.toLowerCase().endsWith('.txt') && file.type !== 'text/plain') {
    ElMessage.warning('请导入 TXT 文本文件')
    return
  }
  text.value = await file.text()
  lastResult.value = undefined
  ElMessage.success(`已读取 ${detectedAccountCount.value} 行账号`)
}

async function copyErrors() {
  if (!lastResult.value?.errors.length) {
    return
  }
  try {
    await navigator.clipboard.writeText(lastResult.value.errors.join('\n'))
    ElMessage.success('已复制失败信息')
  } catch {
    ElMessage.error('复制失败信息失败')
  }
}
</script>

<template>
  <el-dialog v-model="visible" title="导入 iCloud 账号" width="720px" class="icloud-import-dialog">
    <div class="import-layout">
      <el-alert type="info" show-icon :closable="false" class="dialog-alert">
        <template #title>固定格式</template>
        <div>每行一个账号：邮箱----密钥。</div>
        <div>仅接受 @icloud.com；备注在导入后通过列表编辑。</div>
      </el-alert>

      <div class="format-box">
        <code>example-alias@icloud.com----example-key</code>
        <p>邮箱和密钥会清理首尾空格，邮箱统一转为小写。</p>
      </div>

      <div class="import-label">导入分组</div>
      <el-select
        v-model="selectedGroup"
        filterable
        allow-create
        default-first-option
        class="import-group-select"
        placeholder="选择或输入 iCloud 分组"
      >
        <el-option
          v-for="group in store.groups"
          :key="group.id"
          :label="group.name"
          :value="group.name"
        />
      </el-select>

      <button
        class="upload-drop"
        type="button"
        @click="openFilePicker"
        @dragover.prevent
        @drop.prevent="handleDrop"
      >
        <el-icon><UploadFilled /></el-icon>
        <span>拖拽 TXT 文件到此处 或 点击选择</span>
        <small>支持粘贴文本和 .txt 文件</small>
      </button>
      <input ref="fileInput" type="file" accept=".txt,text/plain" class="hidden-file" @change="handleFileChange" />
    </div>

    <div class="import-summary" :class="{ active: detectedAccountCount > 0 }">
      <strong>导入摘要</strong>
      <span>目标分组：{{ selectedGroup || ICLOUD_DEFAULT_GROUP }}；检测到 {{ detectedAccountCount }} 行账号</span>
      <small>覆盖导入只影响 iCloud 账号，重复导入会更新密钥并保留已有备注。</small>
    </div>

    <el-input
      v-model="text"
      type="textarea"
      :rows="12"
      resize="vertical"
      placeholder="example-alias@icloud.com----example-key"
    />

    <div v-if="lastResult" class="import-result">
      <el-tag type="success">新增 {{ lastResult.imported }}</el-tag>
      <el-tag>更新 {{ lastResult.updated }}</el-tag>
      <el-tag v-if="lastResult.errors.length" type="danger">失败 {{ lastResult.errors.length }}</el-tag>
      <el-button v-if="lastResult.errors.length" size="small" plain @click="copyErrors">复制失败信息</el-button>
    </div>
    <el-scrollbar v-if="lastResult?.errors.length" max-height="140px" class="error-list">
      <p v-for="error in lastResult.errors" :key="error">{{ error }}</p>
    </el-scrollbar>

    <template #footer>
      <el-button @click="visible = false">取消</el-button>
      <el-button :loading="importing" :disabled="!text.trim()" @click="submit(true)">覆盖导入</el-button>
      <el-button type="primary" :loading="importing" :disabled="!text.trim()" @click="submit(false)">追加导入</el-button>
    </template>
  </el-dialog>
</template>