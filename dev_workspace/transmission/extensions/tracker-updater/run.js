// run.js — tracker-updater (tjs runtime)
// 通过 Transmission RPC 更新全局默认 Tracker 列表（session-set + default_trackers）
// 新添加的种子自动获得这些 Tracker，无需逐 torrent 更新。
// 数据源：https://github.com/adysec/tracker (trackers_best.txt)

const TRACKER_URL = 'https://raw.githubusercontent.com/adysec/tracker/main/trackers_best.txt';

const action = tjs.env.SUPD_ACTION || 'update-trackers';
const serviceDir = tjs.env.SUPD_SERVICE_DIR || '';

// --- 读取 Transmission RPC 配置 ---
// 从 settings.json 读取 rpc-port / rpc-bind-address / rpc-url
// 文件不存在或解析失败时回退到默认端口 9091

async function readRPCConfig() {
  const defaults = { rpcUrl: 'http://127.0.0.1:9091/transmission/rpc' };
  if (!serviceDir) return defaults;

  const configPath = serviceDir + '/config/settings.json';
  try {
    const content = await tjs.readFile(configPath);
    const text = typeof content === 'string' ? content : new TextDecoder().decode(content);
    const settings = JSON.parse(text);

    const port = settings['rpc-port'] || 9091;
    // 0.0.0.0 表示监听所有接口，本地连接用 127.0.0.1
    const bindAddr = settings['rpc-bind-address'] || '127.0.0.1';
    const host = bindAddr === '0.0.0.0' ? '127.0.0.1' : bindAddr;
    const rpcUrlPath = (settings['rpc-url'] || '/transmission/') + 'rpc';

    return { rpcUrl: 'http://' + host + ':' + port + rpcUrlPath };
  } catch (e) {
    console.log('读取 settings.json 失败（' + e.message + '），使用默认 RPC 端口 9091');
    return defaults;
  }
}

// --- Transmission RPC 请求（自动处理 409 CSRF） ---
async function transRpc(rpcUrl, method, arguments_, sessionId) {
  const headers = { 'Content-Type': 'application/json' };
  if (sessionId) {
    headers['X-Transmission-Session-Id'] = sessionId;
  }

  const body = JSON.stringify({ method, arguments: arguments_ || {} });

  let resp = await fetch(rpcUrl, { method: 'POST', headers, body });

  // 409 = CSRF 保护，需携带 session-id 重试
  if (resp.status === 409) {
    const newSessionId = resp.headers.get('X-Transmission-Session-Id');
    if (!newSessionId) {
      throw new Error('RPC 返回 409 但未携带 X-Transmission-Session-Id 头');
    }
    headers['X-Transmission-Session-Id'] = newSessionId;
    resp = await fetch(rpcUrl, { method: 'POST', headers, body });
  }

  if (resp.status === 401) {
    throw new Error('RPC 认证失败（401），请检查 settings.json 中的 rpc-authentication-required 设置');
  }

  if (!resp.ok) {
    throw new Error('RPC HTTP ' + resp.status + ': ' + resp.statusText);
  }

  const data = await resp.json();
  if (data.result !== 'success') {
    throw new Error('RPC 返回错误: ' + data.result);
  }

  return { data, sessionId: headers['X-Transmission-Session-Id'] };
}

// --- Action: 更新全局 Tracker ---
async function updateTrackers() {
  // 1. 读取 RPC 配置（从 settings.json 或使用默认值）
  console.log('::progress:: 5 "正在读取 Transmission RPC 配置..."');
  const rpcConfig = await readRPCConfig();
  const RPC_URL = rpcConfig.rpcUrl;
  console.log('RPC 地址: ' + RPC_URL);

  // 2. 获取 Tracker 列表
  console.log('::progress:: 20 "正在获取 Tracker 列表..."');
  const trackerResp = await fetch(TRACKER_URL, {
    headers: { 'User-Agent': 'supd-tjs-ext' },
  });
  if (!trackerResp.ok) {
    console.log('::result:: error "获取 Tracker 列表失败: HTTP ' + trackerResp.status + '"');
    tjs.exit(1);
    return;
  }
  const trackerList = (await trackerResp.text()).trim();
  const trackerCount = trackerList.split('\n').filter(l => l.trim()).length;
  console.log('已获取 ' + trackerCount + ' 个 Tracker');

  // 3. 连接 Transmission RPC，获取 session-id
  console.log('::progress:: 40 "正在连接 Transmission RPC..."');
  let rpcResult = await transRpc(RPC_URL, 'session-get', {});
  const sessionId = rpcResult.sessionId;
  console.log('RPC 连接成功');

  // 4. 设置全局默认 Tracker 列表（session-set + default_trackers）
  // default_trackers 格式：每行一个 announce URL，tier 之间空行分隔（BEP 12）
  console.log('::progress:: 70 "正在更新全局 Tracker 列表..."');
  rpcResult = await transRpc(RPC_URL, 'session-set', { default_trackers: trackerList }, sessionId);

  // 5. 统计协议分布
  const protocols = {
    udp: (trackerList.match(/^udp:/gm) || []).length,
    http: (trackerList.match(/^http:/gm) || []).length,
    https: (trackerList.match(/^https:/gm) || []).length,
    wss: (trackerList.match(/^wss:/gm) || []).length,
  };
  const protoSummary = Object.entries(protocols)
    .filter(([, v]) => v > 0)
    .map(([k, v]) => k + '×' + v)
    .join(' ');

  console.log('::progress:: 100 "更新完成"');
  console.log('::result:: success "已更新全局 Tracker 列表（' + trackerCount + ' 个：' + protoSummary + '），新种子将自动获得这些 Tracker"');
}

// --- 主入口 ---
console.log('[tracker-updater] action=' + action);

try {
  switch (action) {
    case 'update-trackers':
      await updateTrackers();
      break;
    default:
      console.log('::result:: error "未知 action: ' + action + '"');
      tjs.exit(1);
  }
} catch (e) {
  console.log('::result:: error "执行失败: ' + e.message + '"');
  console.error(e.stack || e.message);
  tjs.exit(1);
}
