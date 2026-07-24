// run.js — transmission-updater (tjs runtime)
// 检测并更新 transmission-daemon 二进制 + trwm WebUI
// 数据源：https://github.com/qq859952722/transmission-builder/releases
//         https://github.com/qq859952722/transmission_web_manager/releases

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
async function runCmd(args, options = {}) {
  const proc = await tjs.spawn(args, {
    stdout: 'pipe',
    stderr: 'pipe',
    ...options,
  });
  const stdout = await readStream(proc.stdout);
  const stderr = await readStream(proc.stderr);
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

/** 递归查找文件（用 tjs.readDir 替代 find 命令） */
async function findFile(dir, pattern) {
  let entries;
  try {
    entries = await tjs.readDir(dir);
  } catch (e) {
    return null; // 非目录
  }
  for (const entry of entries) {
    const name = typeof entry === 'string' ? entry : entry.name;
    const fullPath = `${dir}/${name}`;
    if (pattern.test(name)) return fullPath;
    const found = await findFile(fullPath, pattern); // 递归子目录
    if (found) return found;
  }
  return null;
}

/** 递归复制目录（用 tjs.readDir + tjs.copyFile 替代 cp -r） */
async function copyDir(src, dst) {
  await tjs.makeDir(dst);
  const entries = await tjs.readDir(src);
  for (const entry of entries) {
    const name = typeof entry === 'string' ? entry : entry.name;
    const srcPath = `${src}/${name}`;
    const dstPath = `${dst}/${name}`;
    let isDir = false;
    try { (await tjs.readDir(srcPath)); isDir = true; } catch (e) {}
    if (isDir) {
      await copyDir(srcPath, dstPath);
    } else {
      await tjs.copyFile(srcPath, dstPath);
    }
  }
}

// --- 业务函数 ---

/** 获取当前已安装版本 */
async function getCurrentVersion() {
  try { await tjs.stat(`${serviceDir}/transmission-daemon`); } catch (e) { return 'not-installed'; }
  const { stdout } = await runCmd([`${serviceDir}/transmission-daemon`, '--version']);
  const m = stdout.match(/(\d+\.\d+\.\d+)/);
  return m ? m[1] : 'unknown';
}

/** 查询 GitHub Releases 最新版本 */
async function getLatestRelease(apiUrl) {
  const resp = await fetch(apiUrl, { headers: { 'User-Agent': 'supd-tjs-ext' } });
  if (!resp.ok) throw new Error(`GitHub API HTTP ${resp.status}`);
  const data = await resp.json();
  const version = (data.tag_name || '').replace(/.*-/, '') || 'unknown';
  return { version, assets: data.assets || [] };
}

/** 获取系统架构（uname -m 是必要外部依赖，tjs 无内置替代） */
async function getArch() {
  const { stdout } = await runCmd(['uname', '-m']);
  const m = stdout.trim();
  return (m === 'aarch64' || m === 'arm64') ? 'arm64' : 'amd64';
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
  await runCmd(['tar', '-xf', tmpPath, '-C', extractDir]); // tar 是必要依赖

  // 用 tjs.readDir 递归查找二进制（替代 find 命令）
  const binPath = await findFile(extractDir, /^transmission-daemon.*[^.]$/);
  if (!binPath) throw new Error('压缩包中未找到 transmission-daemon 二进制');

  // 替换旧二进制
  const targetPath = `${serviceDir}/transmission-daemon`;
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
  await runCmd(['unzip', '-o', tmpPath, '-d', extractDir]); // unzip 是必要依赖

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
  // chown -R 是必要依赖（tjs.chown 不支持递归）
  await runCmd(['chown', '-R', 'nobody:nobody', serviceDir]);
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
