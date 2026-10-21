// util.js —— 通用工具函数。

export const $ = (sel, root = document) => root.querySelector(sel);
export const $$ = (sel, root = document) => Array.from(root.querySelectorAll(sel));

/** 转义为可安全放入 HTML 文本节点的内容。 */
export function escapeHtml(s) {
  return String(s ?? '').replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;');
}

/** 转义为可安全放入 HTML 属性值的内容。 */
export function escapeAttr(s) {
  return escapeHtml(s).replace(/"/g, '&quot;');
}

/** 延迟执行，等待期间再次调用会重新计时。 */
export function debounce(fn, wait = 200) {
  let timer = null;
  return (...args) => {
    if (timer) clearTimeout(timer);
    timer = setTimeout(() => fn(...args), wait);
  };
}

/** 生成短随机标识。 */
export function uid() {
  return Math.random().toString(16).slice(2, 10) + Date.now().toString(16).slice(-6);
}

/** 把字节数格式化为易读形式。 */
export function formatBytes(n) {
  const num = Number(n) || 0;
  if (num <= 0) return '0 B';
  const units = ['B', 'KB', 'MB', 'GB'];
  let i = 0;
  let v = num;
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024;
    i += 1;
  }
  return `${i === 0 ? v : v.toFixed(1)} ${units[i]}`;
}

/** 把毫秒格式化为易读形式。 */
export function formatDuration(ms) {
  const num = Number(ms);
  if (!Number.isFinite(num) || num < 0) return '-';
  if (num < 1000) return `${Math.round(num)} ms`;
  return `${(num / 1000).toFixed(2)} s`;
}

/** 把时间戳格式化为本地时间。 */
export function formatTime(ts) {
  const d = new Date(Number(ts) || 0);
  const p = (n) => String(n).padStart(2, '0');
  return `${p(d.getMonth() + 1)}-${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}:${p(d.getSeconds())}`;
}

/** 尝试把文本解析为 JSON 并格式化，失败时返回 null。 */
export function prettyJSON(text) {
  try {
    return JSON.stringify(JSON.parse(text), null, 2);
  } catch {
    return null;
  }
}

/** 给 JSON 文本加上语法高亮，返回可直接插入 innerHTML 的 HTML。 */
export function highlightJSON(text) {
  const escaped = escapeHtml(text);
  return escaped.replace(
    /("(?:\\.|[^"\\])*")(\s*:)|("(?:\\.|[^"\\])*")|\b(true|false)\b|\bnull\b|-?\b\d+(?:\.\d+)?(?:[eE][+-]?\d+)?\b/g,
    (match, key, colon, str) => {
      if (key) return `<span class="j-key">${key}</span>${colon}`;
      if (str) return `<span class="j-str">${str}</span>`;
      if (match === 'true' || match === 'false') return `<span class="j-bool">${match}</span>`;
      if (match === 'null') return `<span class="j-null">${match}</span>`;
      return `<span class="j-num">${match}</span>`;
    }
  );
}

/** 在页面右下角弹出一条提示。 */
export function toast(message, kind = 'info', duration = 2600) {
  let host = document.getElementById('toast-host');
  if (!host) {
    host = document.createElement('div');
    host.id = 'toast-host';
    document.body.appendChild(host);
  }
  const el = document.createElement('div');
  el.className = `toast toast-${kind}`;
  el.textContent = message;
  host.appendChild(el);
  requestAnimationFrame(() => el.classList.add('show'));
  setTimeout(() => {
    el.classList.remove('show');
    setTimeout(() => el.remove(), 220);
  }, duration);
}

/** 弹出确认框，返回 Promise<boolean>。 */
export function confirmDialog(message, title = '请确认') {
  return new Promise((resolve) => {
    const backdrop = document.createElement('div');
    backdrop.className = 'modal-backdrop';
    backdrop.innerHTML = `
      <div class="modal modal-sm">
        <div class="modal-title">${escapeHtml(title)}</div>
        <div class="modal-body">${escapeHtml(message)}</div>
        <div class="modal-actions">
          <button class="btn" data-act="cancel">取消</button>
          <button class="btn btn-danger" data-act="ok">确定</button>
        </div>
      </div>`;
    const done = (v) => {
      backdrop.remove();
      resolve(v);
    };
    backdrop.addEventListener('click', (e) => {
      const act = e.target.closest('[data-act]')?.dataset.act;
      if (act === 'ok') done(true);
      else if (act === 'cancel' || e.target === backdrop) done(false);
    });
    document.body.appendChild(backdrop);
    backdrop.querySelector('[data-act="ok"]').focus();
  });
}

/** 读取 base64 字符串并解码为 UTF-8 文本。 */
export function decodeBase64(b64) {
  const bin = atob(b64);
  const bytes = Uint8Array.from(bin, (c) => c.charCodeAt(0));
  return new TextDecoder('utf-8').decode(bytes);
}

/** 弹出输入框，返回 Promise<string|null>。 */
export function promptDialog(title, value = '', placeholder = '') {
  return new Promise((resolve) => {
    const backdrop = document.createElement('div');
    backdrop.className = 'modal-backdrop';
    backdrop.innerHTML = `
      <div class="modal modal-sm">
        <div class="modal-title">${escapeHtml(title)}</div>
        <div class="modal-body">
          <input class="input" id="prompt-input" value="${escapeAttr(value)}" placeholder="${escapeAttr(placeholder)}" />
        </div>
        <div class="modal-actions">
          <button class="btn" data-act="cancel">取消</button>
          <button class="btn btn-primary" data-act="ok">确定</button>
        </div>
      </div>`;
    const input = backdrop.querySelector('#prompt-input');
    const done = (v) => {
      backdrop.remove();
      resolve(v);
    };
    backdrop.addEventListener('click', (e) => {
      const act = e.target.closest('[data-act]')?.dataset.act;
      if (act === 'ok') done(input.value.trim());
      else if (act === 'cancel' || e.target === backdrop) done(null);
    });
    backdrop.addEventListener('keydown', (e) => {
      if (e.key === 'Enter') done(input.value.trim());
      if (e.key === 'Escape') done(null);
    });
    document.body.appendChild(backdrop);
    input.focus();
    input.select();
  });
}
