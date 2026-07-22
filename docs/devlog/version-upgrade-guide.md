# supd 版本升级指南

> 本文档记录 supd 的版本管理架构与升级流程。后续发版时只需按本指南执行，无需重新分析代码。

---

## 一、版本管理架构

supd 的版本号通过 **ldflags 编译时注入**，源码中不硬编码版本号。

### 注入链路

```
git tag v0.0.3
    │
    ├── CI（release.yml）从 tag 名提取版本 → -ldflags "-X main.version=v0.0.3"
    │       └── 同时注入 Dockerfile ARG VERSION=v0.0.3
    │
    ├── Makefile 通过 git describe 自动推导 → -ldflags "-X main.version=$(VERSION)"
    │
    └── 源码默认值 version = "dev"（未被 ldflags 覆盖时显示）
```

### 关键文件（无需修改）

| 文件 | 作用 | 默认值 | 是否需改 |
|------|------|--------|----------|
| `cmd/supd/main.go:14` | 版本变量定义 | `version = "dev"` | ❌ 永远不改 |
| `internal/cli/version.go:14` | CLI 包版本变量 | `Version = "dev"` | ❌ 永远不改 |
| `internal/cli/version.go:34` | `SetVersionInfo(v, bt)` | 由 main 调用注入 | ❌ 永远不改 |
| `Dockerfile:17` | Docker 构建 ARG | `ARG VERSION=dev` | ❌ 永远不改 |
| `Makefile:5` | 本地构建版本推导 | `git describe --tags` | ❌ 永远不改 |
| `.github/workflows/release.yml` | CI 版本提取 | 从 `${GITHUB_REF_NAME}` 提取 | ❌ 永远不改 |

### 唯一需手动修改的文件

| 文件 | 行 | 内容 | 说明 |
|------|----|------|------|
| `README.md` | ~246 | `docker pull ghcr.io/.../supd:vX.Y.Z` | Docker 镜像拉取示例 |
| `README.md` | ~305 | `| 当前版本 | \`vX.Y.Z\` |` | 项目状态表 |

> **测试文件 `internal/api/adapters_test.go` 中的 `"0.0.2-test"` 是测试夹具字符串（验证版本透传），不是版本引用，不需要修改。**

---

## 二、升级流程（每次发版执行）

### Step 1: 更新 README.md

```bash
# 将旧版本号替换为新版本号（仅 2 处）
# 例如 v0.0.2 → v0.0.3
sed -i 's/supd:v0\.0\.2/supd:v0.0.3/' README.md
sed -i 's/当前版本 | `v0\.0\.2`/当前版本 | `v0.0.3`/' README.md
```

或手动编辑 `README.md`：
1. `docker pull ghcr.io/qq859952722/supd:v0.0.3`（Docker 镜像示例）
2. `| 当前版本 | \`v0.0.3\` |`（项目状态表）

### Step 2: 验证构建

```bash
go build ./...
go vet ./...
go test ./... -count=1

# 验证版本注入（本地）
go build -ldflags "-X main.version=0.0.3" -o /tmp/supd-test ./cmd/supd/
/tmp/supd-test version
# 应输出: supd 0.0.3
rm /tmp/supd-test
```

### Step 3: 提交变更

```bash
git add README.md
git commit -m "release: vX.Y.Z — <简述本次变更>"
```

### Step 4: 创建并推送 tag（触发 CI 构建）

```bash
# 创建 annotated tag
git tag -a vX.Y.Z -m "supd vX.Y.Z"

# 推送到远程（自动触发 release.yml 工作流）
git push origin vX.Y.Z
```

### Step 5: 确认 CI 构建

```bash
# 查看 workflow 运行状态
gh run list --limit 3

# 或监控特定 run
gh run watch
```

CI 会自动完成：
1. 编译双架构 tjs（amd64 + arm64）
2. 构建双平台二进制（linux-amd64 + linux-arm64）
3. 构建双架构 Docker 镜像并推送到 GHCR
4. 创建多架构 manifest（`vX.Y.Z` + `latest`）
5. 生成 GitHub Release（含二进制 + checksums + 自动变更记录）

### Step 6: 更新开发日志

在 `docs/devlog/session-notes.md` 末尾追加版本升级记录。

---

## 三、预发布版本

预发布版本（如 `v0.0.3-beta`、`v1.0.0-rc.1`）流程相同：

```bash
git tag -a v0.0.3-beta -m "supd v0.0.3-beta"
git push origin v0.0.3-beta
```

CI 会自动识别预发布（tag 含 `-`），GitHub Release 标记为 prerelease，**不更新 `latest` Docker tag**。

---

## 四、版本号规范

遵循 [Semantic Versioning](https://semver.org/lang/zh-CN/)：

| 版本类型 | 格式 | 何时使用 | 示例 |
|----------|------|----------|------|
| 正式版 | `vMAJOR.MINOR.PATCH` | 正式发布 | `v0.0.3` |
| 预发布 | `vMAJOR.MINOR.PATCH-LABEL` | 测试/候选 | `v0.0.3-beta`、`v1.0.0-rc.1` |

- **PATCH**（0.0.X）：bug 修复、小改进，向后兼容
- **MINOR**（0.X.0）：新功能，向后兼容
- **MAJOR**（X.0.0）：破坏性变更

> 当前处于 0.x.x 阶段，任何变更都可能不向后兼容，但仍遵循 semver 格式。

---

## 五、常见问题

### Q: 为什么不在源码中写死版本号？
A: ldflags 注入是 Go 社区标准实践。源码默认值为 `"dev"`，CI/Docker/Makefile 通过 ldflags 覆盖。好处是：单一真相源（git tag），无需修改源码。

### Q: 忘记改 README 就推了 tag 怎么办？
A: README 版本号仅影响文档展示，不影响构建产物。可以在下个 commit 补改，或创建新 tag。

### Q: 如何本地验证版本注入？
A: `go build -ldflags "-X main.version=0.0.3" -o /tmp/supd ./cmd/supd/ && /tmp/supd version`

### Q: CI 构建失败怎么办？
A: `gh run view <run-id> --log-failed` 查看失败日志，修复后重新推送 tag（需先删除旧 tag）：
```bash
git tag -d vX.Y.Z                    # 删除本地 tag
git push origin :refs/tags/vX.Y.Z    # 删除远程 tag
# 修复后重新创建并推送
git tag -a vX.Y.Z -m "supd vX.Y.Z"
git push origin vX.Y.Z
```

---

## 六、变更记录

| 日期 | 版本 | 变更 |
|------|------|------|
| 2026-07-22 | v0.0.3 | tjs 运行时接入默认配置 + Docker 工具集 + auto-create-users 扩展 + user 字段接入进程启动修复 |
| 2026-07-21 | v0.0.2 | 端口迁移 7979、init 示例、script readiness 修复 |
| 2026-07-21 | v0.0.1 | 首次发布 |
