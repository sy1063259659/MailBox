<script setup lang="ts">
import AccountWorkspaceView from './AccountWorkspaceView.vue'

const emit = defineEmits<{
  importAccounts: []
  exportData: []
  clearData: []
}>()
</script>

<template>
  <AccountWorkspaceView
    @import-accounts="emit('importAccounts')"
    @export-data="emit('exportData')"
    @clear-data="emit('clearData')"
  />
</template>
