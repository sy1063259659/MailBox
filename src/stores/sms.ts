import { defineStore } from 'pinia'
import {
  bindSMSMailbox,
  deleteSMSAccount,
  importSMSAccounts,
  listSMSAccounts,
  listSMSMailboxes,
  updateSMSRemark,
  type SMSAccount,
  type SMSImportResult,
  type SMSMailboxReference,
  type SMSMailboxType,
} from '@/services/smsApi'

interface SMSState {
  accounts: SMSAccount[]
  mailboxes: SMSMailboxReference[]
  loading: boolean
  importErrors: string[]
}

export const useSMSStore = defineStore('sms', {
  state: (): SMSState => ({
    accounts: [],
    mailboxes: [],
    loading: false,
    importErrors: [],
  }),

  actions: {
    async load(): Promise<void> {
      this.loading = true
      try {
        const [accounts, mailboxes] = await Promise.all([listSMSAccounts(), listSMSMailboxes()])
        this.accounts = accounts
        this.mailboxes = mailboxes
      } finally {
        this.loading = false
      }
    },

    async importFromText(text: string, overwrite: boolean): Promise<SMSImportResult> {
      const result = await importSMSAccounts(text, overwrite)
      this.importErrors = result.errors
      await this.load()
      return result
    },

    replaceAccount(account: SMSAccount): void {
      const index = this.accounts.findIndex((item) => item.phone === account.phone)
      if (index >= 0) this.accounts[index] = account
    },

    async updateRemark(phone: string, remark: string): Promise<void> {
      this.replaceAccount(await updateSMSRemark(phone, remark))
    },

    async bindMailbox(phone: string, mailboxType: SMSMailboxType | '', email: string): Promise<void> {
      this.replaceAccount(await bindSMSMailbox(phone, mailboxType, email))
    },

    async deleteAccount(phone: string): Promise<void> {
      await deleteSMSAccount(phone)
      this.accounts = this.accounts.filter((account) => account.phone !== phone)
    },
  },
})
