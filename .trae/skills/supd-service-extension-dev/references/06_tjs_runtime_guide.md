# tjs 运行时扩展开发指南

supd 内置 [txiki.js](https://txiki.js.org/)（简称 tjs）作为 JavaScript 运行时，可用于编写扩展脚本。本文档基于 txiki.js v26.6.0 实际探测整理。

> **何时使用本指南**：当扩展的 `meta.yaml` 中 `runtime: tjs` 时，开发 `run.js` 入口脚本必须参考本指南。tjs 不是 Node.js，API 与 Node 有显著差异。

---

## 1. tjs 运行时概述

| 项目 | 说明 |
|---|---|
| 运行时名称 | `tjs`（`meta.yaml` 中 `runtime: tjs`） |
| 二进制路径 | `/usr/local/bin/tjs`（包装脚本）→ `/usr/local/bin/tjs-bin` |
| 版本 | v26.6.0（由 `.github/workflows/release.yml` 的 `TJS_VERSION` 控制） |
| JavaScript 引擎 | QuickJS（支持 ES2024 + 顶层 await） |
| 模块系统 | ES Modules（`import`/`export`），支持顶层 `await` |
| 入口文件 | `run.js`（由 `meta.yaml` 的 `entry` 指定） |

### 调用方式

supd 执行器通过 `BuildCommand` 构造命令：`[/usr/local/bin/tjs, run.js, ...args]`。

`/usr/local/bin/tjs` 是包装脚本，自动识别子命令：
- `tjs run.js` → `tjs-bin run run.js`（自动补 `run` 子命令）
- `tjs run run.js` → `tjs-bin run run.js`（显式 `run`）
- `tjs --version` → `tjs-bin --version`

---

## 2. 工作流集成与 musl 兼容性（关键约束）

> **⚠️ 这是 tjs 集成最容易出错的环节，修改 CI/Dockerfile 时务必遵守。**

### 问题背景

supd 运行时镜像是 `alpine:3.20`（musl libc），而 GitHub Actions 默认 `ubuntu-latest` 是 glibc。若在 ubuntu 上编译 tjs，产出的二进制依赖 `/lib64/ld-linux-x86-64.so.2`，在 Alpine 中报错：

```
/usr/local/bin/tjs-bin: cannot execute: required file not found
exit code 127
```

### 正确做法

1. **CI 必须在 Alpine 容器中编译 tjs**（见 `.github/workflows/release.yml` 的 `build-tjs` job）
   ```yaml
   - name: 在 Alpine 容器中编译 txiki.js
     run: |
       docker run --rm -e TJS_VERSION -v /tmp/tjs-binary:/output \
         alpine:3.20 sh -c '
           apk add --no-cache build-base cmake ninja git ca-certificates \
             curl-dev libffi-dev openssl-dev zlib-dev linux-headers
           git clone --recursive --depth 1 --branch "${TJS_VERSION}" \
             https://github.com/saghul/txiki.js.git /tmp/txiki-src
           cd /tmp/txiki-src && make
           cp $(find build -name tjs -type f | head -1) /output/tjs
         '
   ```

2. **Dockerfile 必须安装 tjs 运行时依赖**（musl 编译的 tjs 仍需动态库）
   ```dockerfile
   RUN apk add --no-cache ... libffi libstdc++ libgcc ...
   ```

3. **验证二进制是 musl 链接**：
   ```
   file /usr/local/bin/tjs-bin
   # 正确: interpreter /lib/ld-musl-x86_64.so.1
   # 错误: interpreter /lib64/ld-linux-x86-64.so.2
   ```

### 排查清单（tjs 扩展报 exit code 127 时）

| 检查项 | 命令 |
|---|---|
| tjs-bin 是否存在 | `ls -la /usr/local/bin/tjs-bin` |
| 是否 musl 链接 | `file /usr/local/bin/tjs-bin` |
| 运行时库是否齐全 | `ldd /usr/local/bin/tjs-bin`（不应有 "not found"） |
| 能否执行 | `/usr/local/bin/tjs-bin --version` |

---

## 3. tjs API 速查（基于 v26.6.0 实际探测）

> 以下 API 均经实际运行验证。tjs 的 API **主要是全局 `tjs` 对象的方法**，不是子对象（与 Node.js 的 fs/process 不同）。

### 3.1 全局 `tjs` 对象

#### 环境与系统信息
| API | 类型 | 说明 |
|---|---|---|
| `tjs.version` | string | tjs 版本号（如 `"26.6.0"`） |
| `tjs.engine` | object | 引擎信息 |
| `tjs.platform` | string | 平台标识 |
| `tjs.pid` / `tjs.ppid` | number | 当前/父进程 PID |
| `tjs.cwd` | string | 当前工作目录 |
| `tjs.homeDir` | string | 用户主目录 |
| `tjs.hostName` | string | 主机名 |
| `tjs.tmpDir` | string | 临时目录 |
| `tjs.exePath` | string | tjs 可执行文件路径 |
| `tjs.args` | string[] | 命令行参数数组 |
| `tjs.env` | object | **环境变量对象**（如 `tjs.env.HOME`） |
| `tjs.system` | object | 系统信息（`cpus`/`loadAvg`/`networkInterfaces`/`uptime`/`userInfo`） |

#### 文件系统（异步，返回 Promise）
| API | 说明 |
|---|---|
| `await tjs.readFile(path)` | 读取文件，返回 `Uint8Array` |
| `await tjs.writeFile(path, data)` | 写入文件，data 为 `Uint8Array` 或 `string` |
| `await tjs.readDir(path)` | 列出目录，返回数组 |
| `await tjs.stat(path)` / `tjs.lstat(path)` | 文件状态，返回含 `mode`/`size`/`mtim` 等 |
| `await tjs.makeDir(path)` | 创建目录 |
| `await tjs.makeTempDir()` / `tjs.makeTempFile()` | 创建临时目录/文件 |
| `await tjs.remove(path)` | 删除文件或目录 |
| `await tjs.rename(old, new)` | 重命名/移动 |
| `await tjs.copyFile(src, dst)` | 复制文件 |
| `await tjs.chmod(path, mode)` | 修改权限（mode 为数字，如 `0o755`） |
| `await tjs.chown(path, uid, gid)` / `tjs.lchown` | 修改属主 |
| `await tjs.realPath(path)` | 解析真实路径 |
| `await tjs.readLink(path)` | 读取符号链接 |
| `await tjs.symlink(target, path)` / `tjs.link` | 创建符号/硬链接 |
| `await tjs.utime(path, atim, mtim)` / `tjs.lutime` | 修改访问/修改时间 |
| `await tjs.statFs(path)` | 文件系统状态 |
| `tjs.watch(path, callback)` | 监听文件变化 |

#### 进程与执行
| API | 说明 |
|---|---|
| `await tjs.spawn(args, options)` | 启动子进程，args 为数组，返回进程对象 |
| `await tjs.exec(cmdline)` | 执行命令行字符串 |
| `tjs.kill(pid, signal)` | 发送信号 |
| `tjs.exit(code)` | 退出进程 |
| `tjs.chdir(path)` | 改变工作目录 |

#### 网络
| API | 说明 |
|---|---|
| `tjs.connect(options)` | TCP 连接 |
| `tjs.listen(options)` | TCP 监听 |
| `tjs.lookup(hostname)` | DNS 查询 |
| `tjs.serve(handler)` | HTTP 服务 |

#### 标准流与信号
| API | 说明 |
|---|---|
| `tjs.stdin` / `tjs.stdout` / `tjs.stderr` | 标准流对象 |
| `tjs.addSignalListener(sig, cb)` | 信号监听 |
| `tjs.removeSignalListener(sig, cb)` | 移除监听 |

### 3.2 ES 模块（通过 `import`）

只有以下两个模块需要 `import`，其余 API 都在全局 `tjs` 对象上：

```javascript
// 路径处理（同 Node.js path 模块）
import path from 'tjs:path';
path.join('/a', 'b', 'c');     // '/a/b/c'
path.dirname('/a/b/c.txt');     // '/a/b'
path.basename('/a/b/c.txt');    // 'c.txt'
path.extname('/a/b/c.txt');     // '.txt'
path.resolve('/a', 'b');        // '/a/b'

// 哈希
import { createHash } from 'tjs:hashing';
const hash = createHash('sha256');
hash.update('data');
const digest = hash.digest();  // Uint8Array
```

### 3.3 Web Platform APIs（全局，无需 import）

| 类别 | 可用 API |
|---|---|
| **HTTP** | `fetch`, `Request`, `Response`, `Headers`, `FormData` |
| **流** | `ReadableStream`, `WritableStream`, `TransformStream` |
| **编码** | `TextEncoder`, `TextDecoder`, `atob`, `btoa` |
| **压缩** | `CompressionStream`, `DecompressionStream` |
| **URL** | `URL`, `URLSearchParams`, `URLPattern` |
| **WebSocket** | `WebSocket`, `WebSocketStream` |
| **Socket** | `TCPSocket`, `TCPServerSocket`, `TLSSocket`, `UDPSocket` |
| **定时器** | `setTimeout`, `setInterval`, `clearTimeout`, `clearInterval` |
| **二进制** | `Uint8Array`, `Blob`, `File`, `FileReader` |
| **其他** | `console`, `crypto`, `performance`, `AbortController`, `localStorage`, `Worker`, `XMLHttpRequest` |

> **注意**：tjs **没有** Node.js 的 `Buffer`、`require`、`process`、`__dirname`。使用 `TextEncoder`/`TextDecoder` 替代 Buffer。

### 3.4 supd 注入的环境变量

tjs 扩展通过 `tjs.env` 访问 supd 注入的 14 个 `SUPD_*` 变量：

```javascript
const serviceDir = tjs.env.SUPD_SERVICE_DIR;   // 关联服务目录
const action = tjs.env.SUPD_ACTION;             // 当前 action ID
const runId = tjs.env.SUPD_RUN_ID;              // 运行 ID
const extName = tjs.env.SUPD_EXTENSION_NAME;    // 扩展名
```

完整变量列表见 `references/02_extension_spec.md` 第 4 节。

---

## 4. tjs 扩展配置（meta.yaml）

```yaml
name: my-tjs-ext
version: "1.0.0"
description: "tjs 扩展示例"
runtime: tjs          # 关键：指定 tjs 运行时
entry: run.js         # 入口文件（.js，不是 .sh）
timeout_seconds: 60   # tjs 脚本通常较快，可设短

concurrency: replace

actions:
  - id: do-something
    label: 执行操作
    button_style: primary

triggers:
  on_demand: true
```

> **注意**：`entry` 指向的 `run.js` **不需要可执行权限**（tjs 解释执行），但仍建议 `chmod +x` 保持一致性。

---

## 5. run.js 开发模板

### 5.1 基本 skeleton（含 supd stdout 协议）

```javascript
// run.js — tjs 扩展入口
// 1. 读取 supd 注入的上下文
const action = tjs.env.SUPD_ACTION || 'run';
const serviceDir = tjs.env.SUPD_SERVICE_DIR || '';

// 2. 根据 action 分发
switch (action) {
  case 'check':
    await doCheck();
    break;
  case 'install':
    await doInstall();
    break;
  default:
    console.log(`unknown action: ${action}`);
}

// 3. 上报进度与结果（supd stdout 协议）
//    ::progress:: <0-100> "可选消息"
//    ::result:: <success|warning|error> "结果消息"
async function doCheck() {
  console.log('::progress:: 50 "检查中..."');
  // ... 业务逻辑 ...
  console.log('::result:: success "检查完成"');
}

async function doInstall() {
  try {
    console.log('::progress:: 10 "开始安装"');
    // ... 安装逻辑 ...
    console.log('::progress:: 100 "安装完成"');
    console.log('::result:: success "安装成功"');
  } catch (e) {
    console.error('安装失败:', e.message);
    console.log(`::result:: error "安装失败: ${e.message}"`);
    tjs.exit(1);
  }
}
```

### 5.2 文件下载与保存（fetch + tjs.writeFile）

```javascript
// ⚠️ 重要：大文件（>10MB）必须用流式读取，resp.arrayBuffer() 会卡死！
// 详见 7.5 节「fetch 大文件 arrayBuffer 卡死」
async function downloadFile(url, destPath) {
  console.log(`下载: ${url}`);
  const resp = await fetch(url, {
    headers: { 'User-Agent': 'supd-tjs-ext' },
  });
  if (!resp.ok) {
    throw new Error(`HTTP ${resp.status}: ${resp.statusText}`);
  }

  // ✅ 流式读取：resp.body.getReader() 分块接收，稳定可靠
  const reader = resp.body.getReader();
  const chunks = [];
  let received = 0;
  while (true) {
    const { done, value } = await reader.read();
    if (done) break;
    chunks.push(value);
    received += value.length;
  }
  // 合并 chunks（内存占用 = 文件大小，对几十 MB 可接受）
  const buffer = new Uint8Array(received);
  let pos = 0;
  for (const chunk of chunks) {
    buffer.set(chunk, pos);
    pos += chunk.length;
  }

  await tjs.writeFile(destPath, buffer);
  console.log(`已保存到 ${destPath} (${buffer.length} bytes)`);
  return buffer.length;
}
```

> **❌ 错误写法（大文件会卡死）**：`const buffer = new Uint8Array(await resp.arrayBuffer());`
> tjs 的 `resp.arrayBuffer()` 对大响应体（实测 34MB 即触发）会永久挂起直至扩展超时。
> 小响应（JSON API、几 KB 文本）用 `await resp.json()` / `await resp.text()` 没问题。

### 5.3 执行外部命令（tjs.spawn）

```javascript
async function runCommand(args, cwd) {
  const proc = await tjs.spawn(args, {
    cwd: cwd || tjs.cwd,
    stdout: 'pipe',
    stderr: 'pipe',
  });
  // 读取输出
  const encoder = new TextEncoder();
  const decoder = new TextDecoder();
  // ... 读取 proc.stdout / proc.stderr ...
  const status = await proc.wait();
  return status;
}
```

### 5.4 读取 action 参数

supd 通过 `SUPD_ACTION` 环境变量传递当前 action ID，通过 `SUPD_ACTION_ARGS` 或命令行参数传递 args。在 tjs 中：

```javascript
const action = tjs.env.SUPD_ACTION;
// action args 通过 tjs.args 传递（在 entry 之后）
// tjs.args = ['tjs', 'run', 'run.js', ...actionArgs]
const actionArgs = tjs.args.slice(3);  // 跳过 'tjs' 'run' 'run.js'
```

---

## 6. tjs 与 bash 扩展的差异

| 维度 | bash 扩展 | tjs 扩展 |
|---|---|---|
| `runtime` | `bash` | `tjs` |
| `entry` | `run.sh`（需 `chmod +x`） | `run.js`（无需可执行权限） |
| 异步 | 不支持（需阻塞） | 原生 `async`/`await` + 顶层 await |
| HTTP 请求 | `curl` 命令 | `fetch()` 全局函数 |
| 文件操作 | shell 命令（`cat`/`cp`/`mv`） | `tjs.readFile`/`tjs.writeFile` |
| JSON 处理 | `jq` 命令 | 原生 `JSON.parse`/`JSON.stringify` |
| 环境变量 | `$VAR` | `tjs.env.VAR` |
| 执行命令 | 直接调用 | `tjs.spawn()` / `tjs.exec()` |
| stdout 协议 | 相同（`::progress::`/`::result::`） | 相同 |
| SUPD_* 变量 | 相同（14 个） | 相同（通过 `tjs.env` 访问） |

### 何时选择 tjs

- 需要 JSON 解析、复杂逻辑判断
- 需要跨平台（不依赖 shell 工具）
- 需要 fetch 处理 HTTP API（比 curl 更灵活）
- 需要异步并发

### 何时选择 bash

- 简单的命令编排
- 依赖 shell 工具（curl/jq/sed/grep）
- 启动速度要求高（tjs 有 JS 引擎启动开销）

---

## 7. 常见错误排查

### 7.1 exit code 127 — tjs 二进制问题

**症状**：扩展立即失败，exit code 127，日志无输出。

**原因**：tjs-bin 缺失或无法运行（musl/glibc 不匹配）。

**排查**（创建一个 bash 诊断扩展）：
```bash
#!/bin/bash
ls -la /usr/local/bin/tjs*
file /usr/local/bin/tjs-bin
ldd /usr/local/bin/tjs-bin 2>&1 | head -5
/usr/local/bin/tjs-bin --version 2>&1
```

**解决**：见第 2 节「工作流集成与 musl 兼容性」。

### 7.2 模块导入失败

**症状**：`import 'tjs:filesystem'` 报错。

**原因**：tjs 的文件系统 API 在全局 `tjs` 对象上，不是模块。只有 `tjs:path` 和 `tjs:hashing` 是模块。

**解决**：用 `tjs.readFile()` 而非 `import 'tjs:filesystem'`。

### 7.3 Buffer 未定义

**症状**：`ReferenceError: Buffer is not defined`。

**原因**：tjs 没有 Node.js 的 `Buffer`。

**解决**：用 `TextEncoder`/`TextDecoder` + `Uint8Array`：
```javascript
const encoder = new TextEncoder();
const bytes = encoder.encode('text');
const decoder = new TextDecoder();
const text = decoder.decode(uint8array);
```

### 7.4 fetch 证书错误

**症状**：`fetch` HTTPS 请求报证书错误。

**解决**：确保容器安装了 `ca-certificates`（Dockerfile 已含）。自定义 CA 用 `--tls-ca` 或 `TJS_CA_BUNDLE` 环境变量。

### 7.5 fetch 大文件 arrayBuffer 卡死（⚠️ 高频坑）

**症状**：用 `await resp.arrayBuffer()` 读取大响应体（实测 34MB 即触发）时，扩展永久挂起，直至 `timeout_seconds` 超时。日志停在 `arrayBuffer()` 调用前，无任何错误输出，状态变为 `timeout`。

**根因**：tjs 的 `resp.arrayBuffer()` 对大响应体存在阻塞/死锁问题，会卡住事件循环。

**解决**：改用 `ReadableStream` 流式分块读取，收集后合并：

```javascript
const reader = resp.body.getReader();
const chunks = [];
let received = 0;
while (true) {
  const { done, value } = await reader.read();
  if (done) break;
  chunks.push(value);
  received += value.length;
}
const buffer = new Uint8Array(received);
let pos = 0;
for (const chunk of chunks) { buffer.set(chunk, pos); pos += chunk.length; }
// buffer 即为完整文件内容，可用 tjs.writeFile 写入
```

流式读取实测 34MB 仅需 ~7 秒，稳定可靠（已在 v0.0.12 镜像验证）。

**注意**：小响应（JSON API、几 KB 文本）用 `await resp.json()` / `await resp.text()` / `await resp.arrayBuffer()` 均正常，问题仅出现在大响应体（>10MB 量级）。

---

## 8. 完整示例：on_demand tjs 扩展

见 `examples/09-tjs-ext/`（如存在）或本节内联示例：

```yaml
# meta.yaml
name: tjs-demo
version: "1.0.0"
description: "tjs 运行时扩展示例"
runtime: tjs
entry: run.js
timeout_seconds: 30
actions:
  - id: run
    label: 运行
    button_style: primary
triggers:
  on_demand: true
```

```javascript
// run.js
const action = tjs.env.SUPD_ACTION || 'run';
console.log('::progress:: 25 "启动中"');
console.log(`tjs version: ${tjs.version}`);
console.log(`cwd: ${tjs.cwd}`);
console.log(`action: ${action}`);

// 演示 fetch
console.log('::progress:: 50 "请求中"');
try {
  const resp = await fetch('https://api.github.com/repos/saghul/txiki.js');
  const data = await resp.json();
  console.log(`txiki.js stars: ${data.stargazers_count}`);
} catch (e) {
  console.log(`fetch failed: ${e.message}`);
}

// 演示文件写入
const encoder = new TextEncoder();
await tjs.writeFile('/tmp/tjs-demo.txt', encoder.encode('hello from tjs\n'));

console.log('::progress:: 100 "完成"');
console.log('::result:: success "tjs demo done"');
```
