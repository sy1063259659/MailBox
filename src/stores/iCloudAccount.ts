import { defineStore } from 'pinia'
import {
  deleteICloudAccount,
  deleteICloudGroup,
  importICloudAccounts,
  listICloudAccounts,
  listICloudGroups,
  moveICloudAccounts,
  renameICloudGroup,
  reorderICloudGroups,
  updateICloudRemark,
  type ICloudAccount,
  type ICloudGroup,
  type ICloudImportResult,
} from '@/services/iCloudAccountApi'

export const ICLOUD_DEFAULT_GROUP = '默认分组'

interface ICloudAccountState {
  accounts: ICloudAccount[]
  groups: ICloudGroup[]
  selectedGroup: string
  loading: boolean
  importErrors: string[]
}

export const useICloudAccountStore = defineStore('iCloudAccount', {
  state: (): ICloudAccountState => ({
    accounts: [],
    groups: [],
    selectedGroup: '',
    loading: false,
    importErrors: [],
  }),

  actions: {
    async load(): Promise<void> {
      this.loading = true
      try {
        const [accounts, groups] = await Promise.all([listICloudAccounts(), listICloudGroups()])
        this.accounts = accounts
        this.groups = groups
        if (this.selectedGroup && !groups.some((group) => group.name === this.selectedGroup)) {
          this.selectedGroup = ''
        }
      } finally {
        this.loading = false
      }
    },

    async importFromText(text: string, overwrite: boolean, group: string): Promise<ICloudImportResult> {
      const result = await importICloudAccounts(text, overwrite, group.trim() || ICLOUD_DEFAULT_GROUP)
      this.importErrors = result.errors
      await this.load()
      return result
    },

    async updateRemark(email: string, remark: string): Promise<void> {
      const account = await updateICloudRemark(email, remark)
      const index = this.accounts.findIndex((item) => item.email === account.email)
      if (index >= 0) {
        this.accounts[index] = account
      }
    },

    async moveToGroup(emails: string[], group: string): Promise<void> {
      const normalizedGroup = group.trim() || ICLOUD_DEFAULT_GROUP
      await moveICloudAccounts(emails, normalizedGroup)
      await this.load()
    },

    async deleteAccount(email: string): Promise<void> {
      await deleteICloudAccount(email)
      this.accounts = this.accounts.filter((account) => account.email !== email)
    },

    async reorderGroups(ids: number[]): Promise<void> {
      this.groups = await reorderICloudGroups(ids)
    },

    async renameGroup(id: number, oldName: string, newName: string): Promise<void> {
      const group = await renameICloudGroup(id, newName.trim())
      await this.load()
      if (this.selectedGroup === oldName) {
        this.selectedGroup = group.name
      }
    },

    async deleteGroup(id: number, name: string): Promise<void> {
      await deleteICloudGroup(id)
      this.groups = this.groups.filter((group) => group.id !== id)
      if (this.selectedGroup === name) {
        this.selectedGroup = ''
      }
    },

    setSelectedGroup(group: string): void {
      this.selectedGroup = group
    },
  },
})