// 共享类型定义：扩展相关
// I-03-001 修复：抽取 CronTasks.tsx 中的 ExtSummary / RunResult 共享类型

/** 扩展摘要（GET /api/extensions 返回数组元素） */
export interface ExtSummary {
  name: string
  last_run_at?: string
  last_status?: string
}

/** 扩展运行结果（POST /run 和 GET /runs/{id} 返回） */
export interface RunResult {
  run_id: string
  extension_name: string
  action_id: string
  state: string
  exit_code: number
  progress: number
  result_msg: string
  result_level: string
  started_at: string
  finished_at: string
  trigger_type: string
  service_name: string
}
