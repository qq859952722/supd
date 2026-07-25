// run.js — tjs 运行时扩展示例
// 演示：环境变量访问、fetch、文件写入、stdout 协议
// 参考文档: references/06_tjs_runtime_guide.md

const action = tjs.env.SUPD_ACTION || 'run';

console.log('::progress:: 25 "启动 tjs 扩展"');
console.log(`tjs version: ${tjs.version}`);
console.log(`cwd: ${tjs.cwd}`);
console.log(`action: ${action}`);
console.log(`service dir: ${tjs.env.SUPD_SERVICE_DIR || '(none)'}`);

// 演示 fetch（GitHub API）
console.log('::progress:: 50 "请求 GitHub API"');
try {
  const resp = await fetch('https://api.github.com/repos/saghul/txiki.js');
  if (resp.ok) {
    const data = await resp.json();
    console.log(`txiki.js stars: ${data.stargazers_count}, open issues: ${data.open_issues_count}`);
  } else {
    console.log(`GitHub API responded ${resp.status}`);
  }
} catch (e) {
  console.log(`fetch failed (网络问题，不影响演示): ${e.message}`);
}

// 演示文件写入与读取
console.log('::progress:: 75 "文件操作演示"');
const encoder = new TextEncoder();
const decoder = new TextDecoder();
const demoPath = '/tmp/tjs-demo-output.txt';

await tjs.writeFile(demoPath, encoder.encode(`hello from tjs ${tjs.version}\n`));
const readBack = decoder.decode(await tjs.readFile(demoPath));
console.log(`文件读写验证: ${readBack.trim()}`);

// 演示路径模块
import path from 'tjs:path';
console.log(`path.join 示例: ${path.join('/a', 'b', 'c.js')}`);

console.log('::progress:: 100 "演示完成"');
console.log('::result:: success "tjs 演示扩展执行成功"');
