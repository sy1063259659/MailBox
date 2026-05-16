import { defineStore } from 'pinia'
import type { MailAccount, StorageStats } from '@/types'
import {
  deleteRemoteAccount,
  exportRemoteAccounts,
  importRemoteAccounts,
  listRemoteAccounts,
  listRemoteGroups,
  moveRemoteAccountsToGroup,
  splitRemoteHotmailAccount,
  updateRemoteAccountRemark,
  type MailGroup,
} from '@/services/accountApi'
import {
  clearLocalMailData,
  deleteLocalMailDataForAccount,
  getStats,
} from '@/services/storage'

const DEFAULT_GROUP = '默认分组'

export interface AccountImportResult {
  imported: number
  updated: number
  errors: string[]
}

export interface AccountGroupStat {
  group: string
  count: number
}

interface AccountState {
  accounts: MailAccount[]
  remoteGroups: MailGroup[]
  selectedGroup: string
  stats: StorageStats | undefined
  loading: boolean
  importErrors: string[]
}

export const useAccountStore = defineStore('account', {
  state: (): AccountState => ({
    accounts: [],
    remoteGroups: [],
    selectedGroup: '',
    stats: undefined,
    loading: false,
    importErrors: [],
  }),

  getters: {
    groups: (state): string[] =>
      state.remoteGroups.length > 0
        ? state.remoteGroups.map((group) => group.name)
        : Array.from(new Set(state.accounts.map((account) => account.group))).sort((left, right) =>
            left.localeCompare(right),
          ),

    groupStats: (state): AccountGroupStat[] => {
      const counts = state.accounts.reduce<Record<string, number>>((result, account) => {
        result[account.group] = (result[account.group] ?? 0) + 1
        return result
      }, {})

      return Object.entries(counts)
        .map(([group, count]) => ({ group, count }))
        .sort((left, right) => left.group.localeCompare(right.group))
    },

    filteredAccounts: (state): MailAccount[] =>
      state.selectedGroup
        ? state.accounts.filter((account) => account.group === state.selectedGroup)
        : state.accounts,

    accountTree: (state): MailAccount[] => buildAccountTree(state.accounts),
  },

  actions: {
    async loadAccounts(): Promise<void> {
      this.loading = true

      try {
        const [accounts, groups] = await Promise.all([listRemoteAccounts(), listRemoteGroups()])
        this.accounts = accounts.map(normalizeRemoteAccount)
        this.remoteGroups = groups
      } finally {
        this.loading = false
      }
    },

    async importAccountsFromText(text: string): Promise<AccountImportResult> {
      const result = await importRemoteAccounts(text, false)
      this.importErrors = result.errors
      await this.loadAccounts()
      await this.refreshStats()
      return result
    },

    async overwriteAccountsFromText(text: string): Promise<AccountImportResult> {
      const result = await importRemoteAccounts(text, true)
      await clearLocalMailData()
      this.importErrors = result.errors
      await this.loadAccounts()
      await this.refreshStats()
      return result
    },

    async moveAccountsToGroup(emails: string[], group: string): Promise<void> {
      const normalizedGroup = group.trim() || DEFAULT_GROUP
      await moveRemoteAccountsToGroup(emails, normalizedGroup)
      await this.loadAccounts()

      if (this.selectedGroup && !this.accounts.some((account) => account.group === this.selectedGroup)) {
        this.selectedGroup = normalizedGroup
      }
    },

    async updateAccountRemark(email: string, remark: string): Promise<void> {
      await updateRemoteAccountRemark(email, remark)
      await this.loadAccounts()
    },

    async deleteAccount(email: string): Promise<void> {
      const deletedEmails = await deleteRemoteAccount(email)
      const affectedEmails = deletedEmails.length > 0 ? deletedEmails : this.accounts
        .filter((account) => account.email === email || account.parentEmail === email)
        .map((account) => account.email)
      await Promise.all(affectedEmails.map((accountEmail) => deleteLocalMailDataForAccount(accountEmail)))
      this.accounts = this.accounts.filter((account) => !affectedEmails.includes(account.email))

      if (this.selectedGroup && !this.accounts.some((account) => account.group === this.selectedGroup)) {
        this.selectedGroup = ''
      }

      await this.refreshStats()
    },

    async splitHotmailAccount(email: string): Promise<void> {
      await splitRemoteHotmailAccount(email)
      await this.loadAccounts()
    },

    async clearLocalMailCache(): Promise<void> {
      await clearLocalMailData()
      this.stats = undefined
      this.importErrors = []
      await this.refreshStats()
    },

    async exportData(): Promise<string> {
      return exportRemoteAccounts()
    },

    async refreshStats(): Promise<void> {
      this.stats = await getStats()
    },

    setSelectedGroup(group: string): void {
      this.selectedGroup = group
    },
  },
})

function normalizeRemoteAccount(account: MailAccount): MailAccount {
  return {
    ...account,
    refreshToken: account.refreshToken ?? '',
    group: account.group || DEFAULT_GROUP,
    remark: account.remark ?? '',
    displayName: account.displayName || account.email,
    provider: account.provider ?? 'microsoft',
  }
}

function buildAccountTree(accounts: MailAccount[]): MailAccount[] {
  const childrenByParent = new Map<string, MailAccount[]>()
  const syntheticParentEmails = new Set<string>()

  for (const account of accounts) {
    if (!account.parentEmail) {
      continue
    }
    const children = childrenByParent.get(account.parentEmail) ?? []
    children.push({ ...account, children: undefined })
    childrenByParent.set(account.parentEmail, children)
    syntheticParentEmails.add(account.parentEmail)
  }

  const roots = accounts
    .filter((account) => !account.parentEmail)
    .map((account) => ({
      ...account,
      children: (childrenByParent.get(account.email) ?? []).sort(sortSplitChildren),
    }))

  for (const parentEmail of syntheticParentEmails) {
    if (roots.some((account) => account.email === parentEmail)) {
      continue
    }
    const children = (childrenByParent.get(parentEmail) ?? []).sort(sortSplitChildren)
    const firstChild = children[0]
    if (!firstChild) {
      continue
    }
    roots.push({
      ...firstChild,
      email: parentEmail,
      displayName: parentEmail,
      parentEmail: undefined,
      splitIndex: undefined,
      splitGeneratedAt: undefined,
      children,
    })
  }

  return roots.sort((left, right) => right.createdAt.localeCompare(left.createdAt) || left.email.localeCompare(right.email))
}

function sortSplitChildren(left: MailAccount, right: MailAccount): number {
  return (left.splitIndex ?? 0) - (right.splitIndex ?? 0) || left.email.localeCompare(right.email)
}
