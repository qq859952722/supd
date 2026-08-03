// run.js — transmission-updater (tjs runtime)
// 检测并更新 transmission-daemon 二进制 + trwm WebUI
// 数据源：https://github.com/qq859952722/transmission-builder/releases
//         https://github.com/qq859952722/transmission_web_manager/releases
//
// v1.1.0 变更：
//   - 修复二进制路径：使用 bin/transmission-daemon（与 service.yaml command 一致），原误用根目录
//   - 兼容非特权运行：chownRecursive 失败时不再终止，依赖 pre-start-fixperms 修复权限

const BIN_REPO = 'qq859952722/transmission-builder';
const BIN_API = `https://api.github.com/repos/${BIN_REPO}/releases/latest`;
const WEBUI_REPO = 'qq859952722/transmission_web_manager';
const WEBUI_API = `https://api.github.com/repos/${WEBUI_REPO}/releases/latest`;

const action = tjs.env.SUPD_ACTION || 'check-update';
const serviceDir = tjs.env.SUPD_SERVICE_DIR || tjs.cwd;

// --- tjs 内置工具函数 ---

/** 读取 ReadableStream 为字符串 */
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

/** 执行外部命令（tjs 无法内置替换的场景：tar/unzip/chown 等） */
// 修复：并发读取 stdout/stderr，避免管道死锁（子进程写 stderr 超过缓冲时阻塞，
// 而父进程还在等 stdout → 双方卡死。tar/unzip/chown 输出几乎都在 stderr）
async function runCmd(args, options = {}) {
  const proc = await tjs.spawn(args, {
    stdout: 'pipe',
    stderr: 'pipe',
    ...options,
  });
  const [stdout, stderr] = await Promise.all([readStream(proc.stdout), readStream(proc.stderr)]);
  const status = await proc.wait();
  return { stdout, stderr, exitCode: status.exitCode ?? 0 };
}

/** 流式下载文件（避免 arrayBuffer 卡死，>10MB 必须用此方式） */
async function downloadFile(url, destPath) {
  const resp = await fetch(url, {
    headers: { 'User-Agent': 'supd-tjs-ext' },
  });
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

/** 列出目录条目，兼容 tjs 不同版本返回的目录形态：
 *  - tjs 26.6.0: tjs.readDir 返回 DirHandle（需 for await 异步迭代，entry 含 .name / .isDirectory()）
 *  - 旧文档版: 返回数组（元素为字符串或 {name,isDir/isDirectory}）
 *  - 个别实现: 返回以文件名为 key 的对象
 * 统一返回 [{name, isDir}]；非目录返回 null
 * 修复：原代码假设 readDir 返回数组并直接 for...of，实际 DirHandle 不可同步迭代 → "value is not iterable" */
async function listEntries(dir) {
  let dh;
  try {
    dh = await tjs.readDir(dir);
  } catch (e) {
    return null; // 非目录
  }
  // tjs 26.6.0: DirHandle（异步可迭代）
  if (dh && typeof dh[Symbol.asyncIterator] === 'function') {
    const out = [];
    for await (const ent of dh) {
      const name = typeof ent === 'string' ? ent : ent.name;
      let isDir;
      if (typeof ent === 'string') {
        isDir = false;
      } else if (typeof ent.isDirectory === 'function') {
        isDir = ent.isDirectory();
      } else if (typeof ent.isFile === 'function') {
        isDir = !ent.isFile(); // 某些实现 isDirectory 不可用，用 isFile 反推
      } else {
        // tjs 26.6.0: isDirectory/isFile 为布尔 getter 属性
        isDir = !!ent.isDirectory;
      }
      out.push({ name, isDir });
    }
    if (typeof dh.close === 'function') { try { await dh.close(); } catch (e) {} }
    return out;
  }
  // 旧版：数组
  if (Array.isArray(dh)) {
    return dh.map((ent) => {
      const name = typeof ent === 'string' ? ent : ent.name;
      const isDir = typeof ent === 'string' ? false : (typeof ent.isDirectory === 'function' ? ent.isDirectory() : !!ent.isDir);
      return { name, isDir };
    });
  }
  // 对象（key 为文件名）
  if (dh && typeof dh === 'object') {
    return Object.keys(dh).map((name) => ({ name, isDir: !!(dh[name] && dh[name].isDir) }));
  }
  return [];
}

/** 递归查找文件（用 tjs.readDir 替代 find 命令）
 * matcher 可为 RegExp 或 (name)=>boolean 谓词，便于精确判定二进制（排除 .sig/.b2sum 等） */
async function findFile(dir, matcher) {
  const entries = await listEntries(dir);
  if (!entries) return null; // 非目录
  for (const entry of entries) {
    const fullPath = `${dir}/${entry.name}`;
    const ok = matcher instanceof RegExp ? matcher.test(entry.name) : matcher(entry.name);
    if (ok) return fullPath;
    if (entry.isDir) {
      const found = await findFile(fullPath, matcher); // 递归子目录
      if (found) return found;
    }
  }
  return null;
}

/** 递归复制目录（用 tjs.readDir + tjs.copyFile 替代 cp -r） */
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

// --- 业务函数 ---

/** 获取当前已安装版本 */
// 修复：transmission-daemon --version 输出到 stderr（stdout 为空），需合并 stdout+stderr 解析
async function getCurrentVersion() {
  try { await tjs.stat(`${serviceDir}/bin/transmission-daemon`); } catch (e) { return 'not-installed'; }
  const { stdout, stderr } = await runCmd([`${serviceDir}/bin/transmission-daemon`, '--version']);
  const m = (stdout + stderr).match(/(\d+\.\d+\.\d+)/);
  return m ? m[1] : 'unknown';
}

/** 查询 GitHub Releases 最新版本 */
// 修复：版本号归一化 —— 使用与 getCurrentVersion 一致的 \d+\.\d+\.\d+ 正则提取核心版本号
// 例：tag "v4.1.3" → "4.1.3"; "transmission-4.1.3" → "4.1.3"; "4.1.3.1" → "4.1.3"
async function getLatestRelease(apiUrl) {
  const resp = await fetch(apiUrl, { headers: { 'User-Agent': 'supd-tjs-ext' } });
  if (!resp.ok) throw new Error(`GitHub API HTTP ${resp.status}`);
  const data = await resp.json();
  const rawTag = (data.tag_name || '').replace(/^v/, '');
  const m = rawTag.match(/(\d+\.\d+\.\d+)/);
  const version = m ? m[1] : rawTag || 'unknown';
  return { version, assets: data.assets || [] };
}

/** 查找 supd 工作区共享 runtimes/ 目录中的 WASM 解压模块 */
async function findSharedWasmPath() {
  const baseDir = tjs.env.SUPD_BASE_DIR || '';
  const candidates = [
    baseDir ? `${baseDir}/runtimes/archive-decompress.wasm` : '',
    baseDir ? `${baseDir}/runtimes/archive.wasm` : '',
    `${serviceDir}/../../runtimes/archive-decompress.wasm`,
    `${serviceDir}/../../runtimes/archive.wasm`,
    `/etc/supd/runtimes/archive-decompress.wasm`,
    `/etc/supd/runtimes/archive.wasm`,
    `${tjs.cwd}/runtimes/archive-decompress.wasm`,
    `${tjs.cwd}/runtimes/archive.wasm`,
  ].filter(Boolean);

  for (const p of candidates) {
    try {
      await tjs.stat(p);
      return p;
    } catch (e) {}
  }
  return null;
}

/** 通用解压归档文件（优先使用 shared runtimes/ 下的 WASM 模块，未找到时平滑降级为系统 Shell 工具） */
async function extractArchive(archivePath, extractDir, type) {
  const wasmPath = await findSharedWasmPath();
  if (wasmPath) {
    try {
      console.log(`[WASM] 检测到共享 WASM 解压模块: ${wasmPath}`);
      const wasiModule = await import('tjs:wasi');
      const WASI = wasiModule.WASI;
      const wasi = new WASI({
        version: 'wasi_snapshot_preview1',
        args: ['archive-extract', archivePath, extractDir],
        env: tjs.env,
      });
      const wasmBytes = await tjs.readFile(wasmPath);
      const { instance } = await WebAssembly.instantiate(wasmBytes, wasi.getImportObject());
      wasi.start(instance);
      console.log('[WASM] 归档包解压成功');
      return;
    } catch (e) {
      console.log(`[WASM Fallback] WASM 解压未成功: ${e.message}，降级使用外部工具`);
    }
  }

  // 降级使用系统 CLI 解压
  if (type === 'tar.xz') {
    await runCmd(['tar', '-xf', archivePath, '-C', extractDir]);
  } else if (type === 'zip') {
    await runCmd(['unzip', '-o', archivePath, '-d', extractDir]);
  }
}

/** 纯 JS 递归修改目录属主（替代外部命令 chown -R，降低对系统 Shell 工具的依赖） */
async function chownRecursive(dirPath, uid, gid) {
  try {
    await tjs.chown(dirPath, uid, gid);
  } catch (e) {}
  const entries = await listEntries(dirPath);
  if (!entries) return;

  for (const ent of entries) {
    const fullPath = `${dirPath}/${ent.name}`;
    try {
      await tjs.chown(fullPath, uid, gid);
    } catch (e) {}
    if (ent.isDir) {
      await chownRecursive(fullPath, uid, gid);
    }
  }
}

/** 获取系统架构（优先使用纯 JS 读取 /proc/cpuinfo 与 tjs.system，降级使用 uname -m） */
async function getArch() {
  try {
    const cpuinfo = new TextDecoder().decode(await tjs.readFile('/proc/cpuinfo'));
    if (cpuinfo.includes('aarch64') || cpuinfo.includes('ARM') || cpuinfo.includes('Architecture: 8')) {
      return 'arm64';
    }
  } catch (e) {}

  try {
    const model = tjs.system?.cpus?.[0]?.model || '';
    if (model.includes('ARM') || model.includes('Cortex') || model.includes('aarch64')) {
      return 'arm64';
    }
  } catch (e) {}

  try {
    const { stdout } = await runCmd(['uname', '-m']);
    const m = stdout.trim();
    return (m === 'aarch64' || m === 'arm64') ? 'arm64' : 'amd64';
  } catch (e) {
    return 'amd64';
  }
}

/** 下载并安装 transmission-daemon 二进制 */
async function installBinary(latest, arch) {
  const asset = latest.assets.find(a => a.name.includes(arch) && a.name.endsWith('.tar.xz'));
  if (!asset) throw new Error(`未找到 ${arch} 架构的下载包`);

  console.log(`::progress:: 40 "下载二进制 v${latest.version}..."`);
  const tmpPath = '/tmp/transmission-update.tar.xz';
  await downloadFile(asset.browser_download_url, tmpPath);

  console.log('::progress:: 60 "解压二进制..."');
  const extractDir = '/tmp/transmission-extract';
  try { await tjs.remove(extractDir); } catch (e) {}
  await tjs.makeDir(extractDir);
  await extractArchive(tmpPath, extractDir, 'tar.xz');

  // 用 tjs.readDir 递归查找二进制（替代 find 命令）
  // 匹配 transmission-daemon 及其带版本/架构后缀形式（如 transmission-daemon-4.1.3-amd64），
  // 并排除 .b2sum/.sig/.sha256 等校验/归档扩展名（原 /^transmission-daemon$/ 过严，.*[^.]$ 过宽）
  const binPath = await findFile(extractDir, (name) => {
    if (!name.startsWith('transmission-daemon')) return false;
    return !/\.(b2sum|sig|sha256|sha512|txt|md|tar|xz|zip|json|asc|sha)$/.test(name);
  });
  if (!binPath) throw new Error('压缩包中未找到 transmission-daemon 二进制');

  // 替换旧二进制
  const targetPath = `${serviceDir}/bin/transmission-daemon`;
  try { await tjs.makeDir(`${serviceDir}/bin`); } catch (e) {}
  try { await tjs.rename(targetPath, `${targetPath}.bak`); } catch (e) {}
  await tjs.copyFile(binPath, targetPath);
  await tjs.chmod(targetPath, 0o755);

  await tjs.remove(tmpPath);
  await tjs.remove(extractDir);
  return await getCurrentVersion();
}

/** 下载并安装 trwm WebUI */
async function installWebUI() {
  console.log('::progress:: 70 "下载 WebUI..."');
  const { assets } = await getLatestRelease(WEBUI_API);
  const asset = assets.find(a => a.name.endsWith('.zip'));
  if (!asset) throw new Error('未找到 WebUI zip 包');

  const tmpPath = '/tmp/trwm-update.zip';
  await downloadFile(asset.browser_download_url, tmpPath);

  console.log('::progress:: 80 "解压 WebUI..."');
  const extractDir = '/tmp/trwm-extract';
  try { await tjs.remove(extractDir); } catch (e) {}
  await tjs.makeDir(extractDir);
  await extractArchive(tmpPath, extractDir, 'zip');

  // 用 tjs.readDir 递归查找 index.html（替代 find 命令）
  const indexHtml = await findFile(extractDir, /^index\.html$/);
  const webSrc = indexHtml ? indexHtml.replace(/\/index\.html$/, '') : extractDir;

  // 用 tjs 递归复制到 web/ 目录（替代 cp -r）
  const webDir = `${serviceDir}/web`;
  try { await tjs.remove(webDir); } catch (e) {}
  await copyDir(webSrc, webDir);

  await tjs.remove(tmpPath);
  await tjs.remove(extractDir);
}

/** 创建必要目录并设置 nobody 属主 */
async function setupDirectories() {
  console.log('::progress:: 85 "创建目录并设置权限..."');
  // config/ 是 Transmission 配置目录（TRANSMISSION_HOME），存放 settings.json/torrents/resume/blocklists
  const dirs = ['config', 'downloads', 'incomplete', 'web'];
  for (const d of dirs) {
    const p = `${serviceDir}/${d}`;
    try { await tjs.makeDir(p); } catch (e) {}
  }
  // 若 config/settings.json 不存在，创建默认配置
  const settingsPath = `${serviceDir}/config/settings.json`;
  try {
    await tjs.stat(settingsPath);
  } catch (e) {
    const defaultSettings = {
      'download-dir': `${serviceDir}/downloads`,
      'incomplete-dir': `${serviceDir}/incomplete`,
      'incomplete-dir-enabled': true,
      'rpc-enabled': true,
      'rpc-port': 9091,
      'rpc-bind-address': '0.0.0.0',
      'rpc-url': '/transmission/',
      'rpc-authentication-required': false,
      'rpc-whitelist-enabled': false,
      'rpc-host-whitelist-enabled': false,
      'peer-port': 51413,
      'dht-enabled': true,
      'pex-enabled': true,
      'lpd-enabled': true,
      'utp-enabled': true,
      'encryption': 1,
      'cache-size-mb': 10,
      'start-added-torrents': true,
      'rename-partial-files': true,
      'watch-dir-enabled': false,
    };
    const encoder = new TextEncoder();
    await tjs.writeFile(settingsPath, encoder.encode(JSON.stringify(defaultSettings, null, 2)));
    console.log('已创建默认 config/settings.json');
  }
  
  // 校验与递归改属主 (nobody:nobody -> uid 65534, gid 65534)
  // 当扩展以非特权身份运行（如 run_as_uid: 1000）时，chown 会因 EPERM 失败。
  // 此时跳过 chown（不阻断流程），依赖 pre-start-fixperms 扩展（root, pre_start 生命周期）修复权限。
  let chownOk = false;
  try {
    await chownRecursive(serviceDir, 65534, 65534);
    chownOk = true;
  } catch (e) {
    const chownResult = await runCmd(['chown', '-R', 'nobody:nobody', serviceDir]);
    if (chownResult.exitCode === 0) {
      chownOk = true;
    }
  }
  if (!chownOk) {
    console.log('⚠ chown 跳过（非 root 身份无权修改属主），将由 pre-start-fixperms 扩展在服务启动前修复为 nobody 属主');
  }
}

// --- Action: 检查更新 ---
async function doCheck() {
  console.log('::progress:: 20 "获取当前版本..."');
  const current = await getCurrentVersion();
  console.log(`当前版本: ${current}`);

  console.log('::progress:: 50 "查询 GitHub 最新版本..."');
  const { version: latest } = await getLatestRelease(BIN_API);
  console.log(`最新版本: ${latest}`);

  console.log('::progress:: 90 "版本比对完成"');
  if (current === latest) {
    console.log(`::result:: success "已是最新版本 v${current}"`);
  } else if (current === 'not-installed') {
    console.log(`::result:: warning "二进制未安装，最新版本 v${latest}。请点击「安装/更新」"`);
  } else {
    console.log(`::result:: warning "发现新版本：v${current} → v${latest}"`);
  }
}

// --- Action: 安装/更新 ---
async function doInstall() {
  console.log('::progress:: 10 "查询最新版本..."');
  const latest = await getLatestRelease(BIN_API);
  if (!latest.version || latest.version === 'unknown') {
    console.log('::result:: error "无法获取最新版本号"'); tjs.exit(1); return;
  }
  console.log(`目标版本: v${latest.version}`);

  console.log('::progress:: 20 "确定系统架构..."');
  const arch = await getArch();
  console.log(`架构: ${arch}`);

  console.log('::progress:: 30 "安装二进制..."');
  const newVer = await installBinary(latest, arch);
  console.log(`二进制版本: v${newVer}`);

  await installWebUI();
  await setupDirectories();

  // 通过 supd API 重启服务
  console.log('::progress:: 95 "重启服务..."');
  const svc = tjs.env.SUPD_SERVICE || 'transmission';
  try {
    const resp = await fetch(`http://127.0.0.1:7979/api/services/${svc}/restart`, { method: 'POST' });
    if (resp.ok) {
      console.log(`::result:: success "已安装 v${newVer} + WebUI，服务已重启"`);
    } else {
      console.log(`::result:: warning "已安装 v${newVer} + WebUI，重启返回 HTTP ${resp.status}，请手动启动"`);
    }
  } catch (e) {
    console.log(`::result:: warning "已安装 v${newVer} + WebUI，重启失败: ${e.message}，请手动启动"`);
  }
}

// --- 主入口 ---
console.log(`[transmission-updater] action=${action}`);
try {
  switch (action) {
    case 'check-update': await doCheck(); break;
    case 'install-update': await doInstall(); break;
    default: console.log(`::result:: error "未知 action: ${action}"`); tjs.exit(1);
  }
} catch (e) {
  console.log(`::result:: error "执行失败: ${e.message}"`);
  console.error(e.stack || e.message);
  tjs.exit(1);
}

