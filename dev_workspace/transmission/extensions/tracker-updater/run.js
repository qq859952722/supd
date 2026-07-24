// run.js — tracker-updater (tjs runtime)
// 定时通过 Transmission RPC 更新所有种子的 Tracker 列表
// 数据源：https://github.com/adysec/tracker (trackers_best.txt)

const TRACKER_URL = 'https://raw.githubusercontent.com/adysec/tracker/main/trackers_best.txt';
const RPC_URL = 'http://127.0.0.1:9091/transmission/rpc';

const action = tjs.env.SUPD_ACTION || 'update-trackers';

// --- 工具函数 ---

/** Transmission RPC 请求（自动处理 409 CSRF） */
async function transRpc(method, arguments_, sessionId) {
  const headers = { 'Content-Type': 'application/json' };
  if (sessionId) {
    headers['X-Transmission-Session-Id'] = sessionId;
  }

  const body = JSON.stringify({ method, arguments: arguments_ || {} });

  let resp = await fetch(RPC_URL, { method: 'POST', headers, body });

  // 409 = CSRF 保护，需携带 session-id 重试
  if (resp.status === 409) {
    const newSessionId = resp.headers.get('X-Transmission-Session-Id');
    if (!newSessionId) {
      throw new Error('RPC 返回 409 但未携带 X-Transmission-Session-Id 头');
    }
    headers['X-Transmission-Session-Id'] = newSessionId;
    resp = await fetch(RPC_URL, { method: 'POST', headers, body });
  }

  if (resp.status === 401) {
    throw new Error('RPC 认证失败（401），请检查 settings.json 中的 rpc-authentication-required 设置');
  }

  if (!resp.ok) {
    throw new Error(`RPC HTTP ${resp.status}: ${resp.statusText}`);
  }

  const data = await resp.json();
  if (data.result !== 'success') {
    throw new Error(`RPC 返回错误: ${data.result}`);
  }

  return { data, sessionId: headers['X-Transmission-Session-Id'] };
}

// --- Action: 更新 Tracker ---
async function updateTrackers() {
  // 1. 获取 Tracker 列表
  console.log('::progress:: 10 "正在获取 Tracker 列表..."');
  const trackerResp = await fetch(TRACKER_URL, {
    headers: { 'User-Agent': 'supd-tjs-ext' },
  });
  if (!trackerResp.ok) {
    console.log(`::result:: error "获取 Tracker 列表失败: HTTP ${trackerResp.status}"`);
    tjs.exit(1);
    return;
  }
  const trackerList = (await trackerResp.text()).trim();
  const trackerCount = trackerList.split('\n').filter(l => l.trim()).length;
  console.log(`已获取 ${trackerCount} 个 Tracker`);

  // 2. 连接 Transmission RPC，获取 session-id
  console.log('::progress:: 30 "正在连接 Transmission RPC..."');
  let rpcResult = await transRpc('session-get', {});
  const sessionId = rpcResult.sessionId;
  console.log('RPC 连接成功');

  // 3. 获取所有种子 ID
  console.log('::progress:: 50 "正在获取种子列表..."');
  rpcResult = await transRpc('torrent-get', { fields: ['id', 'name'] }, sessionId);
  const torrents = rpcResult.data.arguments.torrents || [];
  console.log(`共 ${torrents.length} 个种子`);

  if (torrents.length === 0) {
    console.log('::result:: success "无种子需要更新（Tracker 列表已缓存，添加种子时将自动应用）"');
    return;
  }

  // 4. 为所有种子设置 Tracker 列表
  console.log(`::progress:: 70 "正在更新 ${torrents.length} 个种子的 Tracker..."`);
  const ids = torrents.map(t => t.id);
  rpcResult = await transRpc('torrent-set', { ids, trackerList }, sessionId);

  // 5. 统计支持的协议
  const protocols = {
    udp: (trackerList.match(/^udp:/gm) || []).length,
    http: (trackerList.match(/^http:/gm) || []).length,
    https: (trackerList.match(/^https:/gm) || []).length,
    wss: (trackerList.match(/^wss:/gm) || []).length,
  };
  const protoSummary = Object.entries(protocols)
    .filter(([, v]) => v > 0)
    .map(([k, v]) => `${k}×${v}`)
    .join(' ');

  console.log('::progress:: 100 "更新完成"');
  console.log(`::result:: success "已更新 ${torrents.length} 个种子的 Tracker（${trackerCount} 个：${protoSummary}）"`);
}

// --- 主入口 ---
console.log(`[tracker-updater] action=${action}`);

try {
  switch (action) {
    case 'update-trackers':
      await updateTrackers();
      break;
    default:
      console.log(`::result:: error "未知 action: ${action}"`);
      tjs.exit(1);
  }
} catch (e) {
  console.log(`::result:: error "执行失败: ${e.message}"`);
  console.error(e.stack || e.message);
  tjs.exit(1);
}
