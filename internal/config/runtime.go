package config

// REQ-F-028: 三层运行时来源（config > scan > builtin）
// REQ-F-029: 运行时可用性校验
// REQ-F-030: 运行时路径不注入 PATH
// REQ-C-010: 不引入 interface 抽象层

// RuntimeSource 运行时来源类型。
// REQ-F-028: 三层来源 — builtin/config/scan
type RuntimeSource string

const (
	// RuntimeSourceBuiltin 内置默认运行时
	RuntimeSourceBuiltin RuntimeSource = "builtin"
	// RuntimeSourceConfig config.yaml 声明的运行时
	RuntimeSourceConfig RuntimeSource = "config"
	// RuntimeSourceScan runtimes/ 目录扫描发现的运行时
	RuntimeSourceScan RuntimeSource = "scan"
)

// RuntimeEntry 运行时条目。
// REQ-F-028: 每个运行时包含别名、路径、来源、可用性
// REQ-F-029: 可用性标记
type RuntimeEntry struct {
	Alias     string        // 运行时别名
	Path      string        // 可执行文件路径（绝对路径或 PATH 查找示例名）
	Source    RuntimeSource // 来源
	Available bool          // 是否可用（路径存在 + 可执行）
	AbsPath   string        // 解析后的绝对路径（PATH 查找后为完整路径）
}

// RuntimeRegistry 运行时注册表。
// REQ-F-028: 维护三层来源的运行时映射，同名高优先级覆盖低优先级
type RuntimeRegistry struct {
	entries map[string]*RuntimeEntry // key=alias
}

// NewRuntimeRegistry 创建空的运行时注册表。
// REQ-F-028: 注册表初始为空，需通过 Register* 方法填充
func NewRuntimeRegistry() *RuntimeRegistry {
	return &RuntimeRegistry{
		entries: make(map[string]*RuntimeEntry),
	}
}
