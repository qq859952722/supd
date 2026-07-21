package config

// MergeEnv 将多个 EnvFile 按4层顺序合并
// REQ-F-015: 4层合并，后者覆盖前者
// 合并顺序：
//  1. 全局 env 文件（/etc/supd/env/*.yaml，按文件名字母序加载，后者覆盖前者）
//  2. 全局扩展私有 env（/etc/supd/extensions/<ext>/env.yaml）
//  3. 服务 env（/etc/supd/services/<svc>/env.yaml）
//  4. 服务级扩展私有 env（/etc/supd/services/<svc>/extensions/<ext>/env.yaml）
//
// 同名变量后者覆盖前者（包括 enabled 状态）
func MergeEnv(layers ...*EnvFile) map[string]EnvVar {
	merged := make(map[string]EnvVar)

	for _, layer := range layers {
		if layer == nil || layer.Env == nil {
			continue
		}
		for name, v := range layer.Env {
			merged[name] = v
		}
	}

	return merged
}

// ToInjectEnv 从合并结果生成要注入进程的环境变量
// enabled:false 的变量不注入
func ToInjectEnv(merged map[string]EnvVar) map[string]string {
	result := make(map[string]string, len(merged))

	for name, v := range merged {
		if v.IsEnabled() {
			result[name] = v.Value
		}
	}

	return result
}
