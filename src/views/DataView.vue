<script setup lang="ts">
import { computed, onMounted } from 'vue'
import { useAccountStore } from '@/stores/account'

const accountStore = useAccountStore()

onMounted(() => {
  void accountStore.refreshStats()
})

const stats = computed(() => accountStore.stats)
</script>

<template>
  <section class="panel">
    <div class="panel-toolbar">
      <div>
        <h2>本地数据</h2>
        <p>IndexedDB 中保存的账号、邮件和同步状态统计</p>
      </div>
      <el-button @click="accountStore.refreshStats()">刷新统计</el-button>
    </div>

    <el-row :gutter="16">
      <el-col :span="6">
        <div class="stat-card"><span>账号</span><strong>{{ stats?.accountCount ?? 0 }}</strong></div>
      </el-col>
      <el-col :span="6">
        <div class="stat-card"><span>邮件</span><strong>{{ stats?.messageCount ?? 0 }}</strong></div>
      </el-col>
      <el-col :span="6">
        <div class="stat-card"><span>正文缓存</span><strong>{{ stats?.messageBodyCount ?? 0 }}</strong></div>
      </el-col>
      <el-col :span="6">
        <div class="stat-card"><span>同步状态</span><strong>{{ stats?.syncStateCount ?? 0 }}</strong></div>
      </el-col>
    </el-row>

    <el-divider />
    <el-descriptions :column="2" border>
      <el-descriptions-item label="收件箱邮件">{{ stats?.messagesByFolder.inbox ?? 0 }}</el-descriptions-item>
      <el-descriptions-item label="垃圾箱邮件">{{ stats?.messagesByFolder.junkemail ?? 0 }}</el-descriptions-item>
    </el-descriptions>
  </section>
</template>
