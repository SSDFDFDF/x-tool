import { ref } from 'vue'

type JsonObject = Record<string, unknown>

const AUTH_STATUS_URL = '/admin/api/auth/status'
const AUTH_LOGIN_URL = '/admin/api/auth/login'
const AUTH_LOGOUT_URL = '/admin/api/auth/logout'
const AUTH_PASSWORD_URL = '/admin/api/auth/password'

const authState = {
  isAuthenticated: ref(false),
  isInitialized: ref(false),
  isCheckingStatus: ref(false)
}

let pendingStatusRequest: Promise<boolean> | null = null

const successStatuses = new Set(['ok', 'authenticated', 'logged_in', 'loggedin', 'success'])
const failureStatuses = new Set(['unauthorized', 'unauthenticated', 'logged_out', 'loggedout'])

const toJsonObject = (value: unknown): JsonObject | null => {
  if (!value || typeof value !== 'object' || Array.isArray(value)) {
    return null
  }
  return value as JsonObject
}

const parseResponseBody = async (response: Response): Promise<unknown> => {
  const contentType = response.headers.get('content-type') ?? ''

  if (contentType.includes('application/json')) {
    try {
      return await response.json()
    } catch {
      return null
    }
  }

  try {
    const text = await response.text()
    return text ? { message: text } : null
  } catch {
    return null
  }
}

const extractErrorMessage = (payload: unknown, fallback: string): string => {
  const data = toJsonObject(payload)
  if (!data) {
    return fallback
  }

  const error = toJsonObject(data.error)
  if (error && typeof error.message === 'string' && error.message.trim()) {
    return error.message
  }

  if (typeof data.message === 'string' && data.message.trim()) {
    return data.message
  }

  if (typeof data.error === 'string' && data.error.trim()) {
    return data.error
  }

  return fallback
}

const resolveAuthStatus = (payload: unknown): boolean | null => {
  const data = toJsonObject(payload)
  if (!data) {
    return null
  }

  const candidates = ['authenticated', 'is_authenticated', 'logged_in', 'ok']
  for (const key of candidates) {
    if (typeof data[key] === 'boolean') {
      return data[key] as boolean
    }
  }

  if (typeof data.status === 'string') {
    const normalized = data.status.trim().toLowerCase()
    if (successStatuses.has(normalized)) {
      return true
    }
    if (failureStatuses.has(normalized)) {
      return false
    }
  }

  return null
}

const setAuthenticated = (authenticated: boolean) => {
  authState.isAuthenticated.value = authenticated
  authState.isInitialized.value = true
}

export const markLoggedOut = () => {
  setAuthenticated(false)
}

export const ensureAuthStatusLoaded = async (force = false): Promise<boolean> => {
  if (!force && authState.isInitialized.value) {
    return authState.isAuthenticated.value
  }

  if (pendingStatusRequest) {
    return pendingStatusRequest
  }

  pendingStatusRequest = (async () => {
    authState.isCheckingStatus.value = true

    try {
      const response = await fetch(AUTH_STATUS_URL, {
        method: 'GET',
        credentials: 'include',
        headers: {
          Accept: 'application/json'
        }
      })

      const payload = await parseResponseBody(response)

      if (response.status === 401 || response.status === 403) {
        setAuthenticated(false)
        return false
      }

      if (!response.ok) {
        throw new Error(extractErrorMessage(payload, `登录状态检查失败 (HTTP ${response.status})`))
      }

      const authenticated = resolveAuthStatus(payload) ?? true
      setAuthenticated(authenticated)
      return authenticated
    } catch (error) {
      authState.isAuthenticated.value = false
      authState.isInitialized.value = true
      throw error
    } finally {
      authState.isCheckingStatus.value = false
      pendingStatusRequest = null
    }
  })()

  return pendingStatusRequest
}

export const loginWithPassword = async (password: string) => {
  const response = await fetch(AUTH_LOGIN_URL, {
    method: 'POST',
    credentials: 'include',
    headers: {
      'Content-Type': 'application/json',
      Accept: 'application/json'
    },
    body: JSON.stringify({ password })
  })

  const payload = await parseResponseBody(response)

  if (!response.ok) {
    throw new Error(extractErrorMessage(payload, `登录失败 (HTTP ${response.status})`))
  }

  setAuthenticated(true)
  return payload
}

export const logoutAdmin = async () => {
  let requestError: Error | null = null

  try {
    const response = await fetch(AUTH_LOGOUT_URL, {
      method: 'POST',
      credentials: 'include',
      headers: {
        Accept: 'application/json'
      }
    })

    const payload = await parseResponseBody(response)

    if (!response.ok && response.status !== 401 && response.status !== 403) {
      throw new Error(extractErrorMessage(payload, `登出失败 (HTTP ${response.status})`))
    }
  } catch (error) {
    requestError = error instanceof Error ? error : new Error(String(error))
  } finally {
    setAuthenticated(false)
  }

  if (requestError) {
    throw requestError
  }
}

export const updateAdminPassword = async (currentPassword: string, newPassword: string) => {
  const response = await fetch(AUTH_PASSWORD_URL, {
    method: 'POST',
    credentials: 'include',
    headers: {
      'Content-Type': 'application/json',
      Accept: 'application/json'
    },
    body: JSON.stringify({
      current_password: currentPassword,
      new_password: newPassword
    })
  })

  const payload = await parseResponseBody(response)

  if (!response.ok) {
    throw new Error(extractErrorMessage(payload, `修改 admin 口令失败 (HTTP ${response.status})`))
  }

  setAuthenticated(false)
  return payload
}

export { authState }
