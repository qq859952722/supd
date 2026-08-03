// run.js — tracker-updater (tjs runtime)
// 通过 Transmission RPC 更新全局默认 Tracker 列表（session-set + default_trackers）
// 新添加的种子自动获得这些 Tracker，无需逐 torrent 更新。
//
// v1.3.0 重构：3 源合并 + 本地测速验证 + 分组排序（≤50 组）
//   - 数据源：newtrackon（持续更新，有空行分组）+ ngosang/trackerslist（持续更新）+ adysec/tracker（量大）
//   - 本地测速：HTTP/HTTPS 用 fetch 发送测试 announce；UDP 用 BEP-15 CONNECT 探活
//   - 分组策略：优质 tracker（响应快）每个独立成 tier 置顶；其余合并为 1 个 tier 垫底
//   - 总 tier 数 ≤ 50（MAX_TOP_TIERS=49 + 1 个垫底 tier）
//
// v1.2.0：多数据源合并去重 + 保留空行分组结构（每 tracker 独立成 tier）
// v1.1.x：协议白名单过滤（仅 http/https/udp）
//
// 关于空行分组的语义：
//   Transmission 的 default_trackers 字段用空行分隔 tier，每个 tier 内的 tracker 是同级的。
//   多个 tracker 放同一 tier 时 Transmission 并发联系；多个 tier 时按 tier 顺序尝试。
//   优质 tracker 独立成 tier 置顶 → 优先被联系；其余合并一个 tier 垫底 → 兜底回退。

const TRACKER_SOURCES = [
  {
    url: 'https://newtrackon.com/api/all',
    name: 'newtrackon',
    // newtrackon 持续更新（每 6 小时），每个 tracker 之间已有空行分隔
  },
  {
    url: 'https://raw.githubusercontent.com/ngosang/trackerslist/master/trackers_best.txt',
    name: 'ngosang',
    // ngosang/trackerslist 持续更新（默认分支 master），trackers_best 含空行分组
  },
  {
    url: 'https://raw.githubusercontent.com/adysec/tracker/main/trackers_best.txt',
    name: 'adysec',
    // adysec 量大但扁平，需转换为每行一组
  },
];

// Transmission 支持的 announce 协议白名单（官方文档确认）
const SUPPORTED_SCHEMES = ['http://', 'https://', 'udp://'];

// 测速参数
const TEST_TIMEOUT_MS = 2500;   // 单个 tracker 测速超时（毫秒）
const TEST_CONCURRENCY = 30;    // 并发测速数（受 RPC 与系统 fd 限制）
const MAX_TOP_TIERS = 49;       // 优质 tracker 独立 tier 上限（+1 垫底 = 50 组上限）

const action = tjs.env.SUPD_ACTION || 'update-trackers';
const serviceDir = tjs.env.SUPD_SERVICE_DIR || '';

// ---------------------------------------------------------------------------
// 通用辅助
// ---------------------------------------------------------------------------

function withTimeout(promise, timeoutMs, reason) {
  let timeoutId;
  const timeout = new Promise((_, reject) => {
    timeoutId = setTimeout(() => reject(new Error(reason || 'timeout')), timeoutMs);
  });
  return Promise.race([promise, timeout]).finally(() => clearTimeout(timeoutId));
}

// 并发限速执行：items 数组按 concurrency 并发跑 worker，worker 返回任意结果
async function runWithConcurrency(items, concurrency, worker, onProgress) {
  const results = new Array(items.length);
  let nextIndex = 0;
  let completed = 0;

  async function loop() {
    while (true) {
      const i = nextIndex++;
      if (i >= items.length) break;
      try {
        results[i] = await worker(items[i], i);
      } catch (e) {
        results[i] = { error: e.message };
      }
      completed++;
      if (onProgress) onProgress(completed, items.length);
    }
  }

  const n = Math.max(1, Math.min(concurrency, items.length || 1));
  const workers = [];
  for (let i = 0; i < n; i++) workers.push(loop());
  await Promise.all(workers);
  return results;
}

// ---------------------------------------------------------------------------
// Transmission RPC
// ---------------------------------------------------------------------------

async function readRPCConfig() {
  const defaults = { rpcUrl: 'http://127.0.0.1:9091/transmission/rpc' };
  if (!serviceDir) return defaults;

  const configPath = serviceDir + '/config/settings.json';
  try {
    const content = await tjs.readFile(configPath);
    const text = typeof content === 'string' ? content : new TextDecoder().decode(content);
    const settings = JSON.parse(text);

    const port = settings['rpc-port'] || 9091;
    const bindAddr = settings['rpc-bind-address'] || '127.0.0.1';
    const host = bindAddr === '0.0.0.0' ? '127.0.0.1' : bindAddr;
    const rpcUrlPath = (settings['rpc-url'] || '/transmission/') + 'rpc';

    return { rpcUrl: 'http://' + host + ':' + port + rpcUrlPath };
  } catch (e) {
    console.log('读取 settings.json 失败（' + e.message + '），使用默认 RPC 端口 9091');
    return defaults;
  }
}

async function transRpc(rpcUrl, method, arguments_, sessionId) {
  const headers = { 'Content-Type': 'application/json' };
  if (sessionId) headers['X-Transmission-Session-Id'] = sessionId;

  const body = JSON.stringify({ method, arguments: arguments_ || {} });

  let resp = await fetch(rpcUrl, { method: 'POST', headers, body });

  if (resp.status === 409) {
    const newSessionId = resp.headers.get('X-Transmission-Session-Id');
    if (!newSessionId) throw new Error('RPC 返回 409 但未携带 X-Transmission-Session-Id 头');
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
  if (data.result !== 'success') throw new Error('RPC 返回错误: ' + data.result);

  return { data, sessionId: headers['X-Transmission-Session-Id'] };
}

// ---------------------------------------------------------------------------
// 数据源解析与合并
// ---------------------------------------------------------------------------

// 解析单个数据源：返回 tracker URL 列表（已过滤协议、跳过空行和注释）
function parseSource(rawText) {
  const trackers = [];
  const skipped = {};
  let lineCount = 0;

  let text = rawText;
  if (text.length > 0 && text.charCodeAt(0) === 0xFEFF) text = text.slice(1);

  const lines = text.split(/\r?\n/);
  for (const line of lines) {
    lineCount++;
    const trimmed = line.trim();
    if (trimmed.length === 0 || trimmed.startsWith('#')) continue;

    const lower = trimmed.toLowerCase();
    const ok = SUPPORTED_SCHEMES.some((s) => lower.startsWith(s));
    if (!ok) {
      const scheme = lower.includes(':') ? lower.split(':')[0] : '(unknown)';
      skipped[scheme] = (skipped[scheme] || 0) + 1;
      continue;
    }
    trackers.push(trimmed);
  }

  return { trackers, skipped, lineCount };
}

// 多源去重（大小写不敏感），保留首次出现的 URL 形式
function mergeAndDedup(sources) {
  const seen = new Set();
  const merged = [];
  let duplicateCount = 0;

  for (const { name, trackers } of sources) {
    for (const url of trackers) {
      const lower = url.toLowerCase();
      if (seen.has(lower)) {
        duplicateCount++;
        continue;
      }
      seen.add(lower);
      merged.push(url);
    }
  }
  return { merged, duplicateCount };
}

// ---------------------------------------------------------------------------
// Tracker 测速
// ---------------------------------------------------------------------------

// 构造 HTTP/HTTPS 测试 announce URL（随机 info_hash + event=stopped + numwant=0）
// 不实际获取 peer，仅验证 tracker 可达且有响应
function buildAnnounceUrl(announceUrl) {
  const infoHash = new Uint8Array(20);
  const peerId = new Uint8Array(20);
  crypto.getRandomValues(infoHash);
  crypto.getRandomValues(peerId);

  function urlencodeBytes(bytes) {
    let s = '';
    for (const b of bytes) {
      const ch = String.fromCharCode(b);
      if (/[a-zA-Z0-9_.~-]/.test(ch)) {
        s += ch;
      } else {
        s += '%' + b.toString(16).padStart(2, '0').toUpperCase();
      }
    }
    return s;
  }

  const params =
    'info_hash=' + urlencodeBytes(infoHash) +
    '&peer_id=' + urlencodeBytes(peerId) +
    '&port=6881&uploaded=0&downloaded=0&left=0&compact=1&numwant=0&event=stopped';

  const sep = announceUrl.includes('?') ? '&' : '?';
  return announceUrl + sep + params;
}

async function testHttpTracker(url) {
  const testUrl = buildAnnounceUrl(url);
  const start = performance.now();
  const controller = new AbortController();
  const timeoutId = setTimeout(() => controller.abort(), TEST_TIMEOUT_MS);
  try {
    const resp = await fetch(testUrl, {
      signal: controller.signal,
      headers: { 'User-Agent': 'supd-tracker-test/1.3' },
      // 不读取响应体（节省带宽与内存），仅看 HTTP 状态码即可判定存活
    });
    const latency = performance.now() - start;
    // 任何 HTTP 响应都表示 tracker 可达（含 400/403/500 等 tracker 业务错误）
    if (resp.status > 0) {
      return { alive: true, latency, reason: 'http-' + resp.status };
    }
    return { alive: false, latency, reason: 'no-http-status' };
  } catch (e) {
    const latency = performance.now() - start;
    const msg = e && e.name === 'AbortError' ? 'timeout' : (e.message || 'fetch-error');
    return { alive: false, latency, reason: msg };
  } finally {
    clearTimeout(timeoutId);
  }
}

// BEP-15 UDP Tracker Protocol: CONNECT 请求（16 字节）
// magic=0x41727101980, action=0, transaction_id=random
function buildUdpConnectRequest(transactionId) {
  const buf = new Uint8Array(16);
  const dv = new DataView(buf.buffer);
  // protocol_id magic = 0x41727101980（8 字节大端）
  dv.setUint32(0, 0x00000417, false);
  dv.setUint32(4, 0x27101980, false);
  // action = 0 (CONNECT)
  dv.setUint32(8, 0, false);
  // transaction_id
  dv.setUint32(12, transactionId >>> 0, false);
  return buf;
}

// 校验 CONNECT 响应：action=0 且 transaction_id 匹配即认为存活
// action=3 (ERROR) 且 transaction_id 匹配也认为存活（tracker 可达但拒绝服务）
function isValidUdpConnectResponse(data, expectedTransactionId) {
  if (!data || data.length < 16) return false;
  const dv = new DataView(data.buffer, data.byteOffset, data.byteLength);
  const action = dv.getUint32(0, false);
  const transactionId = dv.getUint32(4, false);
  if (transactionId !== expectedTransactionId) return false;
  return action === 0 || action === 3;
}

async function testUdpTracker(url) {
  // 解析 udp://host:port[/path]
  let m = url.match(/^udp:\/\/\[([^\]]+)\]:(\d+)/); // IPv6
  if (!m) m = url.match(/^udp:\/\/([^/:]+):(\d+)/);  // IPv4/域名
  if (!m) return { alive: false, latency: 0, reason: 'invalid-url' };

  const host = m[1];
  const port = parseInt(m[2], 10);

  // 生成随机 transaction_id
  const tidBuf = new Uint8Array(4);
  crypto.getRandomValues(tidBuf);
  const tidDv = new DataView(tidBuf.buffer);
  const transactionId = tidDv.getUint32(0, false);
  const request = buildUdpConnectRequest(transactionId);

  const start = performance.now();
  let sock = null;
  let reader = null;
  let writer = null;
  try {
    if (typeof UDPSocket === 'undefined') {
      return { alive: false, latency: 0, reason: 'no-udp-api' };
    }
    sock = new UDPSocket({ remoteAddress: host, remotePort: port });
    const opened = await withTimeout(sock.opened, TEST_TIMEOUT_MS, 'udp-open-timeout');
    reader = opened.readable.getReader();
    writer = opened.writable.getWriter();
    await writer.write({ data: request });

    const readPromise = reader.read();
    const { value } = await withTimeout(readPromise, TEST_TIMEOUT_MS, 'udp-read-timeout');
    const latency = performance.now() - start;

    if (value && value.data && isValidUdpConnectResponse(value.data, transactionId)) {
      return { alive: true, latency, reason: 'udp-connect-ok' };
    }
    return { alive: false, latency, reason: 'invalid-response' };
  } catch (e) {
    const latency = performance.now() - start;
    const msg = (e && e.message) ? e.message : 'udp-error';
    return { alive: false, latency, reason: msg };
  } finally {
    try { if (reader) reader.releaseLock(); } catch (e) {}
    try { if (writer) writer.releaseLock(); } catch (e) {}
    try { if (sock) sock.close(); } catch (e) {}
    try { if (sock) await sock.closed; } catch (e) {}
  }
}

async function testTracker(url) {
  const lower = url.toLowerCase();
  if (lower.startsWith('udp://')) return testUdpTracker(url);
  return testHttpTracker(url);
}

// ---------------------------------------------------------------------------
// 分组与格式化
// ---------------------------------------------------------------------------

// 输入：测试结果数组 [{ url, alive, latency, reason }, ...]
// 输出：格式化字符串（空行分隔 tier）
// 策略：
//   1. 存活的按 latency 升序排前
//   2. 取前 MAX_TOP_TIERS 个存活 tracker，每个独立成 tier（置顶）
//   3. 其余（存活溢出 + 全部未响应）合并为 1 个 tier（垫底）
//   4. 总 tier 数 ≤ MAX_TOP_TIERS + 1 = 50
function groupAndFormat(results) {
  // 分离存活/未存活
  const alive = results.filter((r) => r && r.alive);
  const dead = results.filter((r) => !r || !r.alive);

  // 存活按 latency 升序
  alive.sort((a, b) => a.latency - b.latency);

  const top = alive.slice(0, MAX_TOP_TIERS);
  const overflowAlive = alive.slice(MAX_TOP_TIERS);

  // 垫底 tier：存活溢出 + 全部未响应（作为兜底回退）
  const bottom = overflowAlive.concat(dead);

  const tiers = [];
  // 优质 tier：每个独立成 tier
  for (const r of top) {
    tiers.push(r.url);
  }
  // 垫底 tier：合并为一个
  if (bottom.length > 0) {
    tiers.push(bottom.map((r) => r.url).join('\n'));
  }

  return {
    output: tiers.join('\n\n'),
    topCount: top.length,
    bottomCount: bottom.length,
    aliveCount: alive.length,
    deadCount: dead.length,
    tierCount: tiers.length,
  };
}

// ---------------------------------------------------------------------------
// 主流程
// ---------------------------------------------------------------------------

async function updateTrackers() {
  // 1. 读取 RPC 配置
  console.log('::progress:: 3 "正在读取 Transmission RPC 配置..."');
  const rpcConfig = await readRPCConfig();
  const RPC_URL = rpcConfig.rpcUrl;
  console.log('RPC 地址: ' + RPC_URL);

  // 2. 下载并解析所有数据源
  console.log('::progress:: 8 "正在下载 ' + TRACKER_SOURCES.length + ' 个数据源..."');
  const sources = [];
  const allSkipped = {};
  let totalRaw = 0;

  for (let i = 0; i < TRACKER_SOURCES.length; i++) {
    const src = TRACKER_SOURCES[i];
    const pct = 8 + Math.floor(((i + 1) / TRACKER_SOURCES.length) * 7);
    console.log('::progress:: ' + pct + " 正在下载 " + src.name + "...");
    console.log('  数据源: ' + src.url);

    try {
      const resp = await fetch(src.url, { headers: { 'User-Agent': 'supd-tjs-ext' } });
      if (!resp.ok) {
        console.log('  ✗ ' + src.name + ' 下载失败: HTTP ' + resp.status);
        continue;
      }
      const rawText = await resp.text();
      const { trackers, skipped, lineCount } = parseSource(rawText);
      console.log('  ✓ ' + src.name + ': ' + lineCount + ' 行，解析出 ' + trackers.length + ' 个 tracker');

      for (const [k, v] of Object.entries(skipped)) {
        allSkipped[k] = (allSkipped[k] || 0) + v;
      }
      totalRaw += trackers.length;
      sources.push({ name: src.name, trackers });
    } catch (e) {
      console.log('  ✗ ' + src.name + ' 下载异常: ' + e.message);
    }
  }

  if (sources.length === 0) {
    console.log('::result:: error "所有数据源下载失败"');
    tjs.exit(1);
    return;
  }

  console.log('共下载 ' + sources.length + '/' + TRACKER_SOURCES.length + ' 个源，原始 tracker 总数: ' + totalRaw);

  // 3. 合并去重
  console.log('::progress:: 18 "合并去重..."');
  const { merged, duplicateCount } = mergeAndDedup(sources);
  console.log('去重后保留 ' + merged.length + ' 个 tracker（删除 ' + duplicateCount + ' 个重复）');

  if (merged.length === 0) {
    console.log('::result:: error "去重后无可用 tracker"');
    tjs.exit(1);
    return;
  }

  // 4. 本地测速
  console.log('::progress:: 22 "开始本地测速（并发 ' + TEST_CONCURRENCY + '，超时 ' + TEST_TIMEOUT_MS + 'ms）..."');
  const testStart = performance.now();
  let lastPct = 22;
  const results = await runWithConcurrency(
    merged,
    TEST_CONCURRENCY,
    async (url) => {
      const r = await testTracker(url);
      return { url, ...r };
    },
    (completed, total) => {
      // 进度映射到 22-80 区间
      const pct = 22 + Math.floor((completed / total) * 58);
      if (pct > lastPct && (pct - lastPct >= 3 || completed === total)) {
        console.log('::progress:: ' + pct + ' 测速进度 ' + completed + '/' + total);
        lastPct = pct;
      }
    }
  );
  const testElapsed = ((performance.now() - testStart) / 1000).toFixed(1);
  console.log('测速完成，耗时 ' + testElapsed + 's');

  // 5. 分组格式化
  const grouped = groupAndFormat(results);
  console.log('分组结果：优质 ' + grouped.topCount + '（独立 tier）+ 垫底 ' + grouped.bottomCount + '（合并 1 tier）= 共 ' + grouped.tierCount + ' 个 tier');
  console.log('存活 ' + grouped.aliveCount + ' / 未响应 ' + grouped.deadCount);

  // 协议分布（基于全部 tracker）
  const proto = { http: 0, https: 0, udp: 0 };
  for (const url of merged) {
    const lower = url.toLowerCase();
    if (lower.startsWith('https://')) proto.https++;
    else if (lower.startsWith('http://')) proto.http++;
    else if (lower.startsWith('udp://')) proto.udp++;
  }
  const protoSummary = Object.entries(proto)
    .filter(([, v]) => v > 0)
    .map(([k, v]) => k + '×' + v)
    .join(' ');

  // 协议存活分布
  const protoAlive = { http: 0, https: 0, udp: 0 };
  for (const r of results) {
    if (!r || !r.alive) continue;
    const lower = r.url.toLowerCase();
    if (lower.startsWith('https://')) protoAlive.https++;
    else if (lower.startsWith('http://')) protoAlive.http++;
    else if (lower.startsWith('udp://')) protoAlive.udp++;
  }
  const protoAliveSummary = Object.entries(protoAlive)
    .filter(([, v]) => v > 0)
    .map(([k, v]) => k + '×' + v)
    .join(' ');

  const skippedTotal = Object.values(allSkipped).reduce((a, b) => a + b, 0);
  const skipSummary = Object.keys(allSkipped).length
    ? Object.entries(allSkipped).map(([k, v]) => k + '×' + v).join(' ')
    : '无';

  // 6. 连接 Transmission RPC
  console.log('::progress:: 85 "正在连接 Transmission RPC..."');
  const rpcResult = await transRpc(RPC_URL, 'session-get', {});
  const sessionId = rpcResult.sessionId;
  console.log('RPC 连接成功');

  // 7. 设置全局默认 Tracker 列表
  console.log('::progress:: 92 "正在更新全局 Tracker 列表..."');
  await transRpc(RPC_URL, 'session-set', { default_trackers: grouped.output }, sessionId);

  console.log('::progress:: 100 "更新完成"');
  console.log(
    '::result:: success "已更新全局 Tracker 列表：3 源合并去重 ' + merged.length + ' 个（' + protoSummary +
      '），测速存活 ' + grouped.aliveCount + '（' + protoAliveSummary + '）。优质 ' + grouped.topCount +
      ' 个独立 tier 置顶，其余 ' + grouped.bottomCount + ' 个合并 1 tier 垫底，共 ' + grouped.tierCount +
      ' 个 tier（≤50）。跳过不支持协议 ' + skippedTotal + ' 个（' + skipSummary + '）。新种子将自动获得这些 Tracker"'
  );
}

// ---------------------------------------------------------------------------
// 主入口
// ---------------------------------------------------------------------------

console.log('[tracker-updater] action=' + action + ' version=1.3.0');

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
