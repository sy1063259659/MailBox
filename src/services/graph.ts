import type {
  GraphMessageBody,
  GraphMessageSummary,
  MailFolder,
} from '@/types/mail'

const GRAPH_BASE_URL = 'https://graph.microsoft.com/v1.0'
const TOKEN_ENDPOINT =
  'https://login.microsoftonline.com/consumers/oauth2/v2.0/token'
const GRAPH_SCOPE = 'https://graph.microsoft.com/Mail.Read offline_access'
const DEFAULT_PAGE_SIZE = 50

const LIST_SELECT_FIELDS = [
  'id',
  'subject',
  'from',
  'toRecipients',
  'receivedDateTime',
  'bodyPreview',
  'isRead',
  'hasAttachments',
] as const

const DETAIL_SELECT_FIELDS = [
  'id',
  'subject',
  'body',
  'from',
  'toRecipients',
  'ccRecipients',
  'receivedDateTime',
  'hasAttachments',
] as const

export type GraphErrorCode =
  | 'token_expired'
  | 'rate_limited'
  | 'graph_error'
  | 'network_error'

export class GraphError extends Error {
  readonly code: GraphErrorCode
  readonly status?: number
  readonly details?: unknown
  readonly retryAfterSeconds?: number

  constructor(options: {
    code: GraphErrorCode
    message: string
    status?: number
    details?: unknown
    retryAfterSeconds?: number
  }) {
    super(options.message)
    this.name = 'GraphError'
    this.code = options.code
    this.status = options.status
    this.details = options.details
    this.retryAfterSeconds = options.retryAfterSeconds
  }
}

export interface RefreshTokenParams {
  clientId: string
  refreshToken: string
}

export interface RefreshTokenResult {
  accessToken: string
  newRefreshToken?: string
  expiresIn?: number
}

export interface ListMessagesParams {
  accessToken: string
  folder: MailFolder
  nextLink?: string
  top?: number
}

export interface ListMessagesResult {
  accessToken: string
  newRefreshToken?: string
  messages: GraphMessageSummary[]
  nextLink?: string
}

export interface GetMessageParams {
  accessToken: string
  id: string
}

type GraphRecipient = {
  emailAddress?: {
    name?: string
    address?: string
  }
}

type GraphUser = {
  emailAddress?: {
    name?: string
    address?: string
  }
}

type GraphBody = {
  contentType?: string
  content?: string
}

type GraphMessage = {
  id: string
  subject?: string
  from?: GraphUser
  toRecipients?: GraphRecipient[]
  ccRecipients?: GraphRecipient[]
  receivedDateTime?: string
  bodyPreview?: string
  body?: GraphBody
  isRead?: boolean
  hasAttachments?: boolean
}

type GraphListResponse = {
  value?: GraphMessage[]
  '@odata.nextLink'?: string
}

type TokenSuccessResponse = {
  access_token: string
  refresh_token?: string
  expires_in?: number
}

type TokenErrorResponse = {
  error?: string
  error_description?: string
}

type GraphErrorResponse = {
  error: {
    code?: string
    message: string
    innerError?: unknown
  }
}

export async function refreshGraphAccessToken(
  params: RefreshTokenParams,
): Promise<RefreshTokenResult> {
  const body = new URLSearchParams({
    client_id: params.clientId,
    grant_type: 'refresh_token',
    refresh_token: params.refreshToken,
    scope: GRAPH_SCOPE,
  })

  const response = await fetchWithNetworkError(TOKEN_ENDPOINT, {
    method: 'POST',
    headers: {
      'Content-Type': 'application/x-www-form-urlencoded',
    },
    body,
  })

  if (!response.ok) {
    throw await createGraphError(response)
  }

  const data = (await response.json()) as TokenSuccessResponse

  return {
    accessToken: data.access_token,
    newRefreshToken: data.refresh_token,
    expiresIn: data.expires_in,
  }
}

export async function listGraphMessages(
  params: ListMessagesParams,
): Promise<ListMessagesResult> {
  const url = params.nextLink ?? buildMessagesUrl(params.folder, params.top)
  const data = await graphFetch<GraphListResponse>(url, params.accessToken)

  return {
    accessToken: params.accessToken,
    messages: (data.value ?? []).map(toMailMessage),
    nextLink: data['@odata.nextLink'],
  }
}

export async function getGraphMessage(
  params: GetMessageParams,
): Promise<GraphMessageBody> {
  const encodedId = encodeURIComponent(params.id)
  const query = new URLSearchParams({
    $select: DETAIL_SELECT_FIELDS.join(','),
  })
  const data = await graphFetch<GraphMessage>(
    `${GRAPH_BASE_URL}/me/messages/${encodedId}?${query.toString()}`,
    params.accessToken,
  )

  return toMailMessageBody(data)
}

async function graphFetch<T>(url: string, accessToken: string): Promise<T> {
  const response = await fetchWithNetworkError(url, {
    headers: {
      Authorization: `Bearer ${accessToken}`,
    },
  })

  if (!response.ok) {
    throw await createGraphError(response)
  }

  return (await response.json()) as T
}

async function fetchWithNetworkError(
  input: RequestInfo | URL,
  init?: RequestInit,
): Promise<Response> {
  try {
    return await fetch(input, init)
  } catch (error) {
    throw new GraphError({
      code: 'network_error',
      message: 'Network request to Microsoft Graph failed.',
      details: error,
    })
  }
}

function buildMessagesUrl(folder: MailFolder, top = DEFAULT_PAGE_SIZE): string {
  const normalizedFolder = normalizeFolder(folder)
  const query = new URLSearchParams({
    $top: String(top),
    $select: LIST_SELECT_FIELDS.join(','),
  })

  return `${GRAPH_BASE_URL}/me/mailFolders/${normalizedFolder}/messages?${query.toString()}`
}

function normalizeFolder(folder: MailFolder): 'inbox' | 'junkemail' {
  const folderValue = String(folder).toLowerCase()

  if (folderValue === 'junk' || folderValue === 'junkemail') {
    return 'junkemail'
  }

  return 'inbox'
}

async function createGraphError(response: Response): Promise<GraphError> {
  const status = response.status
  const details = await readErrorBody(response)
  const message = getErrorMessage(details) ?? response.statusText

  if (status === 401) {
    return new GraphError({
      code: 'token_expired',
      message: message || 'Microsoft Graph access token expired.',
      status,
      details,
    })
  }

  if (status === 429) {
    return new GraphError({
      code: 'rate_limited',
      message: message || 'Microsoft Graph rate limit exceeded.',
      status,
      details,
      retryAfterSeconds: parseRetryAfter(response.headers.get('Retry-After')),
    })
  }

  return new GraphError({
    code: 'graph_error',
    message: message || 'Microsoft Graph request failed.',
    status,
    details,
  })
}

async function readErrorBody(response: Response): Promise<unknown> {
  const text = await response.text()

  if (!text) {
    return undefined
  }

  try {
    return JSON.parse(text) as GraphErrorResponse | TokenErrorResponse
  } catch {
    return text
  }
}

function getErrorMessage(details: unknown): string | undefined {
  const rawMessage = getRawErrorMessage(details)

  if (rawMessage?.includes('AADSTS90023')) {
    return [
      'AADSTS90023：当前 client_id 不允许浏览器前端跨域兑换 refresh token。',
      `请在 Microsoft Entra App Registration 中把应用平台配置为 Single-page application，并登记当前来源 ${window.location.origin}。`,
      '如果这个 refresh_token 来自 Web/服务端应用或未允许当前 Origin 的 Native 应用，纯前端无法使用它取件。',
    ].join(' ')
  }

  return rawMessage
}

function getRawErrorMessage(details: unknown): string | undefined {
  if (isGraphErrorResponse(details)) {
    return details.error.message
  }

  if (isTokenErrorResponse(details)) {
    return details.error_description ?? details.error
  }

  return typeof details === 'string' ? details : undefined
}

function isGraphErrorResponse(details: unknown): details is GraphErrorResponse {
  return (
    typeof details === 'object' &&
    details !== null &&
    'error' in details &&
    typeof details.error === 'object' &&
    details.error !== null &&
    'message' in details.error &&
    typeof details.error.message === 'string'
  )
}

function isTokenErrorResponse(details: unknown): details is TokenErrorResponse {
  return (
    typeof details === 'object' &&
    details !== null &&
    'error' in details &&
    typeof details.error === 'string'
  )
}

function parseRetryAfter(value: string | null): number | undefined {
  if (!value) {
    return undefined
  }

  const seconds = Number(value)
  return Number.isFinite(seconds) ? seconds : undefined
}

function toMailMessage(message: GraphMessage): GraphMessageSummary {
  return {
    id: message.id,
    subject: message.subject ?? '',
    from: toAddress(message.from),
    toRecipients: (message.toRecipients ?? []).map(toRecipientAddress),
    ccRecipients: (message.ccRecipients ?? []).map(toRecipientAddress),
    receivedDateTime: message.receivedDateTime ?? '',
    bodyPreview: message.bodyPreview ?? '',
    isRead: message.isRead ?? false,
    hasAttachments: message.hasAttachments ?? false,
  } satisfies GraphMessageSummary
}

function toMailBody(body: GraphBody | undefined): GraphMessageBody['body'] | undefined {
  if (!body) {
    return undefined
  }

  return {
    contentType: normalizeBodyType(body.contentType),
    content: body.content ?? '',
  } satisfies GraphMessageBody['body']
}

function normalizeBodyType(contentType: string | undefined): 'text' | 'html' {
  return contentType?.toLowerCase() === 'html' ? 'html' : 'text'
}

function toMailMessageBody(message: GraphMessage): GraphMessageBody {
  return {
    id: message.id,
    subject: message.subject ?? '',
    from: toAddress(message.from),
    toRecipients: (message.toRecipients ?? []).map(toRecipientAddress),
    ccRecipients: (message.ccRecipients ?? []).map(toRecipientAddress),
    receivedDateTime: message.receivedDateTime ?? '',
    body: toMailBody(message.body),
    hasAttachments: message.hasAttachments ?? false,
  } satisfies GraphMessageBody
}

function toRecipientAddress(recipient: GraphRecipient): { name?: string; email: string } {
  return {
    name: recipient.emailAddress?.name,
    email: recipient.emailAddress?.address ?? '',
  }
}

function toAddress(user: GraphUser | undefined): { name?: string; email: string } | undefined {
  if (!user?.emailAddress?.address) {
    return undefined
  }

  return {
    name: user.emailAddress.name,
    email: user.emailAddress.address,
  }
}
