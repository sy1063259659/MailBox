<script setup lang="ts">
import AccountPanel from '../components/AccountPanel.vue'
import type { MailAccount } from '../types'

defineProps<{
  accounts: MailAccount[]
  groups: string[]
  selectedGroup: string
  loading: boolean
}>()

const emit = defineEmits<{
  'update:selectedGroup': [value: string]
  deleteAccount: [email: string]
  refresh: []
}>()
</script>

<template>
  <div class="page-grid accounts-page">
    <AccountPanel
      :accounts="accounts"
      :groups="groups"
      :selected-group="selectedGroup"
      :loading="loading"
      @update:selected-group="emit('update:selectedGroup', $event)"
      @delete-account="emit('deleteAccount', $event)"
      @refresh="emit('refresh')"
    />
  </div>
</template>
