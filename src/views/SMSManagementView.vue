<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { ElMessage, ElMessageBox, type UploadFile } from 'element-plus'
import { Connection, CopyDocument, Delete, EditPen, Link, Message, Refresh, Upload } from '@element-plus/icons-vue'
import { getLatestSMS, type SMSAccount, type SMSLatestResult, type SMSMailboxType } from '@/services/smsApi'
import { useSMSStore } from '@/stores/sms'
import { formatDateTime } from '@/utils/dateTime'

const store = useSMSStore()
const keyword = ref('')
const bindingFilter = ref<'all' | 'bound' | 'unbound'>('all')
const page = ref(1)
const pageSize = ref(20)
const selectedRows = ref<SMSAccount[]>([])
const busy = ref(false)

const importVisible = ref(false)
const importText = ref('')
const overwriteImport = ref(false)
const importing = ref(false)

const bindingVisible = ref(false)
const bindingAccount = ref<SMSAccount>()
const bindingValue = ref('')
const bindingSaving = ref(false)

const smsVisible = ref(false)
const smsAccount = ref<SMSAccount>()
const smsResult = ref<SMSLatestResult>()
const smsLoading = ref(false)
const smsPolling = ref(false)
let smsTimer: number | undefined

const filteredAccounts = computed(() => {
  const query = keyword.value.trim().toLowerCase()
  return store.accounts.filter((account) => {
    if (bindingFilter.value === 'bound' && !account.linkedMailboxEmail) return false
    if (bindingFilter.value === 'unbound' && account.linkedMailboxEmail) return false
    return !query || [
      account.phone,
      account.providerHost,
      account.remark,
      account.linkedMailboxEmail,
    ].some((value) => value.toLowerCase().includes(query))
  })
})

const pagedAccounts = computed(() =>
  filteredAccounts.value.slice((page.value - 1) * pageSize.value, page.value * pageSize.value),
)
const boundCount = computed(() => store.accounts.filter((account) => account.linkedMailboxEmail).length)
const importLineCount = computed(() => importText.value.split(/\r?\n/).filter((line) => line.trim()).length)

watch([keyword, bindingFilter], () => {
  page.value = 1
  selectedRows.value = []
})
watch(() => filteredAccounts.value.length, (total) => {
  page.value = Math.min(page.value, Math.max(1, Math.ceil(total / pageSize.value)))
})
watch(smsVisible, (visible) => {
  if (!visible) stopPolling()
})

onMounted(async () => {
  try {
    await store.load()
  } catch (error) {
    showError(error, '加载接码账号失败')
  }
})
onBeforeUnmount(stopPolling)

function showError(error: unknown, fallback: string) {
  ElMessage.error(error instanceof Error ? error.message : fallback)
}

function handleSelection(rows: SMSAccount[]) {
  selectedRows.value = rows
}

function mailboxTypeText(type: SMSMailboxType | '') {
  if (!type) return ''
  return { outlook: 'Outlook', icloud: 'iCloud', icloud_hme: 'iCloud隐藏邮箱' }[type]
}

function openImport() {
  importText.value = ''
  overwriteImport.value = false
  store.importErrors = []
  importVisible.value = true
}

async function readImportFile(file: UploadFile) {
  if (!file.raw) return
  if (!file.name.toLowerCase().endsWith('.txt')) {
    ElMessage.warning('只支持 TXT 文件')
    return
  }
  try {
    importText.value = await file.raw.text()
    ElMessage.success(`已读取 ${importLineCount.value} 行`)
  } catch {
    ElMessage.error('读取 TXT 文件失败')
  }
}

async function submitImport() {
  if (!importText.value.trim()) return ElMessage.warning('请粘贴账号或拖入 TXT 文件')
  if (overwriteImport.value) {
    await ElMessageBox.confirm('覆盖导入会先清空全部接码账号及邮箱绑定，确定继续？', '覆盖导入', { type: 'warning' })
  }
  importing.value = true
  try {
    const result = await store.importFromText(importText.value, overwriteImport.value)
    if (!result.errors.length) importVisible.value = false
    ElMessage.success(`导入完成：新增 ${result.imported}，更新 ${result.updated}${result.errors.length ? `，失败 ${result.errors.length}` : ''}`)
  } catch (error) {
    showError(error, '导入接码账号失败')
  } finally {
    importing.value = false
  }
}

async function copyPhones() {
  const targets = selectedRows.value.length ? selectedRows.value : filteredAccounts.value
  if (!targets.length) return ElMessage.warning('没有可复制的手机号')
  await navigator.clipboard.writeText(targets.map((item) => item.phone).join('\n'))
  ElMessage.success(`已复制 ${targets.length} 个手机号`)
}

function openBinding(account?: SMSAccount) {
  const target = account ?? (selectedRows.value.length === 1 ? selectedRows.value[0] : undefined)
  if (!target) return ElMessage.warning('请选择一个接码账号')
  bindingAccount.value = target
  bindingValue.value = target.linkedMailboxType && target.linkedMailboxEmail
    ? `${target.linkedMailboxType}|${target.linkedMailboxEmail}`
    : ''
  bindingVisible.value = true
}

async function saveBinding() {
  const account = bindingAccount.value
  if (!account) return
  const separator = bindingValue.value.indexOf('|')
  const mailboxType = separator > 0 ? bindingValue.value.slice(0, separator) as SMSMailboxType : ''
  const email = separator > 0 ? bindingValue.value.slice(separator + 1) : ''
  bindingSaving.value = true
  try {
    await store.bindMailbox(account.phone, mailboxType, email)
    bindingVisible.value = false
    ElMessage.success(email ? '邮箱绑定已保存' : '已解除邮箱绑定')
  } catch (error) {
    showError(error, '保存邮箱绑定失败')
  } finally {
    bindingSaving.value = false
  }
}

async function editRemark(account: SMSAccount) {
  const result = await ElMessageBox.prompt('备注最多 500 个字符，留空表示清除。', `编辑备注 · ${account.phone}`, {
    inputValue: account.remark,
    inputType: 'textarea',
    inputValidator: (value) => Array.from(value.trim()).length <= 500 || '备注最多 500 个字符',
  })
  try {
    await store.updateRemark(account.phone, result.value)
    ElMessage.success('备注已保存')
  } catch (error) {
    showError(error, '保存备注失败')
  }
}

async function deleteSelected() {
  if (!selectedRows.value.length) return ElMessage.warning('请先勾选接码账号')
  await ElMessageBox.confirm(`确定删除 ${selectedRows.value.length} 个接码账号？`, '批量删除', { type: 'warning' })
  busy.value = true
  try {
    for (const account of selectedRows.value) await store.deleteAccount(account.phone)
    selectedRows.value = []
    ElMessage.success('接码账号已删除')
  } catch (error) {
    await store.load()
    showError(error, '删除接码账号失败')
  } finally {
    busy.value = false
  }
}

async function openSMS(account?: SMSAccount) {
  const target = account ?? (selectedRows.value.length === 1 ? selectedRows.value[0] : undefined)
  if (!target) return ElMessage.warning('请选择一个接码账号')
  smsAccount.value = target
  smsResult.value = undefined
  smsVisible.value = true
  startPolling()
}

async function refreshSMS() {
  if (!smsAccount.value || smsLoading.value) return
  smsLoading.value = true
  try {
    smsResult.value = await getLatestSMS(smsAccount.value.phone)
    if (smsResult.value.code) {
      stopPolling()
      ElMessage.success('已收到短信验证码')
    }
  } catch (error) {
    stopPolling()
    showError(error, '获取短信失败')
  } finally {
    smsLoading.value = false
  }
}

function startPolling() {
  stopPolling()
  smsPolling.value = true
  void refreshSMS()
  smsTimer = window.setInterval(refreshSMS, 5000)
}

function stopPolling() {
  if (smsTimer) window.clearInterval(smsTimer)
  smsTimer = undefined
  smsPolling.value = false
}

async function copyCode() {
  if (!smsResult.value?.code) return
  await navigator.clipboard.writeText(smsResult.value.code)
  ElMessage.success('验证码已复制')
}
</script>

<template>
  <section class="sms-management">
    <section class="faka-card sms-summary-card">
      <div class="sms-summary">
        <div><span>接码账号</span><strong>{{ store.accounts.length }}</strong></div>
        <div><span>已绑定邮箱</span><strong>{{ boundCount }}</strong></div>
        <div><span>待绑定</span><strong>{{ store.accounts.length - boundCount }}</strong></div>
      </div>
      <div class="faka-action-row sms-action-row">
        <el-button type="primary" :icon="Upload" @click="openImport">批量导入</el-button>
        <el-button :icon="CopyDocument" @click="copyPhones">复制手机号</el-button>
        <el-button :icon="Link" :disabled="selectedRows.length !== 1" @click="openBinding()">绑定邮箱</el-button>
        <el-button :icon="Message" :disabled="selectedRows.length !== 1" @click="openSMS()">实时收码</el-button>
        <el-button type="danger" plain :icon="Delete" :disabled="!selectedRows.length" @click="deleteSelected">删除</el-button>
      </div>
      <div class="sms-filter-row">
        <el-input v-model="keyword" clearable placeholder="搜索手机号、邮箱或备注" :prefix-icon="Connection" />
        <el-select v-model="bindingFilter">
          <el-option label="全部绑定状态" value="all" />
          <el-option label="已绑定邮箱" value="bound" />
          <el-option label="未绑定邮箱" value="unbound" />
        </el-select>
        <span>{{ filteredAccounts.length }} 个结果</span>
      </div>
      <div class="account-selection-hint" :class="{ active: selectedRows.length }">
        <strong>已选 {{ selectedRows.length }} 个接码账号</strong>
        <span>复制和删除优先作用于已选账号；未勾选时复制当前筛选结果</span>
      </div>

      <el-table
        v-loading="store.loading || busy"
        :data="pagedAccounts"
        row-key="id"
        class="faka-account-table"
        height="calc(100vh - 300px)"
        @selection-change="handleSelection"
      >
        <el-table-column type="selection" width="52" align="center" />
        <el-table-column label="#" width="64" align="center">
          <template #default="{ $index }"><span class="row-number">{{ (page - 1) * pageSize + $index + 1 }}</span></template>
        </el-table-column>
        <el-table-column prop="phone" label="手机号" min-width="160">
          <template #default="{ row }"><strong class="sms-phone">{{ row.phone }}</strong></template>
        </el-table-column>
        <el-table-column label="取件来源" min-width="155">
          <template #default="{ row }"><el-tag type="info" effect="plain">{{ row.providerHost }}</el-tag></template>
        </el-table-column>
        <el-table-column label="绑定邮箱" min-width="270" show-overflow-tooltip>
          <template #default="{ row }">
            <button class="sms-binding-cell" @click="openBinding(row)">
              <el-tag v-if="row.linkedMailboxEmail" size="small" type="success">{{ mailboxTypeText(row.linkedMailboxType) }}</el-tag>
              <span :class="{ muted: !row.linkedMailboxEmail }">{{ row.linkedMailboxEmail || '未绑定' }}</span>
            </button>
          </template>
        </el-table-column>
        <el-table-column label="备注" min-width="180" show-overflow-tooltip>
          <template #default="{ row }">
            <div class="remark-cell"><span :class="{ muted: !row.remark }">{{ row.remark || '无备注' }}</span><el-button link :icon="EditPen" @click.stop="editRemark(row)" /></div>
          </template>
        </el-table-column>
        <el-table-column label="最后取码" width="165">
          <template #default="{ row }">{{ row.lastCheckedAt ? formatDateTime(row.lastCheckedAt) : '尚未查询' }}</template>
        </el-table-column>
        <el-table-column label="导入时间" width="165">
          <template #default="{ row }">{{ formatDateTime(row.createdAt) }}</template>
        </el-table-column>
        <el-table-column label="操作" width="150" fixed="right" align="center">
          <template #default="{ row }">
            <el-button size="small" type="primary" :icon="Message" @click.stop="openSMS(row)">收码</el-button>
            <el-button size="small" :icon="Link" @click.stop="openBinding(row)" />
          </template>
        </el-table-column>
      </el-table>

      <div class="account-pagination">
        <el-pagination
          v-model:current-page="page"
          v-model:page-size="pageSize"
          :page-sizes="[20, 50, 100]"
          :total="filteredAccounts.length"
          layout="total, sizes, prev, pager, next"
        />
      </div>
    </section>

    <el-dialog v-model="importVisible" title="批量导入接码账号" width="680px">
      <el-upload drag :auto-upload="false" :show-file-list="false" accept=".txt,text/plain" :on-change="readImportFile">
        <el-icon class="el-icon--upload"><Upload /></el-icon>
        <div class="el-upload__text">拖入 TXT 文件，或点击选择文件</div>
      </el-upload>
      <el-input v-model="importText" type="textarea" :rows="10" resize="vertical" placeholder="+12025550123----http://qk.sms777.top/sms/msg/USA/xxxxxxxx" />
      <div class="sms-import-summary">
        <span>检测到 {{ importLineCount }} 行</span>
        <el-checkbox v-model="overwriteImport">覆盖现有接码账号</el-checkbox>
      </div>
      <el-alert v-if="store.importErrors.length" type="warning" :closable="false" show-icon>
        <template #title>有 {{ store.importErrors.length }} 行导入失败</template>
        <div class="sms-import-errors"><p v-for="error in store.importErrors" :key="error">{{ error }}</p></div>
      </el-alert>
      <template #footer>
        <el-button @click="importVisible = false">取消</el-button>
        <el-button type="primary" :loading="importing" @click="submitImport">一键批量导入</el-button>
      </template>
    </el-dialog>

    <el-dialog v-model="bindingVisible" title="绑定邮箱账号" width="560px">
      <el-form label-width="88px">
        <el-form-item label="手机号"><strong>{{ bindingAccount?.phone }}</strong></el-form-item>
        <el-form-item label="邮箱账号">
          <el-select v-model="bindingValue" filterable clearable placeholder="选择 Outlook、iCloud 或隐藏邮箱">
            <el-option
              v-for="mailbox in store.mailboxes"
              :key="`${mailbox.type}|${mailbox.email}`"
              :value="`${mailbox.type}|${mailbox.email}`"
              :label="`[${mailboxTypeText(mailbox.type)}] ${mailbox.email}`"
            />
          </el-select>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="bindingVisible = false">取消</el-button>
        <el-button type="primary" :loading="bindingSaving" @click="saveBinding">{{ bindingValue ? '保存绑定' : '解除绑定' }}</el-button>
      </template>
    </el-dialog>

    <el-dialog v-model="smsVisible" title="实时短信验证码" width="620px" class="sms-code-dialog">
      <div class="sms-code-head">
        <div><span>接码号码</span><strong>{{ smsAccount?.phone }}</strong></div>
        <el-tag :type="smsPolling ? 'success' : 'info'">{{ smsPolling ? '每 5 秒自动刷新' : '已停止自动刷新' }}</el-tag>
      </div>
      <div class="sms-code-result" :class="{ available: smsResult?.code }">
        <span>最新验证码</span>
        <strong>{{ smsResult?.code || '等待短信' }}</strong>
        <el-button v-if="smsResult?.code" type="primary" :icon="CopyDocument" @click="copyCode">复制验证码</el-button>
      </div>
      <div class="sms-message-panel">
        <span>接口返回</span>
        <p>{{ smsResult?.message || '正在查询最新短信…' }}</p>
        <small v-if="smsResult?.checkedAt">最后刷新 {{ formatDateTime(smsResult.checkedAt) }}</small>
      </div>
      <template #footer>
        <el-button v-if="smsPolling" @click="stopPolling">停止刷新</el-button>
        <el-button v-else :icon="Refresh" @click="startPolling">开始实时刷新</el-button>
        <el-button type="primary" :icon="Refresh" :loading="smsLoading" @click="refreshSMS">立即刷新</el-button>
      </template>
    </el-dialog>
  </section>
</template>
