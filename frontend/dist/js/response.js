// response.js —— 响应查看器：状态、耗时、响应头与响应体。

import {
  $,
  $$,
  escapeHtml,
  escapeAttr,
  formatBytes,
  formatDuration,
  highlightJSON,
  prettyJSON,
  copyText,
} from './util.js';
import { state, emit } from './store.js';
import { bridge } from './bridge.js';
import { headersFilename, selectionFilename, suggestResponseFilename } from './download.js';
import { filterHeaders, formatHeaders } from './headers.js';
import { closeContextMenu, showContextMenu } from './menu.js';

/** 超过该大小的响应体不再做 JSON 美化与高亮，避免 WebView 卡顿。 */
const MAX_PRETTY_BYTES = 1024 * 1024;

const ERROR_KIND_LABELS = {
  timeout: '超时',
  dns: 'DNS 解析失败',
  refused: '连接被拒绝',
  tls: 'TLS 证书错误',
  reset: '连接被重置',
  badurl: '地址不合法',
  network: '网络错误',
  redirects: '重定向过多',
  file: '文件读取失败',
};

/** 初始化响应面板。 */
export function initResponse() {
  $('#btn-view-toggle').addEventListener('click', () => {
    if ($('#btn-view-toggle').disabled) return;
    state.responseView = state.responseView === 'pretty' ? 'raw' : 'pretty';
    $('#btn-view-toggle').textContent = state.responseView === 'pretty' ? '原始' : '美化';
    renderBody();
  });

  $('#btn-save-resp').addEventListener('click', saveResponseBody);
  $('#btn-copy-resp').addEventListener('click', copyResponseBody);
  $('#resp-body-container').addEventListener('contextmenu', openResponseContextMenu);
  $('#resp-headers').addEventListener('contextmenu', openHeadersContextMenu);
  $('#headers-filter').addEventListener('input', renderHeaders);
  $('#headers-filter').addEventListener('keydown', (e) => {
    if (e.key === 'Escape' && e.target.value) {
      e.target.value = '';
      renderHeaders();
      e.stopPropagation();
    }
  });

  $$('.resp-tab').forEach((tab) => {
    tab.addEventListener('click', () => selectTab(tab.dataset.tab));
  });

  // 点击状态栏里的耗时，直接切换到「耗时」页。
  $('#resp-meta').addEventListener('click', (e) => {
    const tab = e.target.closest('[data-goto-tab]')?.dataset.gotoTab;
    if (tab) selectTab(tab);
  });
}

/** 切换响应区 Tab。 */
function selectTab(name) {
  $$('.resp-tab').forEach((tab) => tab.classList.toggle('active', tab.dataset.tab === name));
  $$('.resp-content').forEach((pane) => pane.classList.toggle('active', pane.id === `resp-${name}-pane`));
}

/** 渲染一次请求的结果。 */
export function renderResult(res) {
  closeContextMenu();
  state.result = res;
  renderToggle(res);
  renderMeta();
  renderBody();
  renderHeaders();
  renderTiming();
}

/** 根据响应内容决定「原始 / 美化」按钮是否可用。 */
function renderToggle(res) {
  const save = $('#btn-save-resp');
  if (save) save.disabled = !res || !res.body;

  const btn = $('#btn-view-toggle');
  const tooLarge = Boolean(res && !res.isBinary && (res.body?.length || 0) > MAX_PRETTY_BYTES);
  btn.disabled = !res || Boolean(res.isBinary) || tooLarge;
  if (!res || res.isBinary) {
    btn.textContent = '原始';
    return;
  }
  btn.textContent = state.responseView === 'pretty' ? '原始' : '美化';
}

/** 复制完整响应体。 */
async function copyResponseBody() {
  const res = state.result;
  if (!res) {
    emit('toast', { message: '还没有可复制的响应', kind: 'warn' });
    return;
  }
  const ok = await copyText(res.body || '');
  emit('toast', {
    message: ok
      ? res.isBinary
        ? 'Base64 内容已复制'
        : '响应体已复制到剪贴板'
      : '复制失败，请手动选择文本',
    kind: ok ? 'success' : 'error',
  });
}

/** 复制右键时选中的文本。 */
async function copySelectedResponse(text) {
  const ok = await copyText(text);
  emit('toast', {
    message: ok ? '选中内容已复制' : '复制失败，请手动选择文本',
    kind: ok ? 'success' : 'error',
  });
}

/** 把当前响应体保存到用户选择的文件。 */
async function saveResponseBody() {
  const res = state.result;
  if (!res) {
    emit('toast', { message: '还没有可保存的响应', kind: 'warn' });
    return;
  }
  if (!res.body) {
    emit('toast', {
      message: res.error ? '请求失败，没有可保存的响应体' : '响应体为空，无需保存',
      kind: 'warn',
    });
    return;
  }
  const filename = suggestResponseFilename(res, state.request);
  await saveWithDialog(filename, res.body, Boolean(res.bodyBase64), {
    truncated: Boolean(res.truncated),
  });
}

/** 把选中的响应文本保存为 .txt。 */
async function saveSelectedResponse(text) {
  if (!text) return;
  const filename = selectionFilename(suggestResponseFilename(state.result, state.request));
  await saveWithDialog(filename, text, false, { selected: true });
}

/** 调起保存对话框，统一处理成功、取消与失败。 */
async function saveWithDialog(
  filename,
  body,
  base64Encoded,
  { selected = false, truncated = false } = {}
) {
  const btn = $('#btn-save-resp');
  btn.disabled = true;
  try {
    const path = await bridge.saveResponseBody(filename, body, base64Encoded);
    if (!path) return;
    const label = selected ? '选中内容已保存' : '响应体已保存';
    const suffix = truncated && !selected ? '（已截断）' : '';
    emit('toast', {
      message: `${label}${suffix}：${path}`,
      kind: truncated && !selected ? 'warn' : 'success',
    });
  } catch (err) {
    emit('toast', { message: `保存失败：${err?.message || err}`, kind: 'error' });
  } finally {
    const current = state.result;
    btn.disabled = !current || !current.body;
  }
}

/** 响应体右键菜单。 */
function openResponseContextMenu(e) {
  const res = state.result;
  if (!res || !res.body) return;

  e.preventDefault();
  const selected = selectionText($('#resp-body-container'));
  showContextMenu(
    e.clientX,
    e.clientY,
    [
      { label: '全选响应体', action: selectResponseBodyText },
      { label: '复制选中内容', disabled: !selected, action: () => copySelectedResponse(selected) },
      { label: '复制全部', action: copyResponseBody },
      { separator: true },
      { label: '保存选中内容…', disabled: !selected, action: () => saveSelectedResponse(selected) },
      { label: '保存响应体…', action: saveResponseBody },
    ],
    '响应体'
  );
}

/** 响应头右键菜单。 */
function openHeadersContextMenu(e) {
  const res = state.result;
  if (!res || !Object.keys(res.headers || {}).length) return;

  e.preventDefault();
  const selected = selectionText($('#resp-headers'));
  showContextMenu(
    e.clientX,
    e.clientY,
    [
      { label: '复制选中内容', disabled: !selected, action: () => copySelectedResponse(selected) },
      { label: '复制全部响应头', action: () => copyHeaders(res) },
      { separator: true },
      { label: '保存响应头…', action: () => saveHeaders(res) },
    ],
    '响应头'
  );
}

/** 复制格式化后的全部响应头。 */
async function copyHeaders(res) {
  const text = formatHeaders(res?.headers);
  if (!text) {
    emit('toast', { message: '响应中没有头部信息', kind: 'warn' });
    return;
  }
  const ok = await copyText(text);
  emit('toast', {
    message: ok ? '响应头已复制到剪贴板' : '复制失败，请手动选择文本',
    kind: ok ? 'success' : 'error',
  });
}

/** 把响应头保存为文本文件。 */
async function saveHeaders(res) {
  const text = formatHeaders(res?.headers);
  if (!text) {
    emit('toast', { message: '响应中没有头部信息', kind: 'warn' });
    return;
  }

  const filename = headersFilename(suggestResponseFilename(res, state.request));
  try {
    const path = await bridge.saveResponseBody(filename, text, false);
    if (!path) return;
    emit('toast', { message: `响应头已保存：${path}`, kind: 'success' });
  } catch (err) {
    emit('toast', { message: `保存失败：${err?.message || err}`, kind: 'error' });
  }
}

/** 读取指定容器内的选中文本。 */
function selectionText(container) {
  if (!container) return '';
  const selection = window.getSelection();
  if (!selection || selection.isCollapsed || !selection.rangeCount) return '';
  const anchor = selection.anchorNode;
  const focus = selection.focusNode;
  if (!(anchor && container.contains(anchor)) && !(focus && container.contains(focus))) return '';
  return selection.toString();
}

/** 选中响应体文本，优先只选内容本身。 */
function selectResponseBodyText() {
  const body = $('#resp-body-container');
  const target = body?.querySelector('pre.resp-body') || body;
  if (!target) return;
  const details = target.closest('details');
  if (details) details.open = true;

  const range = document.createRange();
  range.selectNodeContents(target);
  const selection = window.getSelection();
  if (!selection) return;
  selection.removeAllRanges();
  selection.addRange(range);
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
    const kind = ERROR_KIND_LABELS[res.errorKind] || '请求失败';
    box.innerHTML = `
      <span class="status-pill status-err">请求失败</span>
      <span>${escapeHtml(kind)}</span>
      ${
        res.durationMs
          ? `<span class="sep">·</span><button type="button" class="link-btn" data-goto-tab="timing">${formatDuration(res.durationMs)}</button>`
          : ''
      }`;
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
    <button type="button" class="link-btn" data-goto-tab="timing" title="${escapeAttr(timing)}">${formatDuration(
      res.durationMs
    )}</button>
    <span class="sep">·</span>
    <span>${formatBytes(res.size)}</span>
    ${res.contentType ? `<span class="sep">·</span><span>${escapeHtml(res.contentType)}</span>` : ''}
    ${
      res.redirects?.length
        ? `<span class="sep">·</span><span>${res.redirects.length} 次跳转</span>`
        : ''
    }
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
    const contentType = String(res.contentType || '').split(';')[0].trim().toLowerCase();
    const canPreview =
      contentType.startsWith('image/') &&
      !res.truncated &&
      Boolean(res.body) &&
      res.body.length <= 8 * 1024 * 1024;

    if (canPreview) {
      parts.push(
        `<div class="resp-notice">二进制响应（${escapeHtml(contentType)}，${formatBytes(
          res.size
        )}），已按图片预览。</div>`
      );
      parts.push(
        `<div class="resp-image-wrap"><img class="resp-image" alt="响应图片" src="data:${escapeAttr(
          contentType
        )};base64,${escapeAttr(res.body)}" /></div>`
      );
      parts.push(
        `<details class="resp-binary"><summary>查看 Base64</summary><pre class="resp-body raw">${escapeHtml(
          res.body
        )}</pre></details>`
      );
    } else {
      const reason = res.truncated ? '响应体已截断，' : '';
      parts.push(
        `<div class="resp-notice">这是二进制响应（${escapeHtml(
          res.contentType || '未知类型'
        )}，${formatBytes(res.size)}），${reason}下方以 Base64 展示。</div>`
      );
      parts.push(`<pre class="resp-body raw">${escapeHtml(res.body)}</pre>`);
    }
  } else if (res.body) {
    const tooLarge = res.body.length > MAX_PRETTY_BYTES;
    const pretty = tooLarge ? null : prettyJSON(res.body);
    if (state.responseView === 'pretty' && pretty !== null) {
      parts.push(`<pre class="resp-body">${highlightJSON(pretty)}</pre>`);
    } else {
      if (tooLarge) {
        parts.push(
          `<div class="resp-notice">响应体较大（${formatBytes(
            res.body.length
          )}），已跳过 JSON 美化与高亮，按原始文本展示。</div>`
        );
      }
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
  const filter = $('#headers-filter');
  const count = $('#headers-count');
  const res = state.result;

  if (!res) {
    if (filter) filter.disabled = true;
    if (count) count.textContent = '';
    box.innerHTML = '<div class="hint">还没有响应。</div>';
    return;
  }

  const all = Object.entries(res.headers || {});
  if (filter) filter.disabled = !all.length;
  if (!all.length) {
    if (count) count.textContent = '';
    box.innerHTML = '<div class="hint">响应中没有头部信息。</div>';
    return;
  }

  const query = filter?.value.trim() || '';
  const entries = filterHeaders(res.headers, query);
  if (count) {
    count.textContent = query ? `${entries.length} / ${all.length} 项` : `${all.length} 项`;
  }
  if (!entries.length) {
    box.innerHTML = '<div class="hint">没有匹配的响应头。</div>';
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

/** 渲染链路耗时明细。 */
function renderTiming() {
  const box = $('#resp-timing');
  const res = state.result;
  if (!res) {
    box.innerHTML = `
      <div class="resp-placeholder">
        <div class="big">⏱</div>
        <div>发送请求后，这里会显示各阶段耗时</div>
      </div>`;
    return;
  }

  const cards = [
    { label: '总耗时', value: formatDuration(res.durationMs), desc: '发出到读完响应体' },
    { label: 'DNS 解析', value: res.dnsMs > 0 ? formatDuration(res.dnsMs) : '—', desc: '域名解析' },
    { label: 'TCP 连接', value: res.connectMs > 0 ? formatDuration(res.connectMs) : '—', desc: '建立连接' },
    { label: 'TLS 握手', value: res.tlsMs > 0 ? formatDuration(res.tlsMs) : '—', desc: 'HTTPS 握手' },
    { label: '首字节 TTFB', value: res.ttfbMs > 0 ? formatDuration(res.ttfbMs) : '—', desc: '等待服务端首字节' },
  ];

  const grid = cards
    .map(
      (c) => `
        <div class="timing-card">
          <div class="timing-label">${c.label}</div>
          <div class="timing-value">${c.value}</div>
          <div class="timing-desc">${c.desc}</div>
        </div>`
    )
    .join('');

  const meta = [];
  if (res.proto) meta.push(`协议 ${escapeHtml(res.proto)}`);
  if (res.remoteAddr) meta.push(`远端 ${escapeHtml(res.remoteAddr)}`);
  if (res.redirects?.length) meta.push(`跟随 ${res.redirects.length} 次重定向`);
  meta.push(`响应体 ${formatBytes(res.size)}`);

  box.innerHTML = `
    <div class="timing-wrap">
      ${res.error ? '<div class="resp-notice">请求未成功完成，以上为已采集到的耗时。</div>' : ''}
      <div class="timing-grid">${grid}</div>
      <div class="timing-meta">${meta.join(' · ')}</div>
    </div>`;
}
