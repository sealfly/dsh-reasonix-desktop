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
      TestDshConn: () => Promise.reject('bridge unavailable'),
      SetDshConn: () => Promise.reject('bridge unavailable'),
      DshLaunch: () => Promise.reject('bridge unavailable')
    };
  }

  function check() {
    realApp().DshConnStatus().then(s => {
      statusCache = s;
      if (!s.connected) {
        // 连接失败：显示横幅（用户关闭过则本次会话不再自动弹出，仍可在设置里操作）
        try {
          const shown = parseInt(localStorage.getItem(SHOWN_KEY) || '0', 10);
          if (shown && Date.now() - shown < 24 * 3600 * 1000) {
            // 已关闭过（24h 内）：仅保留设置入口不弹横幅
            return;
          }
        } catch (e) {}
        showBanner();
      }
    }).catch(() => {});
  }

  function boot() {
    if (document.readyState === 'loading') {
      document.addEventListener('DOMContentLoaded', () => { injectCss(); setTimeout(check, 4000); });
    } else {
      injectCss();
      setTimeout(check, 4000);
    }
  }
  boot();
})();
