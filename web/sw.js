// babynas Service Worker —— 预缓存应用外壳与全部游戏，离线（离开家庭 NAS）也能玩游戏。
// 媒体流 /api/* 走网络（依赖 NAS），离线时自然不可用，但不影响游戏。

const VERSION = 'v4';
const CACHE = `babynas-${VERSION}`;

// 应用外壳 + 自包含游戏（全部为静态资源）
const SHELL = [
  '/',
  '/index.html',
  '/manifest.json',
  '/icon.svg',
  '/games/shared.js',
  '/games/bubbles/',
  '/games/animals/',
  '/games/drum/',
  '/games/piano/',
  '/games/fireworks/',
  '/games/paint/',
];

self.addEventListener('install', e => {
  e.waitUntil(
    caches.open(CACHE)
      .then(c => Promise.allSettled(SHELL.map(u => c.add(u)))) // 单个失败不阻断安装
      .then(() => self.skipWaiting())
  );
});

self.addEventListener('activate', e => {
  e.waitUntil(
    caches.keys()
      .then(keys => Promise.all(keys.filter(k => k !== CACHE).map(k => caches.delete(k))))
      .then(() => self.clients.claim())
  );
});

self.addEventListener('fetch', e => {
  const { request } = e;
  if (request.method !== 'GET') return;
  const url = new URL(request.url);

  // 媒体 / 接口：强制走网络，SW 绝不介入缓存（否则可能把 /api/stream 返回成 index.html）
  if (url.pathname.startsWith('/api/')) {
    e.respondWith(fetch(request));
    return;
  }

  // HTML 页面（外壳）：网络优先，保证应用更新即时生效；离线时回落缓存
  const isHTML = request.mode === 'navigate' ||
    url.pathname === '/' || url.pathname.endsWith('.html');
  if (isHTML) {
    e.respondWith(
      fetch(request).then(res => {
        if (res.ok && res.type === 'basic') {
          const clone = res.clone();
          caches.open(CACHE).then(c => c.put(request, clone));
        }
        return res;
      }).catch(() => caches.match(request).then(hit => hit || caches.match('/index.html')))
    );
    return;
  }

  // 其他静态资源（游戏 / js / svg）：缓存优先，离线可玩
  e.respondWith(
    caches.match(request).then(hit => hit || fetch(request).then(res => {
      if (res.ok && res.type === 'basic') {
        const clone = res.clone();
        caches.open(CACHE).then(c => c.put(request, clone));
      }
      return res;
    }).catch(() => caches.match('/index.html'))) // 离线兜底
  );
});
