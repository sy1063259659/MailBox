<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import {
  deletePaymentCard, forceReleaseIntegrationLease, importPaymentCards, listIntegrationLeases, listPaymentCards, resetPaymentCard,
  updatePaymentCardStatus, type IntegrationLease, type PaymentCard,
} from '@/services/paymentCardApi'
import { formatDateTime } from '@/utils/dateTime'

const cards = ref<PaymentCard[]>([])
const loading = ref(false)
const importing = ref(false)
const importText = ref('')
const leases = ref<IntegrationLease[]>([])

onMounted(load)

async function load() {
  loading.value = true
  try { [cards.value, leases.value] = await Promise.all([listPaymentCards(), listIntegrationLeases()]) }
  finally { loading.value = false }
}

async function releaseLease(lease: IntegrationLease) {
  await ElMessageBox.confirm(`确认释放队列 ${lease.queueId} 的 ${lease.type} 占用？运行中的任务可能因此失败。`, '手动释放任务占用', { type: 'warning' })
  await forceReleaseIntegrationLease(lease.id)
  await load()
  ElMessage.success('任务占用已释放')
}

async function importCards() {
  if (!importText.value.trim()) return ElMessage.warning('请输入支付卡')
  importing.value = true
  try {
    const result = await importPaymentCards(importText.value)
    importText.value = ''
    await load()
    ElMessage.success(`导入 ${result.cards.length} 张卡${result.errors.length ? `，${result.errors.length} 行未导入` : ''}`)
  } finally { importing.value = false }
}

async function toggle(card: PaymentCard) {
  await updatePaymentCardStatus(card.id, card.status === 'active' ? 'disabled' : 'active')
  await load()
}

async function reset(card: PaymentCard) {
  await ElMessageBox.confirm(`确认重置尾号 ${card.last4} 的支付卡？`, '重置支付卡')
  await resetPaymentCard(card.id)
  await load()
}

async function remove(card: PaymentCard) {
  await ElMessageBox.confirm(`确认删除尾号 ${card.last4} 的支付卡？`, '删除支付卡', { type: 'warning' })
  await deletePaymentCard(card.id)
  await load()
}

function statusText(status: PaymentCard['status']) {
  return status === 'active' ? '可用' : status === 'used' ? '已使用' : '已停用'
}
</script>

<template>
  <section class="card-workspace">
    <div class="card-import">
      <h2>导入支付卡</h2>
      <p>每行一张：卡号----MM/YY----CVC</p>
      <el-input v-model="importText" type="textarea" :rows="5" placeholder="4242424242424242----12/29----123" />
      <el-button type="primary" :loading="importing" @click="importCards">导入</el-button>
    </div>
    <div class="card-list">
      <header><h2>支付卡</h2><el-button @click="load">刷新</el-button></header>
      <el-table v-loading="loading" :data="cards" height="calc(100vh - 300px)">
        <el-table-column prop="numberMasked" label="卡号" min-width="190" />
        <el-table-column prop="expiry" label="有效期" width="100" />
        <el-table-column label="状态" width="110"><template #default="{ row }"><el-tag :type="row.status === 'active' ? 'success' : row.status === 'used' ? 'info' : 'danger'">{{ statusText(row.status) }}</el-tag></template></el-table-column>
        <el-table-column label="关联邮箱" min-width="260"><template #default="{ row }">{{ row.linkedEmails.join(', ') || '—' }}</template></el-table-column>
        <el-table-column label="任务占用" min-width="180"><template #default="{ row }"><span v-if="row.leaseOwner">{{ row.leaseOwner }} · {{ formatDateTime(row.leaseExpiresAt) }}</span><span v-else>—</span></template></el-table-column>
        <el-table-column label="失败原因" min-width="180"><template #default="{ row }">{{ row.failureReason || '—' }}</template></el-table-column>
        <el-table-column label="操作" width="230" fixed="right"><template #default="{ row }">
          <el-button link :disabled="!!row.leaseOwner || row.status === 'used'" @click="toggle(row)">{{ row.status === 'active' ? '停用' : '启用' }}</el-button>
          <el-button link :disabled="!!row.leaseOwner" @click="reset(row)">重置</el-button>
          <el-button link type="danger" :disabled="!!row.leaseOwner || row.linkedEmails.length > 0" @click="remove(row)">删除</el-button>
        </template></el-table-column>
      </el-table>
	  <header class="lease-header"><h2>任务资源占用</h2><span>停止任务默认保留 30 分钟，可在这里手动释放</span></header>
	  <el-table :data="leases" max-height="260">
		<el-table-column prop="type" label="类型" width="90" />
		<el-table-column prop="resource" label="资源" min-width="180" />
		<el-table-column prop="queueId" label="队列" min-width="190" />
		<el-table-column prop="state" label="状态" width="90" />
		<el-table-column label="到期" width="170"><template #default="{ row }">{{ formatDateTime(row.expiresAt) }}</template></el-table-column>
		<el-table-column label="操作" width="100"><template #default="{ row }"><el-button link type="danger" @click="releaseLease(row)">释放</el-button></template></el-table-column>
	  </el-table>
    </div>
  </section>
</template>

<style scoped>
.card-workspace{display:grid;grid-template-columns:minmax(280px,360px) minmax(0,1fr);gap:18px;padding:18px}.card-import,.card-list{background:var(--el-bg-color);border:1px solid var(--el-border-color-light);border-radius:12px;padding:18px}.card-import h2,.card-list h2{margin:0}.card-import p{color:var(--el-text-color-secondary);font-size:13px}.card-import .el-button{margin-top:12px;width:100%}.card-list header{display:flex;align-items:center;justify-content:space-between;margin-bottom:14px}.card-list .lease-header{margin-top:22px}.lease-header span{color:var(--el-text-color-secondary);font-size:13px}@media(max-width:900px){.card-workspace{grid-template-columns:1fr}.card-list .el-table{height:520px}}
</style>
