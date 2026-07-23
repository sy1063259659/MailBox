import { defineStore } from 'pinia'
import {
  cancelICloudHMEJob,
  createICloudHMEAlias,
  createICloudHMEJob,
  createICloudHMESourceAccount,
  deleteICloudHMEAlias,
  deleteICloudHMEGroup,
  deleteICloudHMESource,
  listICloudHMEAliases,
  listICloudHMEGroups,
  listICloudHMEJobs,
  listICloudHMESourceAccounts,
  moveICloudHMEAliases,
  renameICloudHMEGroup,
  reorderICloudHMEGroups,
  retryICloudHMEJob,
  syncICloudHMEAliases,
  updateICloudHMERemark,
  type ICloudHMEAlias,
  type ICloudHMEGroup,
  type ICloudHMEJob,
  type ICloudHMESourceAccount,
} from '@/services/iCloudHmeApi'

export const ICLOUD_HME_DEFAULT_GROUP = '默认分组'

export const useICloudHmeStore = defineStore('iCloudHme', {
  state: () => ({
    sources: [] as ICloudHMESourceAccount[],
    aliases: [] as ICloudHMEAlias[],
    groups: [] as ICloudHMEGroup[],
    jobs: [] as ICloudHMEJob[],
    selectedGroup: '',
    loading: false,
  }),
  actions: {
    async load() {
      this.loading = true
      try {
        const [sources, aliases, groups, jobs] = await Promise.all([
          listICloudHMESourceAccounts(),
          listICloudHMEAliases(),
          listICloudHMEGroups(),
          listICloudHMEJobs(),
        ])
        this.sources = sources
        this.aliases = aliases
        this.groups = groups
        this.jobs = jobs
        if (this.selectedGroup && !groups.some((group) => group.name === this.selectedGroup)) this.selectedGroup = ''
      } finally {
        this.loading = false
      }
    },
    async loadJobs() {
      this.jobs = await listICloudHMEJobs()
    },
    async loadSources() {
      this.sources = await listICloudHMESourceAccounts()
    },
    async createJob(input: {
      mode: 'fixed' | 'pool'
      sourceAccountId?: number
      labelPrefix: string
      group: string
      count: number
    }) {
      const job = await createICloudHMEJob(input)
      await this.loadJobs()
      return job
    },
    async cancelJob(id: number) {
      await cancelICloudHMEJob(id)
      await this.loadJobs()
    },
    async retryJob(id: number) {
      await retryICloudHMEJob(id)
      await this.loadJobs()
    },
    async createSource(input: { name: string; appleIdEmail: string; icloudEmail: string; host: string }) {
      await createICloudHMESourceAccount(input)
      await this.load()
    },
    async deleteSource(id: number) {
      await deleteICloudHMESource(id)
      await this.load()
    },
    async createAlias(sourceId: number, label: string, group: string) {
      const email = await createICloudHMEAlias(sourceId, label, group || ICLOUD_HME_DEFAULT_GROUP)
      await this.load()
      return email
    },
    async syncAliases(sourceId: number) {
      const result = await syncICloudHMEAliases(sourceId)
      await this.load()
      return result
    },
    async updateRemark(email: string, remark: string) {
      const alias = await updateICloudHMERemark(email, remark)
      const index = this.aliases.findIndex((item) => item.email === alias.email)
      if (index >= 0) this.aliases[index] = alias
    },
    async moveToGroup(emails: string[], group: string) {
      await moveICloudHMEAliases(emails, group || ICLOUD_HME_DEFAULT_GROUP)
      await this.load()
    },
    async deleteAlias(email: string) {
      await deleteICloudHMEAlias(email)
      this.aliases = this.aliases.filter((alias) => alias.email !== email)
    },
    async reorderGroups(ids: number[]) {
      this.groups = await reorderICloudHMEGroups(ids)
    },
    async renameGroup(id: number, oldName: string, newName: string) {
      const group = await renameICloudHMEGroup(id, newName)
      await this.load()
      if (this.selectedGroup === oldName) this.selectedGroup = group.name
    },
    async deleteGroup(id: number, name: string) {
      await deleteICloudHMEGroup(id)
      this.groups = this.groups.filter((group) => group.id !== id)
      if (this.selectedGroup === name) this.selectedGroup = ''
    },
  },
})
