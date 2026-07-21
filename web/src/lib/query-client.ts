// REQ-U-012: tanstack-react-query 配置

import { QueryClient } from '@tanstack/react-query'
import { ApiException } from '@/lib/api-client'

export function createQueryClient(): QueryClient {
  return new QueryClient({
    defaultOptions: {
      queries: {
        retry: (failureCount, error) => {
          // 401不重试
          if (error instanceof ApiException && error.status === 401) {
            return false
          }
          return failureCount < 3
        },
        staleTime: 10_000,
        refetchOnWindowFocus: false,
        // G-03-001: 标签页隐藏时暂停 refetchInterval 轮询（tanstack-query v5 默认行为，显式声明以明确意图）
        refetchIntervalInBackground: false,
      },
      mutations: {
        retry: false,
      },
    },
  })
}
