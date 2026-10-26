// tabs.js —— 请求标签页：每个标签独立保存请求、脏状态与响应。

import { $, confirmDialog } from './util.js';
import { state, emit } from './store.js';
import { normalizeRequest, syncRequestEditor } from './request.js';
import { renderResult } from './response.js';

function newTabKey() {
  return `tab-${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 7)}`;
}

function createTab(request) {
  return {
    key: newTabKey(),
    request,
    dirty: false,
    result: null,
    resultRequestId: '',
  };
}

/** 空白且未产生任何结果或修改的标签页可以复用。 */
function isPristine(tab) {
  if (!tab || tab.dirty || tab.result) return false;
  const req = tab.request;
  if (!req || req.id || req.url) return false;
  if (req.name && req.name !== '新建请求') return false;
  if ((req.method || 'GET') !== 'GET') return false;
  if ((req.params || []).some((r) => r.key || r.value)) return false;
  if ((req.headers || []).some((r) => r.key || r.value)) return false;
  const body = req.body || {};
  if (body.type !== 'none' || body.raw || (body.files || []).length) return false;
  if ((req.auth || {}).type !== 'none') return false;
  return true;
}

function applyTab(tab) {
  state.activeTabKey = tab.key;
  syncRequestEditor();
  renderResult(tab.result);
  renderTabs();
  const pane = document.querySelector('.request-pane');
  if (pane) pane.scrollTop = 0;
  emit('request-selected');
  emit('tabs-changed');
}

/** 激活指定标签页。 */
export function activateTab(key) {
  const tab = state.tabs.find((t) => t.key === key);
  if (!tab || state.activeTabKey === key) return;
  applyTab(tab);
}

/**
 * 在标签页中打开请求。
 * 同一个已保存请求会复用已有标签；空白标签也可以复用。
 */
export function openInTab(rawRequest, opts = {}) {
  const req = normalizeRequest(rawRequest);
  const existing =
    req.id && !opts.forceNew ? state.tabs.find((t) => t.request.id === req.id) : null;
  if (existing) {
    activateTab(existing.key);
    return existing;
  }

  const active = state.activeTab;
  let tab;
  if (!opts.forceNew && active && isPristine(active)) {
    tab = active;
    tab.request = req;
    tab.dirty = false;
    tab.result = null;
    tab.resultRequestId = '';
  } else {
    tab = createTab(req);
    state.tabs.push(tab);
  }
  applyTab(tab);
  return tab;
}

/** 关闭标签页；有未保存修改时先确认。 */
export async function closeTab(key) {
  const tab = state.tabs.find((t) => t.key === key);
  if (!tab) return;
  if (tab.dirty) {
    const ok = await confirmDialog(
      `「${tab.request.name || '未命名请求'}」还有未保存修改，确定关闭吗？`,
      '关闭标签页'
    );
    if (!ok) return;
  }

  const index = state.tabs.indexOf(tab);
  const wasActive = state.activeTabKey === key;
  state.tabs.splice(index, 1);

  if (!state.tabs.length) {
    const fresh = createTab(normalizeRequest({}));
    state.tabs.push(fresh);
    applyTab(fresh);
    $('#url-input').focus();
    return;
  }

  if (wasActive) {
    const next = state.tabs[Math.min(index, state.tabs.length - 1)] || state.tabs[0];
    applyTab(next);
    $('#url-input').focus();
  } else {
    renderTabs();
    emit('tabs-changed');
  }
}

/** 渲染顶部标签栏。 */
export function renderTabs() {
  const bar = $('#request-tabs');
  if (!bar) return;

  bar.innerHTML = '';
  for (const tab of state.tabs) {
    const item = document.createElement('div');
    item.className = `request-tab${tab.key === state.activeTabKey ? ' active' : ''}`;
    item.dataset.tabKey = tab.key;
    item.title = tab.request.name || '未命名请求';

    const method = document.createElement('span');
    const methodName = (tab.request.method || 'GET').toUpperCase();
    method.className = `method method-${methodName}`;
    method.textContent = methodName;

    const name = document.createElement('span');
    name.className = 'request-tab-name';
    name.textContent = tab.request.name || '未命名请求';

    item.append(method, name);

    if (tab.dirty) {
      const dot = document.createElement('span');
      dot.className = 'tab-dirty';
      dot.title = '有未保存修改';
      item.appendChild(dot);
    }

    const close = document.createElement('button');
    close.type = 'button';
    close.className = 'tab-close';
    close.dataset.tabClose = tab.key;
    close.title = '关闭标签页';
    close.textContent = '✕';
    item.appendChild(close);

    bar.appendChild(item);
  }

  const add = document.createElement('button');
  add.type = 'button';
  add.className = 'tab-new';
  add.dataset.tabNew = '1';
  add.title = '新建请求标签页';
  add.textContent = '＋';
  bar.appendChild(add);

  const activeItem = bar.querySelector('.request-tab.active');
  if (activeItem) {
    requestAnimationFrame(() =>
      activeItem.scrollIntoView({ block: 'nearest', inline: 'nearest' })
    );
  }
}

/** 初始化标签栏，并保证始终至少有一个标签页。 */
export function initTabs() {
  if (!state.tabs.length) {
    state.tabs.push(createTab(normalizeRequest({})));
    state.activeTabKey = state.tabs[0].key;
  }

  const bar = $('#request-tabs');
  if (!bar) return;

  bar.addEventListener('click', (e) => {
    const close = e.target.closest('[data-tab-close]');
    if (close) {
      closeTab(close.dataset.tabClose);
      return;
    }
    if (e.target.closest('[data-tab-new]')) {
      openInTab({}, { forceNew: true });
      $('#url-input').focus();
      return;
    }
    const item = e.target.closest('[data-tab-key]');
    if (item) activateTab(item.dataset.tabKey);
  });

  bar.addEventListener('auxclick', (e) => {
    if (e.button !== 1) return;
    const item = e.target.closest('[data-tab-key]');
    if (!item) return;
    e.preventDefault();
    closeTab(item.dataset.tabKey);
  });

  renderTabs();
}

/** 从持久化数据恢复标签页；无效数据会被忽略。 */
export function restoreTabs(rawTabs, activeKey) {
  const restored = [];
  for (const raw of Array.isArray(rawTabs) ? rawTabs : []) {
    if (!raw || typeof raw !== 'object') continue;
    let key = typeof raw.key === 'string' && raw.key.trim() ? raw.key.trim() : newTabKey();
    while (restored.some((tab) => tab.key === key)) {
      key = newTabKey();
    }
    restored.push({
      key,
      request: normalizeRequest(raw.request || {}),
      dirty: Boolean(raw.dirty),
      result: null,
      resultRequestId: '',
    });
  }

  if (!restored.length) {
    restored.push(createTab(normalizeRequest({})));
  }

  let active = typeof activeKey === 'string' ? activeKey : '';
  if (!restored.some((tab) => tab.key === active)) {
    active = restored[0].key;
  }
  state.tabs = restored;
  state.activeTabKey = active;
}

/** 生成用于持久化的标签页快照（不包含响应）。 */
export function tabsSnapshot() {
  return {
    tabs: state.tabs.map((tab) => ({
      key: tab.key,
      request: tab.request,
      dirty: Boolean(tab.dirty),
    })),
    activeTabKey: state.activeTabKey,
  };
}
