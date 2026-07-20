<script setup lang="ts">
import { Delete, EditPen, Files, FolderOpened, Message } from '@element-plus/icons-vue'
import type { MailAccount } from '@/types'
import type { MailGroup } from '@/services/accountApi'

type WorkspaceMode = 'accounts' | 'mail'

const DEFAULT_GROUP = '默认分组'

const props = defineProps<{
  remoteGroups: MailGroup[]
  selectedGroup: string
  workspaceMode: WorkspaceMode
  sidebarRootAccounts: MailAccount[]
  currentViewedAccountEmail?: string
  viewingEmail?: string
  accountCount: number
  groupCounts: ReadonlyMap<string, number>
  draggingGroupId?: number
  deletingGroupId?: number
  renamingGroupId?: number
}>()

const emit = defineEmits<{
  backToAccounts: []
  setGroup: [group: string]
  viewAccount: [account: MailAccount]
  groupDragStart: [group: MailGroup, event: DragEvent]
  groupDragOver: [event: DragEvent]
  groupDrop: [group: MailGroup, event: DragEvent]
  groupDragEnd: []
  renameGroup: [group: MailGroup]
  deleteGroup: [group: MailGroup]
}>()

function groupAccountCount(group: MailGroup): number {
  return props.groupCounts.get(group.name) ?? 0
}

function canDeleteGroup(group: MailGroup): boolean {
  return group.name !== DEFAULT_GROUP && groupAccountCount(group) === 0
}

function canRenameGroup(group: MailGroup): boolean {
  return group.name !== DEFAULT_GROUP
}
</script>

<template>
  <aside class="faka-sidebar">
    <div class="faka-brand">
      <el-icon><Message /></el-icon>
      <span>MailBox</span>
    </div>

    <nav class="faka-nav">
      <section class="sidebar-panel group-panel">
        <div class="sidebar-panel-head">
          <span>
            <el-icon><FolderOpened /></el-icon>
            分组
          </span>
          <strong>{{ remoteGroups.length }}</strong>
        </div>
        <div class="sidebar-panel-body group-panel-body">
          <button
            class="faka-nav-item sidebar-list-row pinned-row"
            :class="{ active: workspaceMode === 'accounts' }"
            type="button"
            @click="emit('backToAccounts')"
          >
            <el-icon><Files /></el-icon>
            <span>账号列表</span>
          </button>
          <button
            class="faka-nav-item sidebar-list-row pinned-row"
            :class="{ active: !selectedGroup }"
            type="button"
            @click="emit('setGroup', '')"
          >
            <el-icon><FolderOpened /></el-icon>
            <span>全部</span>
          </button>
          <button
            v-for="group in remoteGroups"
            :key="group.id"
            class="faka-nav-item sidebar-list-row group-nav-item"
            :class="{ active: selectedGroup === group.name, dragging: draggingGroupId === group.id }"
            type="button"
            draggable="true"
            @click="emit('setGroup', group.name)"
            @dragstart="emit('groupDragStart', group, $event)"
            @dragover="emit('groupDragOver', $event)"
            @drop="emit('groupDrop', group, $event)"
            @dragend="emit('groupDragEnd')"
          >
            <span class="drag-handle" aria-hidden="true">⋮⋮</span>
            <el-icon><FolderOpened /></el-icon>
            <span>{{ group.name }}</span>
            <div class="group-actions">
              <small>{{ groupAccountCount(group) }}</small>
              <el-button
                v-if="canRenameGroup(group)"
                class="group-action-button"
                link
                :icon="EditPen"
                :loading="renamingGroupId === group.id"
                :disabled="renamingGroupId === group.id"
                @click.stop="emit('renameGroup', group)"
              />
              <el-button
                v-if="canDeleteGroup(group)"
                class="group-action-button"
                link
                :icon="Delete"
                :loading="deletingGroupId === group.id"
                :disabled="deletingGroupId === group.id"
                @click.stop="emit('deleteGroup', group)"
              />
            </div>
          </button>
        </div>
      </section>

      <section class="sidebar-panel account-panel">
        <div class="sidebar-panel-head">
          <span>
            <el-icon><Message /></el-icon>
            账号快捷
          </span>
          <strong>{{ sidebarRootAccounts.length }}</strong>
        </div>
        <div class="sidebar-panel-body sidebar-account-list">
          <div
            v-for="account in sidebarRootAccounts"
            :key="account.email"
            class="sidebar-account-group"
          >
            <button
              class="faka-nav-item sidebar-list-row account-shortcut parent-shortcut"
              :class="{ active: currentViewedAccountEmail === account.email && workspaceMode === 'mail' }"
              type="button"
              :disabled="viewingEmail === account.email"
              @click="emit('viewAccount', account)"
            >
              <el-icon><Message /></el-icon>
              <span class="shortcut-email">{{ account.email }}</span>
            </button>
          </div>
        </div>
      </section>
    </nav>

    <div class="faka-total-card">
      <el-icon><Files /></el-icon>
      <span>邮箱账号</span>
      <strong>{{ accountCount }}</strong>
    </div>
  </aside>
</template>
