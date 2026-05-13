const API_BASE_URL = import.meta.env.VITE_API_BASE_URL || '/api'
const REQUEST_TIMEOUT_MS = 90_000

export class ApiError extends Error {
  readonly code: string
  readonly status?: number

  constructor(code: string, message: string, status?: number) {
    super(message)
    this.name = 'ApiError'
    this.code = code
    this.status = status
  }
}

interface ErrorResponse {
  ok: false
  error: string
  message?: string
}

export async function apiGet<T>(path: string): Promise<T> {
  return apiRequest<T>(path, { method: 'GET' })
}

export async function apiPost<T>(path: string, body?: unknown): Promise<T> {
  return apiRequest<T>(path, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: body === undefined ? undefined : JSON.stringify(body),
  })
}

export async function apiPatch<T>(path: string, body?: unknown): Promise<T> {
  return apiRequest<T>(path, {
    method: 'PATCH',
    headers: { 'Content-Type': 'application/json' },
    body: body === undefined ? undefined : JSON.stringify(body),
  })
}

export async function apiDelete<T>(path: string): Promise<T> {
  return apiRequest<T>(path, { method: 'DELETE' })
}

export async function apiRequest<T>(path: string, init: RequestInit): Promise<T> {
  let response: Response
  const controller = new AbortController()
  const timeoutId = window.setTimeout(() => controller.abort(), REQUEST_TIMEOUT_MS)

  try {
    response = await fetch(`${API_BASE_URL}${path}`, {
      ...init,
      credentials: 'include',
      signal: controller.signal,
    })
  } catch (error) {
    if (error instanceof DOMException && error.name === 'AbortError') {
      throw new ApiError('timeout', '请求超时，请稍后重试。')
    }
    throw new ApiError('network_error', '无法连接后端服务，请确认服务已启动。')
  } finally {
    window.clearTimeout(timeoutId)
  }

  const data = (await response.json().catch(() => undefined)) as T | ErrorResponse | undefined
  if (!response.ok) {
    const error = data as ErrorResponse | undefined
    if (response.status === 401) {
      window.dispatchEvent(new CustomEvent('mailbox:unauthorized'))
    }
    throw new ApiError(error?.error ?? 'internal_error', error?.message ?? response.statusText, response.status)
  }

  return data as T
}
