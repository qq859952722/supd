// REQ-U-012: 认证状态管理 (zustand)
// token存储、登录/登出、localStorage持久化

import { create } from 'zustand'

const TOKEN_KEY = 'supd_auth_token'

interface AuthState {
  token: string | null
  isAuthenticated: boolean
  showLoginDialog: boolean
  login: (token: string) => void
  logout: () => void
  setShowLoginDialog: (show: boolean) => void
}

function loadToken(): string | null {
  try {
    return localStorage.getItem(TOKEN_KEY)
  } catch {
    return null
  }
}

function saveToken(token: string | null) {
  try {
    if (token) {
      localStorage.setItem(TOKEN_KEY, token)
    } else {
      localStorage.removeItem(TOKEN_KEY)
    }
  } catch {
    // localStorage不可用时静默失败
  }
}

export const useAuthStore = create<AuthState>((set) => {
  const initialToken = loadToken()
  return {
    token: initialToken,
    isAuthenticated: initialToken !== null,
    showLoginDialog: false,
    login: (token: string) => {
      saveToken(token)
      set({ token, isAuthenticated: true, showLoginDialog: false })
    },
    logout: () => {
      saveToken(null)
      set({ token: null, isAuthenticated: false, showLoginDialog: true })
    },
    setShowLoginDialog: (show: boolean) => set({ showLoginDialog: show }),
  }
})
