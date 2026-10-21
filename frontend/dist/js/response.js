// response.js —— 响应查看器：状态、耗时、响应头与响应体。

import { $, $$, escapeHtml, formatBytes, formatDuration, highlightJSON, prettyJSON, decodeBase64 } from './util.js';
import { state, emit } from './store.js';

/** 初始化响应面板。 */
export function initResponse() {
  $('#btn-view-toggle').addEventListener('click', () => {
    state.responseView = state.responseView === 'pretty' ? 'raw' : 'pretty';
    $('#btn-view-toggle').textContent = state.responseView === 'pretty' ? '原始' : '美化';
    renderBody();
  });

  $('#btn-copy-resp').addEventListener('click', async () => {
    const res = state.result;
    if (!res) {
      emit('toast', { message: '还没有可复制的响应', kind: 'warn' });
      return;
    }
    const text = res.isBinary ? res.body : res.body;
    try {
      await navigator.clipboard.writeText(text);
      emit('toast', { message: '响应体已复制到剪贴板', kind: 'success' });
    } catch {
      emit('toast', { message: '复制失败，请手动选择文本', kind: 'error' });
    }
  });

  $$('.resp-tab').forEach((tab) => {
    tab.addEventListener('click', () => {
      $$('.resp-tab').forEach((t) => t.classList.toggle('active', t === tab));
      $('#resp-body-pane').classList.toggle('active', tab.dataset.tab === 'body');
      $('#resp-headers-pane').classList.toggle('active', tab.dataset.tab === 'headers');
    });
  });
}

/** 渲染一次请求的结果。 */
export function renderResult(res) {
  state.result = res;
  renderMeta();
  renderBody();
  renderHeaders();
}

/** 渲染状态栏。 */
function renderMeta() {
  const box = $('#resp-meta');
  const res = state.result;
  if (!res) {
    box.innerHTML = '';
    return;
  }
  if (res.error) {
    box.innerHTML = `<span class="status-pill status-err">请求失败</span><span>${escapeHtml(res.errorKind || '')}</span>`;
    return;
  }
  const cls = `status-${String(res.status || 0)[0]}xx`;
  const timing = [
    `DNS ${formatDuration(res.dnsMs)}`,
    `连接 ${formatDuration(res.connectMs)}`,
    `TLS ${formatDuration(res.tlsMs)}`,
    `首字节 ${formatDuration(res.ttfbMs)}`,
  ].join(' · ');
  box.innerHTML = `
    <span class="status-pill ${cls}">${res.status} ${escapeHtml(res.statusText || '')}</span>
    <span title="${escapeAttrSafe(timing)}">${formatDuration(res.durationMs)}</span>
    <span class="sep">·</span>
    <span>${formatBytes(res.size)}</span>
    ${res.contentType ? `<span class="sep">·</span><span>${escapeHtml(res.contentType)}</span>` : ''}
    ${res.truncated ? '<span class="sep">·</span><span style="color:var(--amber)">已截断</span>' : ''}`;
}

/** 渲染响应体。 */
function renderBody() {
  const box = $('#resp-body-container');
  const res = state.result;
  if (!res) {
    box.innerHTML = `
      <div class="resp-placeholder">
        <div class="big">⌁</div>
        <div>发送一个请求，这里会显示响应</div>
      </div>`;
    return;
  }

  const parts = [];

  if (res.error) {
    parts.push(`<div class="resp-error"><b>请求失败</b><br />${escapeHtml(res.error)}</div>`);
  }

  if (res.unresolvedVars?.length) {
    parts.push(
      `<div class="resp-notice">以下变量未被解析，已按原样发送：<code>${res.unresolvedVars
        .map((v) => `{{${escapeHtml(v)}}}`)
        .join('</code>、<code>')}</code></div>`
    );
  }

  if (res.redirects?.length) {
    const hops = res.redirects
      .map((h) => `<div>${h.status} → ${escapeHtml(h.url)}</div>`)
      .join('');
    parts.push(`<div class="resp-notice">跟随了 ${res.redirects.length} 次重定向：${hops}</div>`);
  }

  if (res.isBinary) {
    parts.push(
      `<div class="resp-notice">这是二进制响应（${escapeHtml(res.contentType || '未知类型')}，${formatBytes(
        res.size
      )}），下方以 Base64 展示。</div>`
    );
    parts.push(`<pre class="resp-body raw">${escapeHtml(res.body)}</pre>`);
  } else if (res.body) {
    const pretty = prettyJSON(res.body);
    if (state.responseView === 'pretty' && pretty !== null) {
      parts.push(`<pre class="resp-body">${highlightJSON(pretty)}</pre>`);
    } else {
      parts.push(`<pre class="resp-body raw">${escapeHtml(res.body)}</pre>`);
    }
  } else if (!res.error) {
    parts.push('<div class="resp-placeholder"><div class="big">∅</div><div>响应体为空</div></div>');
  }

  box.innerHTML = parts.join('');
}

/** 渲染响应头。 */
function renderHeaders() {
  const box = $('#resp-headers');
  const res = state.result;
  if (!res) {
    box.innerHTML = '<div class="hint">还没有响应。</div>';
    return;
  }
  const entries = Object.entries(res.headers || {});
  if (!entries.length) {
    box.innerHTML = '<div class="hint">响应中没有头部信息。</div>';
    return;
  }
  const rows = entries
    .map(
      ([key, values]) =>
        `<tr><td>${escapeHtml(key)}</td><td>${escapeHtml((values || []).join('\n'))}</td></tr>`
    )
    .join('');
  box.innerHTML = `<table><thead><tr><th>名称</th><th>值</th></tr></thead><tbody>${rows}</tbody></table>`;
}

/** 转义用于 title 属性的文本。 */
function escapeAttrSafe(s) {
  return String(s).replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;');
}

/** 把二进制响应解码成可读文本，供调试使用。 */
export function decodeBinary(res) {
  return decodeBase64(res.body);
}
