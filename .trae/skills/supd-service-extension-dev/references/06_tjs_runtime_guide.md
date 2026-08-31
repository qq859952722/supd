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

supd 执行器通过 `BuildCommand` 构造命令：`[/usr/local/bin/tjs, run.js]`。action 区分通过 `SUPD_ACTION` 环境变量传递，不在命令行拼接参数。

`/usr/local/bin/tjs` 是包装脚本，自动识别子命令：
- `tjs run.js` → `tjs-bin run run.js`（自动补 `run` 子命令）
- `tjs run run.js` → `tjs-bin run run.js`（显式 `run`）
- `tjs --version` → `tjs-bin --version`

> **tjs 不是内置运行时**：supd 内置运行时只有 `bash`/`sh`/`python3`/`node`（PATH 查找）。`tjs` 通过 `config.yaml` 的 `runtimes` 映射注册（`supd init` 生成的默认配置已包含 `tjs: /usr/local/bin/tjs`），来源标记为 `config`。可通过 `supd runtimes list` 查看、`supd runtimes install tjs /path/to/tjs` 注册、`supd runtimes remove tjs` 移除。

---

## 2. 工作流集成与 libc 兼容性（关键约束）

> **⚠️ 这是 tjs 集成最容易出错的环节，修改 CI/Dockerfile 时务必遵守。**

### 问题背景

supd 同时提供两种独立运行时镜像：默认 `alpine:3.20` 使用 musl libc，`debian`/`vX.Y.Z-debian` 使用 Debian Bookworm Slim + glibc。两种镜像分别使用对应 libc 环境编译的 tjs，二进制不能混用。若在 Ubuntu/glibc 上编译 tjs 后放入 Alpine，产物依赖 `/lib64/ld-linux-x86-64.so.2`，会报错：

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

3. **Alpine 镜像验证二进制是 musl 链接**：
   ```
   file /usr/local/bin/tjs-bin
   # 正确: interpreter /lib/ld-musl-x86_64.so.1
   # 错误: interpreter /lib64/ld-linux-x86-64.so.2
   ```

4. **Debian 镜像使用独立 glibc 构建**：`Dockerfile.debian` 基于 `debian:bookworm-slim`；CI 的 `build-tjs-debian`/`build-debian` 在 Debian 中编译并验证 tjs，通过 `debian` 或 `vX.Y.Z-debian` 标签发布。不要把 Alpine/musl 的 tjs 复制到 Debian，也不要把 Debian/glibc 的 tjs 复制到 Alpine。

5. **镜像层顺序与增量更新**：`Dockerfile` 与 `Dockerfile.debian` 使用 `COPY --link --chmod=755` 分离 `supd`、`tjs` 二进制层；CI 必须保持 `context: .` 并将对应架构的 tjs 放置为构建上下文根目录的 `./tjs`。更新 supd 时可复用基础系统和 tjs 层，但容器仍需按正常流程重建/重启。

6. **普通服务二进制同样遵循 libc 分流**：在 Alpine 中发现待安装二进制依赖 glibc 时立即停止，不尝试 `glibc`/`gcompat`/`libc6-compat` 等兼容层，改为提示用户切换 Debian 镜像。完整门禁见 `01_service_spec.md` §1.7。

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
| `tjs.engine` | object | 引擎信息（`engine.versions`/`engine.features`/`engine.gc`） |
| `tjs.platform` | string | 平台标识 |
| `tjs.pid` / `tjs.ppid` | number | 当前/父进程 PID |
| `tjs.cwd` | string | 当前工作目录 |
| `tjs.homeDir` | string | 用户主目录 |
| `tjs.hostName` | string | 主机名 |
| `tjs.tmpDir` | string | 临时目录 |
| `tjs.exePath` | string | tjs 可执行文件路径 |
| `tjs.args` | string[] | 命令行参数数组 |
| `tjs.env` | object | **环境变量对象**（如 `tjs.env.HOME`） |
| `tjs.system` | object | **系统信息属性对象**（`cpus`/`loadAvg`/`networkInterfaces`/`uptime`/`userInfo`）。⚠️ 均是 Getter **属性**，不是函数！ |

#### 文件系统（异步，返回 Promise）
| API | 说明 |
|---|---|
| `await tjs.readFile(path)` | 读取文件，返回 `Uint8Array` |
| `await tjs.writeFile(path, data)` | 写入文件，data 为 `Uint8Array` 或 `string` |
| `await tjs.open(path, flags, mode?)` | 打开文件返回 `FileHandle`（**流式读写**，详见 §5.2） |
| `await tjs.readDir(path)` | 列出目录，返回 `DirHandle`（v26.6.0 中需 `for await` 异步迭代，见 §8.3 `listEntries` 兼容函数） |
| `await tjs.stat(path)` / `tjs.lstat(path)` | 文件状态，返回对象（含 `mode`/`size`/`mtim`/`uid`/`gid` 及 `isFile`/`isDirectory` 布尔属性） |
| `await tjs.makeDir(path)` | 创建目录 |
| `await tjs.makeTempDir()` / `tjs.makeTempFile()` | 创建临时目录/文件 |
| `await tjs.remove(path)` | 删除文件或目录 |
| `await tjs.rename(old, new)` | 重命名/移动 |
| `await tjs.copyFile(src, dst)` | 复制文件 |
| `await tjs.chmod(path, mode)` | 修改权限（mode 为数字，如 `0o755`） |
| `await tjs.chown(path, uid, gid)` / `tjs.lchown` | 修改属主（不递归） |
| `await tjs.realPath(path)` | 解析真实路径 |
| `await tjs.readLink(path)` | 读取符号链接 |
| `await tjs.symlink(target, path)` / `tjs.link` | 创建符号/硬链接 |
| `await tjs.utime(path, atim, mtim)` / `tjs.lutime` | 修改访问/修改时间 |
| `await tjs.statFs(path)` | 文件系统状态 |
| `tjs.watch(path, callback)` | 监听文件变化 |

#### FileHandle（由 `tjs.open` 返回，支持流式读写）

`tjs.open(path, flags, mode?)` 返回的 `FileHandle` 对象，flags 支持 `'r'`/`'w'`/`'a'`/`'r+'`/`'w+'`/`'a+'`。

| 方法 | 说明 |
|---|---|
| `await fh.write(Uint8Array)` | 写入数据，**自动推进文件位置**（连续调用即追加），返回写入字节数 |
| `await fh.write(Uint8Array, position)` | pwrite：在指定位置写入（不改变当前文件位置） |
| `await fh.read(Uint8Array)` | 读取到 buffer，自动推进位置，返回读取字节数 |
| `await fh.read(Uint8Array, position)` | pread：在指定位置读取 |
| `await fh.close()` | 关闭文件句柄（也支持 `await using fh = ...` 自动关闭） |
| `await fh.sync()` / `fh.datasync()` | 刷盘 |
| `await fh.stat()` / `fh.truncate(n)` / `fh.chmod(mode)` / `fh.chown(uid,gid)` | 文件操作 |
| `fh.path` | 文件路径属性 |

> **关键**：`fh.write(buf)` 自动推进文件位置，连续调用等价于追加写入。这使得逐块下载直接写盘成为可能（内存占用与文件大小无关）。详见 §5.2 `downloadStream`。

#### 进程与执行
| API | 说明 |
|---|---|
| `await tjs.spawn(args, options)` | 启动子进程，args 为数组，返回进程对象（需用 `Promise.all` 读取 stdout/stderr 避免死锁） |
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
| `tjs.addSignalListener(sig, cb)` | 信号监听（如 `'SIGTERM'`） |
| `tjs.removeSignalListener(sig, cb)` | 移除监听 |

### 3.2 ES 模块（通过 `import`）

tjs 提供以下 7 个内置 `tjs:` ES 模块：

```javascript
// 1. 路径处理（tjs:path）
import path from 'tjs:path';
path.join('/a', 'b', 'c');       // '/a/b/c'
path.dirname('/a/b/c.txt');       // '/a/b'
path.basename('/a/b/c.txt');      // 'c.txt'
path.extname('/a/b/c.txt');       // '.txt'
path.resolve('/a', 'b');          // '/a/b'

// 2. 哈希摘要（tjs:hashing）
import { createHash, SUPPORTED_TYPES } from 'tjs:hashing';
// SUPPORTED_TYPES: md5, sha1, sha224, sha256, sha384, sha512, sha3_256 等
const hash = createHash('sha256');
hash.update('hello');
const hexDigest = hash.digest();  // ⚠️ 返回 16 进制字符串（如 "2cf24dba..."），非 Uint8Array

// 3. SQLite 数据库（tjs:sqlite）
import { Database } from 'tjs:sqlite';
const db = new Database(':memory:');
db.exec('CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT)');
const stmt = db.prepare('INSERT INTO users (name) VALUES (?)');
stmt.run('Alice');
stmt.finalize();
const select = db.prepare('SELECT * FROM users');
const rows = select.all();        // 返回行对象数组
select.finalize();
db.close();

// 4. UUID 生成（tjs:uuid）
import uuid from 'tjs:uuid';
const id = uuid.v4();             // 亦可用全局 crypto.randomUUID()

// 5. C 原生 FFI 绑定（tjs:ffi）
import { dlopen, types, suffix } from 'tjs:ffi';
const lib = dlopen(`libc.${suffix}`, {
  getpid: { rtype: types.sint32, args: [] }
});

// 6. 交互式 Readline 命令行（tjs:readline）
import readline from 'tjs:readline';
const rl = readline.createInterface({ input: tjs.stdin, output: tjs.stdout });

// 7. WASI 支持（tjs:wasi）
import { WASI } from 'tjs:wasi';
const wasi = new WASI({ version: 'wasi_snapshot_preview1' });
```

### 3.3 Web Platform APIs（全局，无需 import）

| 类别 | 可用 API |
|---|---|
| **HTTP** | `fetch`, `Request`, `Response`, `Headers`, `FormData` |
| **加密 (Web Crypto)** | `crypto.randomUUID()`, `crypto.getRandomValues()`, `crypto.subtle` (`digest`, `encrypt`, `decrypt`, `sign`, `verify`, `importKey`, `exportKey`) |
| **流** | `ReadableStream`, `WritableStream`, `TransformStream` |
| **编码** | `TextEncoder`, `TextDecoder`, `TextEncoderStream`, `TextDecoderStream`, `atob`, `btoa` |
| **压缩** | `CompressionStream`, `DecompressionStream`（gzip / deflate） |
| **URL** | `URL`, `URLSearchParams`, `URLPattern` |
| **WebSocket** | `WebSocket`, `WebSocketStream`, `WebSocketError` |
| **Socket** | `TCPSocket`, `TCPServerSocket`, `TLSSocket`, `TLSServerSocket`, `UDPSocket`, `PipeSocket`, `PipeServerSocket` |
| **定时器与微任务** | `setTimeout`, `setInterval`, `clearTimeout`, `clearInterval`, `queueMicrotask` |
| **二进制与深拷贝** | `Uint8Array`, `Blob`, `File`, `FileReader`, `structuredClone()` |
| **其他 Web API** | `console`, `performance` (`performance.now()`), `AbortController`, `localStorage`, `sessionStorage`, `Worker`, `XMLHttpRequest`, `WebAssembly` |

> **注意**：tjs **没有** Node.js 的 `Buffer`、`require`、`process`、`__dirname`。使用 `TextEncoder`/`TextDecoder` 替代 Buffer，使用 `tjs.env` 替代 `process.env`。

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

### 5.2 文件下载与保存

#### downloadStream（推荐：真正的流式落盘，内存与文件大小无关）

使用 `tjs.open()` 打开文件句柄，逐块 `fh.write()` 直接写盘。内存占用仅为单个 chunk（约 8-64KB），与文件大小无关。已在 txiki.js v26.6.0 实测验证。

```javascript
/**
 * 流式下载文件到磁盘 —— 内存占用与文件大小无关（仅单块缓冲）
 * tjs.open + fh.write 实测验证（txiki.js v26.6.0）
 *
 * @param {string} url 下载地址
 * @param {string} destPath 目标路径
 * @param {object} [opts] 可选：onProgress(received, total)、maxBytes、headers
 * @returns {Promise<number>} 已下载字节数
 */
async function downloadStream(url, destPath, opts = {}) {
  const { onProgress = null, maxBytes = 0, headers = {} } = opts;
  const resp = await fetch(url, {
    headers: { 'User-Agent': 'supd-tjs-ext', ...headers },
  });
  if (!resp.ok) throw new Error(`HTTP ${resp.status}: ${resp.statusText}`);

  const total = resp.headers.get('content-length');
  const totalNum = total ? parseInt(total, 10) : 0;

  const fh = await tjs.open(destPath, 'w', 0o644);
  let received = 0;
  try {
    const reader = resp.body.getReader();
    while (true) {
      const { done, value } = await reader.read();
      if (done) break;
      if (maxBytes && received + value.length > maxBytes) {
        throw new Error(`超出大小上限 ${maxBytes}（已接收 ${received + value.length}）`);
      }
      await fh.write(value);   // 自动推进位置，逐块落盘
      received += value.length;
      if (onProgress) onProgress(received, totalNum);
    }
    await fh.sync();           // 刷盘
  } finally {
    await fh.close();
  }
  return received;
}
```

#### downloadFile（旧版：全量内存，适合小文件）

`fetch` + `getReader()` 分块读取后合并为完整 `Uint8Array` 再 `tjs.writeFile`。内存峰值约为文件大小的 2 倍（chunks 数组 + 合并 buffer）。适合几 MB 以内的小文件/JSON。**大文件请用上面的 `downloadStream`。**

```javascript
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

> 完整可复用的 `runCmd()` / `readStream()` 辅助函数见 §8.3「通用辅助函数」。以下为简要说明：

```javascript
// tjs.spawn 启动子进程，stdout/stderr 为 ReadableStream
// ⚠️ 关键：必须使用 Promise.all 并发读取 stdout 和 stderr，避免管道死锁！
const proc = await tjs.spawn(['tar', '-xf', path, '-C', dir], {
  stdout: 'pipe',
  stderr: 'pipe',
});
// 并发读取 stdout/stderr（避免子进程写 stderr 超过 64KB 缓冲区阻塞导致死锁）
const [stdout, stderr] = await Promise.all([
  readStream(proc.stdout),
  readStream(proc.stderr),
]);
const status = await proc.wait();  // 等待退出，返回 {exitCode}
```

> **⚠️ 管道死锁警告**：`tjs.spawn` 的 `stdout`/`stderr` 是 `ReadableStream`。如果串行 `await readStream(proc.stdout)` 再 `await readStream(proc.stderr)`，当子进程大量向 stderr 输出（如 `tar -v`/`unzip`）填满 OS 缓冲区时，子进程会被卡住等待 stderr 消费，而父进程卡在等待 stdout 结束，导致**永久死锁挂起**直至超时。必须使用 `Promise.all` 并发读取。

### 5.4 读取 action 参数

supd 通过 `SUPD_ACTION` 环境变量传递当前 action ID。在 tjs 中：

```javascript
const action = tjs.env.SUPD_ACTION;
// 根据 action 值分支处理
if (action === 'backup') {
  // 备份逻辑
} else if (action === 'verify') {
  // 验证逻辑
}
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

**解决**：见第 2 节「工作流集成与 libc 兼容性」。

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

### 7.6 `tjs.readDir` 返回 `DirHandle` 导致 `value is not iterable` 或 `isDirectory is not a function`

**症状**：`TypeError: value is not iterable` 或 `TypeError: ent.isDirectory is not a function`。

**原因**：
1. 在 txiki.js v26.6.0 中，`tjs.readDir(path)` 返回的是 **`DirHandle`**（异步可迭代对象），直接使用 `for...of` 同步循环会报错。
2. 迭代出的 `ent` 对象的 `isDirectory` 和 `isFile` 在 v26.6.0 中是**布尔 Getter 属性**（如 `ent.isDirectory`），而不是 Node.js 式的方法（`ent.isDirectory()`）。

**解决**：使用 `listEntries()` 兼容封装函数（见 §8.3），使用 `for await (const ent of dh)` 并兼容属性与方法判断。

### 7.7 子进程管道死锁（stdout 与 stderr 串行读取）

**症状**：扩展运行到子进程执行（如 `tar`/`unzip`/`chown`）时无限挂起至超时。

**原因**：串行 `await readStream(proc.stdout)` 再 `await readStream(proc.stderr)`，若子进程向 stderr 输出大量日志填满 OS pipe buffer (64KB)，子进程阻塞在写 stderr，父进程阻塞在读 stdout，形成死锁。

**解决**：在 `runCmd()` 中使用 `Promise.all([readStream(proc.stdout), readStream(proc.stderr)])` 并发读取（见 §8.3）。

### 7.8 二进制工具 `--version` 输出在 stderr

**症状**：通过子进程获取版本号（如 `daemon --version`）时返回空字符串或匹配失败。

**原因**：部分 C/C++ 工具（如 `transmission-daemon --version`）会将版本帮助信息打印到 `stderr` 而非 `stdout`。

**解决**：合并 `stdout` 与 `stderr` 字符串后再执行正则提取：
```javascript
const { stdout, stderr } = await runCmd([binPath, '--version']);
const m = (stdout + stderr).match(/(\d+\.\d+\.\d+)/);
const version = m ? m[1] : 'unknown';
```

---

## 8. 外部命令依赖管理（减少外部依赖）

tjs 扩展应优先使用内置 API，仅在「无等价替代」时才调用外部命令。下表基于实际开发经验整理。

### 8.1 可用 tjs 内置 API 替代的 shell 工具

| shell 工具 | tjs 替代方案 | 说明 |
|---|---|---|
| `curl` / `wget` | `fetch()` | 全局函数，支持 headers/stream |
| `jq` | `JSON.parse()` / `JSON.stringify()` | 原生 JSON 处理 |
| `cat` | `await tjs.readFile(path)` | 返回 `Uint8Array`，用 `TextDecoder` 转字符串 |
| `rm` / `rm -r` | `await tjs.remove(path)` | 自动递归删除目录 |
| `mkdir -p` | `await tjs.makeDir(path)` | 递归创建 |
| `mv` | `await tjs.rename(old, new)` | 重命名/移动 |
| `chmod` | `await tjs.chmod(path, 0o755)` | mode 为数字 |
| `test -f` / `test -d` | `await tjs.stat(path)` try/catch | 抛错即不存在 |
| `find <dir> -name <pat>` | `findFile()` 见 §8.3 | 递归 `tjs.readDir` / `listEntries` |
| `cp -r` | `copyDir()` 见 §8.3 | 递归 `listEntries` + `tjs.copyFile` |
| `echo > file` | `await tjs.writeFile(path, data)` | data 为 string 或 Uint8Array |

### 8.2 无 tjs 等价替代、必须保留的外部工具

| 工具 | 原因 | 备注 |
|---|---|---|
| `tar` | `DecompressionStream` 仅支持 gzip/deflate，**不支持 tar 多文件归档格式** | .tar.gz/.tar.xz 解压必须保留 |
| `unzip` | `DecompressionStream` **不支持 ZIP 归档格式**（ZIP 有独立文件索引） | .zip 解压必须保留 |
| `chown -R` | `tjs.chown` 存在但**不支持递归**，需手动遍历 | 整目录改属主建议保留 shell；少量文件可手动递归 |
| `uname -m` | `tjs.platform`/`tjs.system` 无可靠的 CPU 架构字段 | 获取架构信息需保留 |

> **原则**：调用外部命令时统一封装 `runCmd()` 辅助函数（见 §8.3），集中管理 `tjs.spawn` + 流读取 + 退出码，避免散落各处。

### 8.3 通用辅助函数（可直接复用）

```javascript
// --- 1. 读取 ReadableStream 为字符串（用于读取子进程 stdout/stderr） ---
async function readStream(stream) {
  const reader = stream.getReader();
  const decoder = new TextDecoder();
  let result = '';
  while (true) {
    const { done, value } = await reader.read();
    if (done) break;
    result += decoder.decode(value, { stream: true });
  }
  result += decoder.decode();
  return result;
}

// --- 2. 执行外部命令（带并发读取防止管道死锁） ---
async function runCmd(args, options = {}) {
  const proc = await tjs.spawn(args, {
    stdout: 'pipe',
    stderr: 'pipe',
    ...options,
  });
  // 必须 Promise.all 并发读取，防止 stderr 溢出导致 OS 管道死锁
  const [stdout, stderr] = await Promise.all([
    readStream(proc.stdout),
    readStream(proc.stderr),
  ]);
  const status = await proc.wait();
  return { stdout, stderr, exitCode: status.exitCode ?? 0 };
}

// --- 3. 目录文件列表兼容读取函数（处理 v26.6.0 DirHandle 异步迭代及属性差异） ---
async function listEntries(dir) {
  let dh;
  try {
    dh = await tjs.readDir(dir);
  } catch (e) {
    return null; // 非目录或不存在
  }
  // txiki.js v26.6.0: DirHandle (Symbol.asyncIterator)
  if (dh && typeof dh[Symbol.asyncIterator] === 'function') {
    const out = [];
    for await (const ent of dh) {
      const name = typeof ent === 'string' ? ent : ent.name;
      let isDir = false;
      if (typeof ent === 'string') {
        isDir = false;
      } else if (typeof ent.isDirectory === 'function') {
        isDir = ent.isDirectory();
      } else if (typeof ent.isFile === 'function') {
        isDir = !ent.isFile();
      } else {
        isDir = !!ent.isDirectory; // v26.6.0 布尔 getter 属性
      }
      out.push({ name, isDir });
    }
    if (typeof dh.close === 'function') { try { await dh.close(); } catch (e) {} }
    return out;
  }
  // 数组兼容格式
  if (Array.isArray(dh)) {
    return dh.map((ent) => {
      const name = typeof ent === 'string' ? ent : ent.name;
      const isDir = typeof ent === 'string' ? false : (typeof ent.isDirectory === 'function' ? ent.isDirectory() : !!ent.isDir);
      return { name, isDir };
    });
  }
  return [];
}

// --- 4. 递归查找文件（支持 RegExp 或 (name)=>boolean 谓词） ---
async function findFile(dir, matcher) {
  const entries = await listEntries(dir);
  if (!entries) return null;
  for (const entry of entries) {
    const fullPath = `${dir}/${entry.name}`;
    const ok = matcher instanceof RegExp ? matcher.test(entry.name) : matcher(entry.name);
    if (ok) return fullPath;
    if (entry.isDir) {
      const found = await findFile(fullPath, matcher);
      if (found) return found;
    }
  }
  return null;
}

// --- 5. 递归复制目录（替代 cp -r） ---
async function copyDir(src, dst) {
  await tjs.makeDir(dst);
  const entries = await listEntries(src);
  if (!entries) return;
  for (const entry of entries) {
    const srcPath = `${src}/${entry.name}`;
    const dstPath = `${dst}/${entry.name}`;
    if (entry.isDir) {
      await copyDir(srcPath, dstPath);
    } else {
      await tjs.copyFile(srcPath, dstPath);
    }
  }
}

// --- 6. 流式下载文件（避免 arrayBuffer 卡死，>10MB 必须用此方式） ---
async function downloadFile(url, destPath) {
  const resp = await fetch(url, { headers: { 'User-Agent': 'supd-tjs-ext' } });
  if (!resp.ok) throw new Error(`HTTP ${resp.status}: ${resp.statusText}`);
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
  await tjs.writeFile(destPath, buffer);
  return received;
}
```

### 8.4 tjs.chown 不支持递归的应对方案

当需要对整棵目录树改属主时，两种方案：

- **方案 A（推荐，整目录）**：调用 shell `chown -R`，一行命令搞定：
  ```javascript
  await runCmd(['chown', '-R', 'nobody:nobody', serviceDir]);
  ```
- **方案 B（少量文件/无 shell 环境）**：手动递归 `tjs.chown`：
  ```javascript
  async function chownRecursive(path, uid, gid) {
    try { await tjs.chown(path, uid, gid); } catch (e) {}
    let entries;
    try { entries = await tjs.readDir(path); } catch (e) { return; }
    for (const entry of entries) {
      const name = typeof entry === 'string' ? entry : entry.name;
      await chownRecursive(`${path}/${name}`, uid, gid);
    }
  }
  ```

---

## 9. 服务与扩展的权限配置最佳实践

针对「服务降权运行 + 扩展按需提权」的常见安全模式，supd 提供两种身份配置方式（详见 `02_extension_spec.md` §2）：

### 9.1 典型模式：服务 nobody + 扩展 root

适用于「服务面向网络、扩展需管理文件权限」的场景（如下载器、媒体服务）：

```yaml
# service.yaml — 服务以非特权用户运行
name: my-service
user: nobody          # 服务进程降权为 nobody
command: [./my-daemon]
```

```yaml
# meta.yaml — 扩展以 root 运行以便 chown/下载安装
name: my-updater
runtime: tjs
run_as: root          # 扩展提权，可执行 chown/写入服务目录
actions:
  - id: install
```

### 9.2 权限协调要点

1. **服务 `user` 字段**：服务以指定用户身份启动，其写入的文件属主为该用户（如 `nobody`）。
2. **扩展 `run_as: root`**：扩展需要修改服务目录文件属主、安装二进制等操作时，必须提权为 root。
3. **扩展安装后 chown**：扩展以 root 下载/创建文件后，应将 `bin/` 的属主设为 root 或 supd（服务用户只读执行），将 `data/` 的属主设为服务用户。**禁止** `chown -R <serviceDir>` 整目录递归——这会让服务用户获得修改 `bin/`、扩展脚本和 `service.yaml` 的权限，破坏权限隔离：
   ```javascript
   // ✅ 正确：分别处理 bin/ 和 data/
   await runCmd(['chown', '-R', 'root:root', `${serviceDir}/bin`]);
   await runCmd(['chown', '-R', 'nobody:nobody', `${serviceDir}/data`]);
   // ❌ 错误：整目录递归 chown
   // await runCmd(['chown', '-R', 'nobody:nobody', serviceDir]);
   ```
4. **服务级扩展继承规则**：`run_as` 为空时继承所属服务身份；显式指定 `run_as` 则覆盖继承。

### 9.3 服务直接启动二进制（避免 start.sh 包装）

当二进制自身支持指定配置目录时，**直接在 `command` 中调用二进制**，无需 `start.sh` 包装脚本：

```yaml
# ✅ 推荐：直接启动二进制，-g 指定配置目录为当前目录
command:
  - ./my-daemon
  - -f          # 前台运行（supd 管理生命周期，禁止 daemonize）
  - -g
  - .           # 配置目录为服务工作目录
```

避免的 `start.sh` 反模式：
- ❌ 用 shell 脚本下载/安装二进制（应由扩展负责更新）
- ❌ 用 shell 脚本设置目录权限（应由扩展 `run_as: root` 负责）
- ❌ 在脚本中 daemonize 服务（supd 通过 PID 管理生命周期）

> 二进制版本更新、目录初始化等「一次性/按需」操作交给 `on_demand` 扩展完成，服务 `command` 只负责启动常驻进程。

---

## 10. 完整示例：on_demand tjs 扩展

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

---

## 11. WASM 工具使用指南

tjs 内置 WebAssembly 全局 API 和 `tjs:wasi` 模块，可加载并执行 WASI 编译的 `.wasm` 文件。

> **兼容条件**：模块必须使用 tjs 支持的 WASI Preview 1 ABI（通常导入 `wasi_snapshot_preview1`），且所需 WebAssembly 特性受 tjs 内置 WAMR 支持。产物可以来自 wasi-sdk、Rust `wasm32-wasip1`、Zig、TinyGo 等工具链。普通 Emscripten npm 包通常依赖配套 JS glue，不能把其中的 `.wasm` 单独交给 `tjs:wasi` 执行。
>
> `wasm-objdump -x tool.wasm` 可用于查看 imports，但不能仅凭出现 `env` 就断定模块来自 Emscripten。实际兼容性取决于 `wasi.getImportObject()` 与调用方能否满足模块的全部 imports。

### 11.1 WASM 调用模板

```javascript
import { WASI } from 'tjs:wasi';

// 1. 读取 .wasm 文件
const wasmPath = '/etc/supd/runtimes/example.wasm';
const wasmBytes = await tjs.readFile(wasmPath);

// 2. 创建 WASI 实例。文件型 CLI 必须显式授权目录。
const wasi = new WASI({
  version: 'wasi_snapshot_preview1',
  args: ['example', '--input', '/work/input.dat'], // args[0] = 程序名
  env: tjs.env,
  preopens: {
    '/work': '/实际宿主目录', // guest 路径 -> host 路径
  },
  returnOnExit: true,
});

// 3. 编译并实例化
const { instance } = await WebAssembly.instantiate(wasmBytes, wasi.getImportObject());

// 4. 启动并检查退出码
const exitCode = wasi.start(instance);
if (exitCode !== 0) throw new Error(`WASM 工具退出码 ${exitCode}`);
```

**库模式 WASM**（导出自定义函数而非 `_start`）：

```javascript
const { instance } = await WebAssembly.instantiate(wasmBytes, wasi.getImportObject());
// 不调用 wasi.start()，直接调用导出函数
const result = instance.exports.my_function(...args);
```

### 11.2 Skill 内置成品（必要时直接使用）

Skill 已随附两个经过 **txiki.js v26.6.0 本地实测**的 WASI CLI，位于 `assets/wasm/`。开发扩展时可直接复制到扩展目录，或上传到 supd 的 `runtimes/`，无需重新编译。

| 文件 | 能力 | 版本与体积 | SHA-256 |
|------|------|------------|---------|
| `zstd.wasm` | `.zst` 压缩/解压 | Zstandard 1.5.7，682032 B | `fd3a74f1588a347638543955e848efc4c4b77616fe50c47d19e909b2ee24a4fa` |
| `bsdtar.wasm` | 创建/列出/解包 tar；此裁剪版仅内置 zstd filter，适合 `.tar.zst` | bsdtar/libarchive 3.8.7，1240004 B | `e13ebb15ca0971f6629a6313bc043c532dd9be3a0e6bb0b7f8a395de835ad0c0` |

固定来源为 [haskell-wasm/bsdtar-wasm@012117d](https://github.com/haskell-wasm/bsdtar-wasm/commit/012117de366c13285036f37b4fcd9a59d1a06fbb)。两个模块均只导入 `wasi_snapshot_preview1`，无 Node、DOM 或 Emscripten JS glue 依赖；启用了 `simd128`。第三方许可见同目录 `THIRD_PARTY_LICENSES.txt`。

**直接复制和部署**：

```bash
# 在 Skill 根目录执行；也可只复制其中一个
cp assets/wasm/zstd.wasm /etc/supd/runtimes/zstd.wasm
cp assets/wasm/bsdtar.wasm /etc/supd/runtimes/bsdtar.wasm
```

#### zstd.wasm 调用示例

```javascript
import { WASI } from 'tjs:wasi';

const hostDir = tjs.env.SUPD_SERVICE_DIR;
const wasi = new WASI({
  version: 'wasi_snapshot_preview1',
  args: ['zstd', '-d', '-f', '/work/package.zst', '-o', '/work/package'],
  env: tjs.env,
  preopens: { '/work': hostDir },
  returnOnExit: true,
});
const bytes = await tjs.readFile('/etc/supd/runtimes/zstd.wasm');
const { instance } = await WebAssembly.instantiate(bytes, wasi.getImportObject());
const exitCode = wasi.start(instance);
if (exitCode !== 0) throw new Error(`zstd.wasm 解压失败：${exitCode}`);
```

`bsdtar.wasm` 用法相同，只需将参数改成例如：

```javascript
args: ['bsdtar', '-xf', '/work/package.tar.zst', '-C', '/work/output']
```

本地验收已完成：`zstd.wasm` 对 33000 B 文本压缩后解压逐字节一致；`bsdtar.wasm` 成功列出并解包 `.tar.zst`，且校验了解包文件内容。两项测试均由本地源码编译的 tjs v26.6.0 执行。

**不要直接使用**只从 npm 包中取出的 `7z-wasm`、`jq-wasm`、`sevenzip-wasm`、`ffmpeg.wasm` 等文件；这些项目通常需要其配套 Emscripten JS glue。`rockwotj/jq-wasi` 的 Release 只有 libjq 静态库，并非可直接 `wasi.start()` 的 jq CLI 成品。

### 11.3 自编译 WASI 工具

推荐使用 [wasi-sdk](https://github.com/WebAssembly/wasi-sdk) 将 C/Rust 工具编译为 .wasm：

```bash
# 安装 wasi-sdk（以 v25 为例）
wget https://github.com/WebAssembly/wasi-sdk/releases/download/wasi-sdk-25/wasi-sdk-25.0-x86_64-linux.tar.gz
tar xf wasi-sdk-25.0-x86_64-linux.tar.gz
export WASI_SDK_PATH=/opt/wasi-sdk-25.0

# 编译 C 程序为 WASI .wasm
${WASI_SDK_PATH}/bin/clang \
  --sysroot=${WASI_SDK_PATH}/share/wasi-sysroot \
  -O2 -o tool.wasm tool.c

# 编译 Rust 程序（需安装 wasm32-wasip1 target）
rustup target add wasm32-wasip1
cargo build --target wasm32-wasip1 --release
# 产物在 target/wasm32-wasip1/release/tool.wasm
```

### 11.4 WASM 文件部署

WASM 文件部署到 supd 的运行时目录 `<workdir>/runtimes/`（Docker 默认 `workdir=/etc/supd`，即 `/etc/supd/runtimes/`；本地开发为 `test_workdir/runtimes/`）：

1. **构建时内置**：在 Dockerfile 中 `COPY` 或 `curl` 下载到 `/etc/supd/runtimes/`
2. **运行时上传**：通过 supd API `POST /api/runtimes/upload?name=tool.wasm` 上传（存储到 `<workdir>/runtimes/`，权限 0755）
3. **直接复制**：`docker cp tool.wasm <container>:/etc/supd/runtimes/tool.wasm`
4. **API 列出/删除**：`GET /api/runtimes` 列出全部运行时；`DELETE /api/runtimes/{name}` 删除上传的文件（仅能删除 `scan` 来源，不能删除 `builtin`/`config` 声明的别名）

### 11.5 WASM 调用注意事项

- **路径约定**：共享 WASM 工具放在 `/etc/supd/runtimes/<name>.wasm`；扩展私有工具也可随扩展部署，并用绝对路径读取
- **stdin/stdout/stderr**：默认使用宿主 fd 0/1/2。`console.log('::result:: ...')` 是扩展脚本向 supd 上报结果的协议，不是 WASI stdout 的自动转换
- **文件系统访问**：WASI 采用能力模型，`preopens` 默认为空。必须只映射工具实际需要的宿主目录，不能把宿主进程可访问的全部文件系统视为自动可见
- **退出码**：tjs v26.6.0 默认 `returnOnExit: true`，`wasi.start(instance)` 返回退出码。必须检查返回值；设置 `returnOnExit: false` 会让当前 tjs 进程直接按该代码退出
- **CLI/库模式**：带 `_start` 的命令模块调用 `start()`；库模式需要模块导出可直接调用的函数，并自行满足额外 imports
- **体积控制**：引入成品前记录版本、来源、许可证、大小和 SHA-256，并在目标 tjs 版本上做真实输入输出测试
