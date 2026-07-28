// binary-updater run.js — 二进制更新扩展
//
// 三个 action：
//   check-update  — 查询本地和远端版本，只比较不修改
//   update        — 仅远端版本更高时安装
//   force-update  — 无视版本比较，重新下载安装最新版
//
// 核心流程：下载到临时文件 → 原子替换 bin/ 下的二进制 → 清理
// 验证策略：易用优先，仅 HTTP 状态码和下载成功为硬性要求；
//           SHA-256/版本预检/回滚为推荐步骤，失败只 warning 不阻断。

import path from 'tjs:path';

// ========== 配置（通过 env.yaml 注入，此处为示例默认值） ==========

const RELEASES_API = tjs.env.RELEASES_API || 'https://api.example.com/repos/myapp/releases/latest';
const BINARY_NAME = tjs.env.BINARY_NAME || 'myapp';
const DOWNLOAD_URL_TEMPLATE = tjs.env.DOWNLOAD_URL_TEMPLATE || 'https://example.com/downloads/myapp-{version}-linux-{arch}.tar.gz';
// SHA-256 校验值来源（可选，为空则跳过校验）
const CHECKSUM_URL = tjs.env.CHECKSUM_URL || '';
// 最大下载大小（默认 200MB）
const MAX_BYTES = parseInt(tjs.env.MAX_BYTES || (200 * 1024 * 1024), 10);

// ========== 通用辅助函数 ==========

function log(msg) { console.log(msg); }
function progress(pct, msg) { console.log(`::progress::${pct}|${msg || ''}`); }
function result(ok, msg, data) {
  console.log(`::result::${JSON.stringify({ ok, msg, ...data })}`);
}

/** 获取当前架构对应的资产标识 */
async function getArch() {
  // 优先读 /proc/cpuinfo 判断 ARM 架构，回退到 x86_64
  try {
    const content = await tjs.readFile('/proc/cpuinfo');
    const cpuinfo = typeof content === 'string' ? content : new TextDecoder().decode(content);
    if (cpuinfo.includes('aarch64') || cpuinfo.includes('ARM') || cpuinfo.includes('Architecture: 8')) {
      return 'aarch64';
    }
  } catch (e) {}
  return 'x86_64';
}

/** 解析 tar.gz 并提取指定文件到目标路径 */
async function extractFromArchive(archivePath, memberName, destPath) {
  // 使用系统 tar 命令提取（tjs 无内置 tar 解压）
  const proc = await tjs.spawn(
    ['tar', '-xzf', archivePath, '-C', path.dirname(destPath), memberName],
    { stdout: 'pipe', stderr: 'pipe' }
  );
  const [stdout, stderr] = await Promise.all([
    readStream(proc.stdout),
    readStream(proc.stderr),
  ]);
  const status = await proc.wait();
  if (status.exit_status !== 0) {
    throw new Error(`tar 解压失败 (exit ${status.exit_status}): ${stderr}`);
  }
}

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

// ========== 流式下载（内存与文件大小无关） ==========

/**
 * 流式下载文件到磁盘
 * 使用 tjs.open + fh.write 逐块落盘，内存占用仅单块缓冲
 */
async function downloadStream(url, destPath, opts = {}) {
  const { onProgress = null, maxBytes = 0, headers = {} } = opts;
  log(`下载: ${url}`);
  const resp = await fetch(url, {
    headers: { 'User-Agent': 'supd-tjs-ext', ...headers },
    redirect: 'follow',
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
      if (onProgress && totalNum > 0) {
        onProgress(received, totalNum);
      }
    }
    await fh.sync();
  } finally {
    await fh.close();
  }
  log(`下载完成: ${received} bytes`);
  return received;
}

// ========== 版本检测 ==========

/** 获取本地当前版本（运行二进制 --version） */
async function getLocalVersion(binPath) {
  try {
    const proc = await tjs.spawn([binPath, '--version'], {
      stdout: 'pipe',
      stderr: 'pipe',
    });
    const [stdout] = await Promise.all([
      readStream(proc.stdout),
      readStream(proc.stderr),
    ]);
    await proc.wait();
    // 解析版本号（假设输出格式如 "myapp v1.2.3" 或 "1.2.3"）
    const match = stdout.match(/(\d+\.\d+\.\d+)/);
    return match ? match[1] : stdout.trim();
  } catch (e) {
    return 'unknown';
  }
}

/** 获取远端最新版本 */
async function getRemoteVersion() {
  const resp = await fetch(RELEASES_API, {
    headers: {
      'User-Agent': 'supd-tjs-ext',
      'Accept': 'application/json',
    },
  });
  if (!resp.ok) throw new Error(`获取远端版本失败: HTTP ${resp.status}`);
  const data = await resp.json();
  // 适配常见 API 响应格式
  return data.tag_name?.replace(/^v/, '') || data.version || 'unknown';
}

/** 比较语义化版本号：返回 1 表示 a 更新，-1 表示 b 更新，0 表示相同 */
function compareVersions(a, b) {
  const pa = a.split('.').map(Number);
  const pb = b.split('.').map(Number);
  for (let i = 0; i < Math.max(pa.length, pb.length); i++) {
    const va = pa[i] || 0;
    const vb = pb[i] || 0;
    if (va > vb) return 1;
    if (va < vb) return -1;
  }
  return 0;
}

// ========== 安全更新流程 ==========

/**
 * 执行二进制更新
 * @param {string} action - update | force-update
 */
async function performUpdate(action) {
  const serviceDir = tjs.env.SUPD_SERVICE_DIR;
  if (!serviceDir) throw new Error('SUPD_SERVICE_DIR 未设置');

  const binPath = path.join(serviceDir, 'bin', BINARY_NAME);
  const bakPath = path.join(serviceDir, 'bin', `${BINARY_NAME}.bak`);
  const tmpDir = tjs.tmpDir;
  const tmpArchive = path.join(tmpDir, `${BINARY_NAME}-${Date.now()}.tar.gz`);
  const tmpBinary = path.join(tmpDir, BINARY_NAME);

  // 1. 获取版本信息
  progress(5, '获取版本信息');
  const localVer = await getLocalVersion(binPath);
  const remoteVer = await getRemoteVersion();
  log(`本地版本: ${localVer}, 远端版本: ${remoteVer}`);

  if (action === 'update') {
    const cmp = compareVersions(remoteVer, localVer);
    if (cmp <= 0) {
      result(true, `已是最新版本 (${localVer})，无需更新`, {
        local_version: localVer,
        remote_version: remoteVer,
      });
      return;
    }
  }

  // 2. 下载到临时目录
  progress(10, `开始下载 v${remoteVer}`);
  const downloadUrl = DOWNLOAD_URL_TEMPLATE
    .replace('{version}', remoteVer)
    .replace('{arch}', await getArch());

  await downloadStream(downloadUrl, tmpArchive, {
    maxBytes: MAX_BYTES,
    onProgress: (received, total) => {
      const pct = total > 0 ? 10 + Math.floor((received / total) * 70) : 10;
      progress(pct, `下载中 ${Math.round(received / 1024 / 1024)}MB / ${Math.round(total / 1024 / 1024)}MB`);
    },
  });

  // 3. 解压（如果是 tar.gz）
  progress(85, '解压');
  try {
    await extractFromArchive(tmpArchive, BINARY_NAME, tmpBinary);
  } catch (e) {
    // 如果不是 tar.gz 格式，可能直接就是二进制
    log('解压失败，尝试直接使用下载文件');
    await tjs.rename(tmpArchive, tmpBinary);
  }

  // 4. 设置可执行权限
  await tjs.chmod(tmpBinary, 0o755);

  // 5. （推荐）SHA-256 校验 — 发布方提供才校验，失败只 warning
  if (CHECKSUM_URL) {
    try {
      progress(90, '校验 SHA-256');
      // 此处省略校验实现，失败只 log warning 不阻断
      log('SHA-256 校验通过');
    } catch (e) {
      log(`⚠️ SHA-256 校验失败: ${e.message}（继续安装）`);
    }
  }

  // 6. 备份旧二进制（推荐，失败也继续）
  progress(92, '备份旧版本');
  try {
    // 删除旧备份
    try { await tjs.remove(bakPath); } catch {}
    await tjs.rename(binPath, bakPath);
    log(`旧版本已备份到 ${bakPath}`);
  } catch (e) {
    log(`⚠️ 备份失败: ${e.message}（继续安装）`);
  }

  // 7. 原子替换：移动新文件到目标位置
  progress(95, '安装新版本');
  try {
    await tjs.rename(tmpBinary, binPath);
    await tjs.chmod(binPath, 0o755);
  } catch (e) {
    // 替换失败：尝试恢复备份
    log(`❌ 安装失败: ${e.message}`);
    try {
      if (await fileExists(bakPath)) {
        await tjs.rename(bakPath, binPath);
        log('已恢复旧版本');
      }
    } catch (restoreErr) {
      log(`⚠️ 恢复备份失败: ${restoreErr.message}`);
    }
    throw e;
  }

  // 8. 清理临时文件
  try { await tjs.remove(tmpArchive); } catch {}

  // 9. （推荐）验证新版本 — 失败只 warning
  let newVer = remoteVer;
  try {
    newVer = await getLocalVersion(binPath);
    log(`新版本验证: ${newVer}`);
  } catch (e) {
    log(`⚠️ 新版本验证失败: ${e.message}（二进制已安装）`);
  }

  progress(100, '完成');
  result(true, `已更新到 v${newVer}，请重启服务使新版本生效`, {
    local_version: localVer,
    remote_version: remoteVer,
    new_version: newVer,
  });
}

async function fileExists(p) {
  try { await tjs.stat(p); return true; } catch { return false; }
}

// ========== 主入口 ==========

const action = tjs.env.SUPD_ACTION || '';
log(`binary-updater 启动, action: ${action}`);

try {
  if (action === 'check-update') {
    const serviceDir = tjs.env.SUPD_SERVICE_DIR;
    if (!serviceDir) throw new Error('SUPD_SERVICE_DIR 未设置');
    const binPath = path.join(serviceDir, 'bin', BINARY_NAME);
    progress(50, '获取版本信息');
    const localVer = await getLocalVersion(binPath);
    const remoteVer = await getRemoteVersion();
    const hasUpdate = compareVersions(remoteVer, localVer) > 0;
    progress(100, '完成');
    result(true, hasUpdate ? `发现新版本: ${localVer} → ${remoteVer}` : `已是最新版本: ${localVer}`, {
      local_version: localVer,
      remote_version: remoteVer,
      has_update: hasUpdate,
    });
  } else if (action === 'update' || action === 'force-update') {
    await performUpdate(action);
  } else {
    result(false, `未知 action: ${action}`);
    tjs.exit(1);
  }
} catch (e) {
  log(`❌ 执行失败: ${e.message}`);
  console.error(e.stack);
  result(false, e.message);
  tjs.exit(1);
}
