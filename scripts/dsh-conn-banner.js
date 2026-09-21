// dsh-conn-banner.js — DSH 连接状态横幅 + 自选 DSH 后端设置
// 注入到 Reasonix v1.31.4 前端：启动后查询 A.DshConnStatus()，
// 未连接时显示横幅（启动 DSH / 连接设置），设置弹层支持 host:port 自选 + 测试。
(() => {
  const SHOWN_KEY = 'dsh-conn-banner-shown';
  const CONN_ID = 'dsh-conn-banner';
  const SETTINGS_ID = 'dsh-conn-settings';
  let statusCache = null;

  const css = `
#${CONN_ID}{position:fixed;right:14px;bottom:14px;z-index:2147483646;max-width:360px;background:linear-gradient(135deg,#0153e5,#0b3fa8);color:#fff;border-radius:10px;padding:12px 14px;font-family:'Segoe UI',system-ui,sans-serif;font-size:13px;box-shadow:0 6px 24px rgba(1,83,229,.35);display:none}
#${CONN_ID}.show{display:block}
#${CONN_ID} .b-title{font-weight:600;margin-bottom:6px;display:flex;align-items:center;gap:8px}
#${CONN_ID} .b-title .dot{width:9px;height:9px;border-radius:50%;background:#ffd166;flex:none}
#${CONN_ID} .b-desc{opacity:.9;line-height:1.45;margin-bottom:10px}
#${CONN_ID} .b-actions{display:flex;gap:8px;flex-wrap:wrap}
#${CONN_ID} button{border:none;border-radius:6px;padding:6px 12px;font-size:12px;cursor:pointer;font-family:inherit}
#${CONN_ID} .btn-start{background:#fff;color:#0153e5;font-weight:600}
#${CONN_ID} .btn-set{background:rgba(255,255,255,.18);color:#fff}
#${CONN_ID} .btn-x{background:transparent;color:rgba(255,255,255,.75);margin-left:auto}
#${CONN_ID} .b-status{font-size:12px;margin-top:8px;opacity:.85;display:none}
#${SETTINGS_ID}{position:fixed;inset:0;z-index:2147483647;background:rgba(10,16,30,.55);display:none;align-items:center;justify-content:center;font-family:'Segoe UI',system-ui,sans-serif}
#${SETTINGS_ID}.show{display:flex}
#${SETTINGS_ID} .s-card{background:#fff;border-radius:12px;padding:20px 22px;width:380px;max-width:92vw;box-shadow:0 12px 40px rgba(0,0,0,.35);color:#1a1a2e}
#${SETTINGS_ID} .s-title{font-size:16px;font-weight:700;margin-bottom:14px;display:flex;justify-content:space-between;align-items:center}
#${SETTINGS_ID} .s-title .x{cursor:pointer;color:#999;font-size:18px;line-height:1;padding:2px 6px}
#${SETTINGS_ID} label{display:block;font-size:12px;color:#666;margin:10px 0 4px}
#${SETTINGS_ID} input{width:100%;box-sizing:border-box;padding:8px 10px;border:1px solid #d5dae4;border-radius:7px;font-size:13px;font-family:inherit}
#${SETTINGS_ID} .s-row{display:flex;gap:10px}
#${SETTINGS_ID} .s-row .s-host{flex:1}
#${SETTINGS_ID} .s-row .s-port{width:110px}
#${SETTINGS_ID} .s-actions{display:flex;gap:8px;margin-top:16px;justify-content:flex-end}
#${SETTINGS_ID} .s-actions button{border:none;border-radius:7px;padding:8px 16px;font-size:13px;cursor:pointer;font-family:inherit}
#${SETTINGS_ID} .btn-save{background:#0153e5;color:#fff;font-weight:600}
#${SETTINGS_ID} .btn-test{background:#eef2f9;color:#0153e5}
#${SETTINGS_ID} .btn-cancel{background:#f0f1f4;color:#666}
#${SETTINGS_ID} .s-status{font-size:12px;margin-top:10px;padding:8px 10px;border-radius:6px;display:none}
#${SETTINGS_ID} .s-status.ok{display:block;background:#e8f8ef;color:#147d3f}
#${SETTINGS_ID} .s-status.err{display:block;background:#fdeeee;color:#c0392b}
#${SETTINGS_ID} .s-hint{font-size:11px;color:#999;margin-top:6px}
`;

  function injectCss() {
    const s = document.createElement('style');
    s.id = CONN_ID + '-css';
    s.textContent = css;
    document.head.appendChild(s);
  }

  function el(id, html) {
    let n = document.getElementById(id);
    if (!n) {
      n = document.createElement('div');
      n.id = id;
      document.body.appendChild(n);
    }
    n.innerHTML = html;
    return n;
  }

  function showBanner() {
    // 已经在显示就**不要重建**：否则周期复检会把「正在启动 DSH 后端…」这类进度文字清掉。
    const existing = document.getElementById(CONN_ID);
    if (existing && existing.classList.contains('show')) return;
    el(CONN_ID, `
      <div class="b-title"><span class="dot"></span>DSH 后端未连接</div>
      <div class="b-desc">界面功能需要 DSH 后端（默认 ${statusCache ? statusCache.configured.host + ':' + statusCache.configured.port : '127.0.0.1:3080'}）。可一键启动，或自选其他 DSH 后端。</div>
      <div class="b-actions">
        <button class="btn-start" id="dsh-conn-start">🚀 启动 DSH</button>
        <button class="btn-set" id="dsh-conn-open">⚙️ 连接设置</button>
        <button class="btn-x" id="dsh-conn-close">✕</button>
      </div>
      <div class="b-status" id="dsh-conn-bstatus"></div>
    `);
    document.getElementById('dsh-conn-start').onclick = () => {
      const st = document.getElementById('dsh-conn-bstatus');
      st.style.display = 'block';
      st.textContent = '正在启动 DSH 后端...';
      window.appStartDSH && window.appStartDSH();
    };
    document.getElementById('dsh-conn-open').onclick = openSettings;
    document.getElementById('dsh-conn-close').onclick = () => {
      document.getElementById(CONN_ID).classList.remove('show');
      try { localStorage.setItem(SHOWN_KEY, Date.now().toString()); } catch (e) {}
    };
    document.getElementById(CONN_ID).classList.add('show');
  }

  function openSettings() {
    const c = (statusCache && statusCache.configured) || { host: '127.0.0.1', port: 3080 };
    el(SETTINGS_ID, `
      <div class="s-card">
        <div class="s-title">DSH 连接设置 <span class="x" id="dsh-conn-sclose">✕</span></div>
        <div class="s-row">
          <div class="s-host"><label>主机地址</label><input id="dsh-conn-host" value="${c.host}" placeholder="127.0.0.1"></div>
          <div class="s-port"><label>端口</label><input id="dsh-conn-port" value="${c.port}" placeholder="3080"></div>
        </div>
        <div class="s-hint">可指向本机或局域网/远程的 DeepSeek Harness 实例。</div>
        <div class="s-status" id="dsh-conn-sstatus"></div>
        <div class="s-actions">
          <button class="btn-test" id="dsh-conn-test">测试连接</button>
          <button class="btn-cancel" id="dsh-conn-cancel">取消</button>
          <button class="btn-save" id="dsh-conn-save">保存</button>
        </div>
      </div>
    `);
    const st = document.getElementById('dsh-conn-sstatus');
    const host = () => document.getElementById('dsh-conn-host').value.trim() || '127.0.0.1';
    const port = () => parseInt(document.getElementById('dsh-conn-port').value, 10) || 3080;
    document.getElementById('dsh-conn-sclose').onclick = closeSettings;
    document.getElementById('dsh-conn-cancel').onclick = closeSettings;
    document.getElementById('dsh-conn-test').onclick = () => {
      st.className = 's-status'; st.style.display = 'block'; st.textContent = '测试中...';
      realApp().TestDshConn(host(), port()).then(r => {
        if (r.ok) { st.className = 's-status ok'; st.textContent = '✅ 连接成功：' + r.host + ':' + r.port; }
        else { st.className = 's-status err'; st.textContent = '❌ 无法连接：' + (r.error || '未知错误'); }
      }).catch(e => { st.className = 's-status err'; st.textContent = '❌ 调用失败：' + e; });
    };
    document.getElementById('dsh-conn-save').onclick = () => {
      st.className = 's-status'; st.style.display = 'block'; st.textContent = '保存中...';
      realApp().SetDshConn(host(), port()).then(r => {
        if (r.ok && r.connected) {
          st.className = 's-status ok'; st.textContent = '✅ 已保存并连接：' + r.host + ':' + r.port;
          setTimeout(() => { closeSettings(); location.reload(); }, 900);
        } else if (r.ok) {
          st.className = 's-status err'; st.textContent = '⚠️ 已保存，但当前无法连接：' + (r.warning || '');
        } else {
          st.className = 's-status err'; st.textContent = '❌ ' + (r.error || '保存失败');
        }
      }).catch(e => { st.className = 's-status err'; st.textContent = '❌ 调用失败：' + e; });
    };
    document.getElementById(SETTINGS_ID).classList.add('show');
  }

  function closeSettings() {
    const m = document.getElementById(SETTINGS_ID);
    if (m) m.classList.remove('show');
  }

  // 通过 DSH 桥启动（走 Go 侧 DshLaunch）
  window.appStartDSH = () => {
    realApp().DshLaunch().then(r => {
      const st = document.getElementById('dsh-conn-bstatus');
      if (r.ok && r.started) {
        st.textContent = '✅ DSH 正在启动，初始化可能需要几秒...';
        setTimeout(() => location.reload(), 3500);
      } else if (r.ok && r.alreadyRunning) {
        st.textContent = '✅ DSH 已在运行，正在刷新...';
        setTimeout(() => location.reload(), 1200);
      } else {
        st.textContent = '❌ ' + (r.error || '启动失败');
      }
    }).catch(e => {
      const st = document.getElementById('dsh-conn-bstatus');
      if (st) st.textContent = '❌ 启动失败：' + e;
    });
  };

  function realApp() {
    const g = window.go && window.go.main && window.go.main.App;
    if (g) return g;
    return {
      // 桥不可用时不要把界面判成"未连接"（否则会弹一个用户无法处置的横幅）
      DshConnStatus: () => Promise.resolve({ connected: true, configured: { host: '127.0.0.1', port: 3080 } }),
      TestDshConn: () => Promise.reject('bridge unavailable'),
      SetDshConn: () => Promise.reject('bridge unavailable'),
      DshLaunch: () => Promise.reject('bridge unavailable')
    };
  }

  // 周期复检的节奏：未连接时查得勤（用户刚点完"启动 DSH"要尽快看到恢复），
  // 已连接时也保持 15s 一次（每次 DshConnStatus 真的会 ping 一次 DSH，实测 0.2~0.5s，
  // 15s 足够便宜，同时能在后端掉线后较快把横幅放回来）。
  const POLL_DISCONNECTED_MS = 8000;
  const POLL_CONNECTED_MS = 15000;
  let pollTimer = null;
  let ticks = 0; // 诊断计数：暴露在 window.__dshBanner 上，便于确认轮询真的在跑
  // 上一次的连通判定（null = 还没判过）。用来识别"断开 → 连上"这个跃迁。
  let wasConnected = null;

  function bannerVisible() {
    const n = document.getElementById(CONN_ID);
    return !!(n && n.classList.contains('show'));
  }

  // 「断开 → 连上」时自动重载页面。
  //
  // 为什么需要：前端有大量组件**只在挂载时取一次数** —— 项目树是典型：它挂载时调用
  // GetProjectTreeSnapshot，之后既不轮询、也要等后端事件才刷新，而我们没发那类事件。
  // 于是"应用先启动、DSH 后端后起来"时，项目树会把空树记下来，一直显示「还没有项目」。
  // 真机实测（2026-09-21）：DSH 里有 12 个项目 / 39 个会话，界面却是空的；
  // 手动刷新页面后全部恢复。这里让"后端稍后可用"这种情况自愈。
  // 冷却：同一次会话 20s 内只自动重载一次，避免后端抖动导致反复刷新。
  const RELOAD_AT_KEY = 'dsh-conn-autoreload-at';
  function reloadForReconnect() {
    let last = 0;
    try { last = parseInt(sessionStorage.getItem(RELOAD_AT_KEY) || '0', 10) || 0; } catch (e) {}
    if (Date.now() - last < 20000) return false;
    try { sessionStorage.setItem(RELOAD_AT_KEY, String(Date.now())); } catch (e) {}
    location.reload();
    return true;
  }

  function hideBanner() {
    const n = document.getElementById(CONN_ID);
    if (n) n.classList.remove('show');
  }

  function schedule(ms) {
    if (pollTimer) clearTimeout(pollTimer);
    pollTimer = setTimeout(check, ms);
  }

  // 给桥调用加超时竞速：Wails 桥上**不存在的/异常的方法可能返回永不 settle 的 Promise**
  // （本项目已在 ui_test_hook.go 记录过这个坑）。若不复检就被它拖死，轮询会永久停摆 ——
  // 实测症状正是"横幅显示后再也不会自动消失/消失后再也不会出现"。
  function withTimeout(p, ms) {
    return Promise.race([
      Promise.resolve(p),
      new Promise(res => setTimeout(() => res({ __timeout: true }), ms))
    ]);
  }

  function check() {
    ticks++;
    withTimeout(realApp().DshConnStatus(), 10000).then(s => {
      statusCache = s;
      const connected = !!(s && s.connected);
      const transition = wasConnected === false && connected; // 断开 → 连上
      // 诊断出口：排查"横幅不消失/不出现/项目树空白"时先看这里
      window.__dshBanner = { ticks, connected, wasConnected, transition, timeout: !!(s && s.__timeout), at: Date.now(), pollMs: connected ? POLL_CONNECTED_MS : POLL_DISCONNECTED_MS };
      if (connected) {
        // **连上就把横幅收掉**。
        // 旧实现只在启动 4s 后查一次、且只有 showBanner 没有 hideBanner ——
        // 真机实测（2026-09-21）：DshConnStatus() 返回 connected:true 时，
        // #dsh-conn-banner 仍是 class=show / display:block / opacity:1，一直挂在那儿误导用户。
        hideBanner();
        wasConnected = true;
        // 之前判过"未连接"，现在连上了 → 让只在挂载取数的组件（项目树等）重新初始化
        if (transition && reloadForReconnect()) return;
        schedule(POLL_CONNECTED_MS);
        return;
      }
      wasConnected = false;
      // 未连接：用户手动关过（24h 内）就不再自动弹
      let suppressed = false;
      try {
        const shown = parseInt(localStorage.getItem(SHOWN_KEY) || '0', 10);
        suppressed = !!(shown && Date.now() - shown < 24 * 3600 * 1000);
      } catch (e) {}
      if (!suppressed) showBanner();
      schedule(POLL_DISCONNECTED_MS);
    }).catch(e => {
      // 查询失败（如桥还没就绪）：保持现状，稍后再试
      window.__dshBanner = { ticks, connected: null, wasConnected, error: String(e && e.message ? e.message : e), at: Date.now() };
      schedule(POLL_DISCONNECTED_MS);
    });
  }

  function boot() {
    const start = () => {
      injectCss();
      setTimeout(check, 4000);
      // 窗口回到前台/获得焦点时立刻复检一次（用户多半是刚去看别处启动 DSH 了）
      document.addEventListener('visibilitychange', () => { if (!document.hidden) check(); });
      window.addEventListener('focus', () => { if (bannerVisible()) check(); });
    };
    if (document.readyState === 'loading') {
      document.addEventListener('DOMContentLoaded', start);
    } else {
      start();
    }
  }
  boot();
})();
