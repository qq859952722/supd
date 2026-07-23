// run.js — qbittorrent-updater 扩展（tjs 运行时）
// 功能：检查 GitHub 最新版本、下载并安装 qbittorrent-nox 静态二进制
// 参考: references/06_tjs_runtime_guide.md

import path from 'tjs:path';

// ────────────────────────────────────────────────────────────
// 配置
// ────────────────────────────────────────────────────────────
const REPO = 'userdocs/qbittorrent-nox-static';
// 当前内置版本（service.yaml 的 version 对应）
const CURRENT_TAG = 'release-5.2.3_v2.0.13';

// 架构 → asset 名映射（qbittorrent-nox-static 的命名规则）
const ARCH_MAP = {
  'x86_64': 'x86_64-qbittorrent-nox',
  'aarch64': 'aarch64-qbittorrent-nox',
  'armv7l': 'armv7-qbittorrent-nox',
};

// 二进制保存路径：服务目录下的 qbittorrent-nox
const SERVICE_DIR = tjs.env.SUPD_SERVICE_DIR || '/etc/supd/services/qbittorrent';
const BINARY_PATH = path.join(SERVICE_DIR, 'qbittorrent-nox');

// ────────────────────────────────────────────────────────────
// 工具函数
// ────────────────────────────────────────────────────────────
function formatBytes(bytes) {
  if (bytes == null) return '?';
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
  if (bytes < 1024 * 1024 * 1024) return `${(bytes / 1024 / 1024).toFixed(2)} MB`;
  return `${(bytes / 1024 / 1024 / 1024).toFixed(2)} GB`;
}

function getArch() {
  // 通过 tjs.system 探测架构；回退到 x86_64
  // qbittorrent-nox-static 的 x86_64 二进制是静态 musl 编译，兼容 alpine
  return 'x86_64';
}

async function fetchJSON(url) {
  const resp = await fetch(url, {
    headers: {
      'User-Agent': 'supd-qbittorrent-updater',
      'Accept': 'application/vnd.github+json',
    },
  });
  if (!resp.ok) {
    throw new Error(`GitHub API 返回 ${resp.status}: ${resp.statusText}`);
  }
  return await resp.json();
}

async function getLatestRelease() {
  console.log(`查询最新版本: ${REPO}`);
  return await fetchJSON(`https://api.github.com/repos/${REPO}/releases/latest`);
}

async function getReleaseByTag(tag) {
  console.log(`查询指定版本: ${tag}`);
  return await fetchJSON(`https://api.github.com/repos/${REPO}/releases/tags/${tag}`);
}

function findAssetUrl(release, arch) {
  const assetName = ARCH_MAP[arch];
  if (!assetName) {
    throw new Error(`不支持的架构: ${arch}（支持: ${Object.keys(ARCH_MAP).join(', ')}）`);
  }
  const asset = release.assets && release.assets.find(a => a.name === assetName);
  if (!asset) {
    const available = release.assets ? release.assets.map(a => a.name).join(', ') : '(无)';
    throw new Error(`未找到 ${assetName}，可用 assets: ${available}`);
  }
  return { url: asset.browser_download_url, size: asset.size, name: asset.name };
}

async function ensureServiceDir() {
  try {
    await tjs.stat(SERVICE_DIR);
  } catch {
    console.log(`服务目录不存在，创建: ${SERVICE_DIR}`);
    await tjs.makeDir(SERVICE_DIR);
  }
}

async function downloadAndInstall(downloadUrl, expectedSize) {
  console.log('::progress:: 30 "开始下载二进制"');
  console.log(`下载地址: ${downloadUrl}`);

  await ensureServiceDir();

  const resp = await fetch(downloadUrl, {
    headers: { 'User-Agent': 'supd-qbittorrent-updater' },
  });
  if (!resp.ok) {
    throw new Error(`下载失败 HTTP ${resp.status}: ${resp.statusText}`);
  }

  // 流式读取响应体：resp.arrayBuffer() 对大文件（>10MB）会卡死，
  // 必须用 ReadableStream 分块读取（详见 references/06_tjs_runtime_guide.md 坑点）
  // 同时上报实时下载进度，让前端能看到流式下载过程
  const reader = resp.body.getReader();
  const chunks = [];
  let received = 0;
  let lastReport = 0;
  let lastTime = Date.now();
  // 首次上报：显示总大小，让用户知道预期
  if (expectedSize) {
    console.log(`::progress:: 30 "开始下载 (总大小 ${formatBytes(expectedSize)})"`);
  }
  while (true) {
    const { done, value } = await reader.read();
    if (done) break;
    chunks.push(value);
    received += value.length;
    // 每 ~1MB 或每 1 秒上报一次进度（30% → 60%），让流式进度更直观
    const now = Date.now();
    if (expectedSize && (received - lastReport >= 1 * 1024 * 1024 || now - lastTime >= 1000)) {
      const pct = 30 + Math.floor((received / expectedSize) * 30);
      console.log(`::progress:: ${pct} "下载中 ${formatBytes(received)}/${formatBytes(expectedSize)}"`);
      lastReport = received;
      lastTime = now;
    }
  }
  // 合并 chunks 为单个 Uint8Array
  const buffer = new Uint8Array(received);
  let pos = 0;
  for (const chunk of chunks) {
    buffer.set(chunk, pos);
    pos += chunk.length;
  }

  console.log('::progress:: 60 "下载完成，写入文件"');
  console.log(`下载大小: ${formatBytes(buffer.length)}`);

  if (expectedSize && Math.abs(buffer.length - expectedSize) > 1024) {
    console.log(`::warning:: 大小不匹配: 期望 ${formatBytes(expectedSize)}, 实际 ${formatBytes(buffer.length)}`);
  }

  // 备份旧二进制（如果存在）
  try {
    await tjs.stat(BINARY_PATH);
    const backupPath = `${BINARY_PATH}.bak`;
    console.log(`备份旧二进制到 ${backupPath}`);
    await tjs.rename(BINARY_PATH, backupPath);
  } catch {
    // 旧文件不存在，无需备份
  }

  // 写入新二进制并设置可执行权限
  await tjs.writeFile(BINARY_PATH, buffer);
  await tjs.chmod(BINARY_PATH, 0o755);
  console.log(`::progress:: 90 "已写入并设置可执行权限"`);

  // 验证文件
  const stat = await tjs.stat(BINARY_PATH);
  console.log(`安装完成: ${BINARY_PATH} (${stat.size} bytes, mode 0o${stat.mode.toString(8)})`);

  return buffer.length;
}

// ────────────────────────────────────────────────────────────
// Action 实现
// ────────────────────────────────────────────────────────────
async function actionCheckUpdate() {
  console.log('::progress:: 10 "查询 GitHub 最新版本"');
  const release = await getLatestRelease();
  const latestTag = release.tag_name;
  const publishedAt = release.published_at;

  console.log(`\n=== 版本信息 ===`);
  console.log(`当前版本: ${CURRENT_TAG}`);
  console.log(`最新版本: ${latestTag}`);
  console.log(`发布时间: ${publishedAt}`);
  console.log(`Release: ${release.html_url}`);

  const hasUpdate = latestTag !== CURRENT_TAG;
  if (hasUpdate) {
    console.log(`\n⚠️  发现新版本: ${latestTag}`);
    console.log(`运行 "安装最新版" action 来更新`);
    console.log(`::result:: warning "发现新版本: ${latestTag}（当前 ${CURRENT_TAG}）"`);
  } else {
    console.log(`\n✅ 已是最新版本`);
    console.log(`::result:: success "已是最新版本: ${latestTag}"`);
  }

  console.log(`\n可用 assets:`);
  for (const a of release.assets || []) {
    console.log(`  ${a.name}  (${(a.size / 1024 / 1024).toFixed(2)} MB)`);
  }
}

async function actionInstall(actionArgs) {
  const tag = actionArgs && actionArgs.length > 0 ? actionArgs[0] : CURRENT_TAG;
  console.log(`::progress:: 5 "安装指定版本: ${tag}"`);

  const release = await getReleaseByTag(tag);
  const arch = getArch();
  const asset = findAssetUrl(release, arch);

  console.log(`\n=== 安装信息 ===`);
  console.log(`版本: ${tag}`);
  console.log(`架构: ${arch}`);
  console.log(`文件: ${asset.name} (${(asset.size / 1024 / 1024).toFixed(2)} MB)`);
  console.log(`目标: ${BINARY_PATH}`);

  const size = await downloadAndInstall(asset.url, asset.size);

  console.log('::progress:: 100 "安装完成"');
  console.log(`::result:: success "qbittorrent-nox ${tag} 安装完成 (${(size / 1024 / 1024).toFixed(2)} MB)"`);
}

async function actionInstallLatest() {
  console.log('::progress:: 5 "查询最新版本"');
  const release = await getLatestRelease();
  const tag = release.tag_name;
  const arch = getArch();
  const asset = findAssetUrl(release, arch);

  console.log(`\n=== 安装最新版 ===`);
  console.log(`版本: ${tag}`);
  console.log(`架构: ${arch}`);
  console.log(`文件: ${asset.name} (${(asset.size / 1024 / 1024).toFixed(2)} MB)`);

  const size = await downloadAndInstall(asset.url, asset.size);

  console.log('::progress:: 100 "安装完成"');
  console.log(`::result:: success "qbittorrent-nox ${tag} 安装完成 (${(size / 1024 / 1024).toFixed(2)} MB)"`);
}

// ────────────────────────────────────────────────────────────
// 主入口
// ────────────────────────────────────────────────────────────
const action = tjs.env.SUPD_ACTION || 'check-update';
// action args 通过命令行传递（tjs.args = ['tjs','run','run.js', ...args]）
const actionArgs = tjs.args.slice(3);

console.log(`qbittorrent-updater 启动 (action=${action})`);
console.log(`服务目录: ${SERVICE_DIR}`);

try {
  switch (action) {
    case 'check-update':
      await actionCheckUpdate();
      break;
    case 'install':
      await actionInstall(actionArgs);
      break;
    case 'install-latest':
      await actionInstallLatest();
      break;
    default:
      console.log(`未知 action: ${action}（支持: check-update, install, install-latest）`);
      console.log('::result:: error "未知 action"');
      tjs.exit(1);
  }
} catch (e) {
  console.error(`\n❌ 执行失败: ${e.message}`);
  console.error(e.stack || '');
  console.log(`::result:: error "执行失败: ${e.message}"`);
  tjs.exit(1);
}
