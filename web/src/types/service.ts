// 共享类型定义：服务相关
// I-03-001 修复：消除 Services.tsx / ServiceOverview.tsx / StatusBar.tsx 中的重复定义

/** 服务状态（与后端 core.ServiceState 一致，7 种状态，禁止新增） */
export type ServiceState = 'pending' | 'starting' | 'up' | 'ready' | 'stopping' | 'down' | 'failed'

/** 服务列表项（GET /api/services 返回的服务摘要） */
export interface ServiceItem {
  name: string
  status: ServiceState
  uptime: number
  restart_count: number
  icon?: string
  tags?: string[]
  cpu_percent?: number
  memory_mb?: number
  ports?: Array<{ protocol: string; port: number; address: string; state: string; is_http: boolean }>
}

/** 服务列表响应（GET /api/services） */
export interface ServicesResponse {
  services: ServiceItem[]
}
