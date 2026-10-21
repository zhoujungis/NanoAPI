// app.js —— 应用装配：启动加载、发送请求、保存集合、设置与快捷键。

import { $, escapeHtml, escapeAttr, toast, confirmDialog } from './util.js';
import { state, on, emit, findItem, flattenTargets } from './store.js';
import { bridge, backendReady } from './bridge.js';
import { initRequest, loadRequest, normalizeRequest } from './request.js';
import { initResponse, renderResult } from './response.js';
import { initSidebar, addCollection, addRequest, renderTree, renderEnvs, renderHistory, renderEnvSelect } from './sidebar.js';

/** 启动应用。 */
async function boot() {
  if (!backendReady()) {
    document.body.innerHTML =
      '<div style="padding:40px;font-family:system-ui;color:#e3e7ee;background:#171a20;height:100vh">' +
      '后端尚未就绪，请重新启动 NanoAPI。</div>';
    return;
  }

  try {
    const data = await bridge.loadAll();
    state.collections = data.collections || [];
    state.environments = data.environments || [];
    state.history = data.history || [];
    state.settings = data.settings || state.settings;
    state.appInfo = (await bridge.appInfo()) || {};
  } catch (err) {
    toast(`加载数据失败：${err}`, 'error');
  }

  applyTheme(state.settings.theme);
  initRequest();
  initResponse();
  initSidebar();
  initTopbar();
  initSplitter();
  initShortcuts();
  renderResult(null);
  updatePathLabel();

  // 订阅状态变化，触发持久化与重绘。
  on('collections', () => {
    bridge.saveCollections(state.collections).catch((e) => toast(`保存集合失败：${e}`, 'error'));
  });
  on('environments', () => {
    bridge.saveEnvironments(state.environments).catch((e) => toast(`保存环境失败：${e}`, 'error'));
  });
  on('history', () => renderHistory());
  on('toast', ({ message, kind }) => toast(message, kind));
  on('request-selected', () => updatePathLabel());
}

/** 顶栏控件。 */
function initTopbar() {
  $('#env-select').addEventListener('change', (e) => {
    state.activeEnvId = e.target.value;
    emit('env-changed');
  });

  $('#btn-new-request').addEventListener('click', () => {
    loadRequest(normalizeRequest({}));
    state.result = null;
    renderResult(null);
    updatePathLabel();
  });

  $('#btn-send').addEventListener('click', send);
  $('#btn-save').addEventListener('click', () => saveRequest(false));
  $('#btn-save-as').addEventListener('click', () => saveRequest(true));
  $('#btn-settings').addEventListener('click', openSettings);
}

/** 应用主题。 */
function applyTheme(theme) {
  document.documentElement.setAttribute('data-theme', theme === 'light' ? 'light' : 'dark');
}

/** 发送请求。 */
async function send() {
  if (state.sending) return;
  const btn = $('#btn-send');
  state.sending = true;
  btn.disabled = true;
  btn.innerHTML = '<span class="spinner"></span> 发送中';

  try {
    const res = await bridge.sendRequest(state.request, state.activeEnvId, {
      timeoutMs: state.settings.timeoutMs,
      followRedirects: state.settings.followRedirects,
      insecureTls: state.settings.insecureTls,
    });
    renderResult(res);
    await refreshHistory();
  } catch (err) {
    toast(`发送失败：${err}`, 'error');
  } finally {
    state.sending = false;
    btn.disabled = false;
    btn.textContent = '发送';
  }
}

/** 从后端刷新历史，保证与后端记录一致。 */
async function refreshHistory() {
  try {
    const data = await bridge.loadAll();
    state.history = data.history || [];
    renderHistory();
  } catch {
    /* 刷新失败不影响本次结果展示 */
  }
}

/**
 * 保存请求到集合。
 * @param {boolean} forceAs 强制弹出「另存为」对话框
 */
function saveRequest(forceAs) {
  const existing = state.request.id ? findItem(state.collections, state.request.id).item : null;
  if (existing && !forceAs) {
    existing.name = state.request.name || existing.name;
    if (existing.request) {
      existing.request = normalizeRequest({ ...state.request, id: existing.request.id });
      existing.request.name = existing.name;
    }
    emit('collections');
    renderTree();
    updatePathLabel();
    toast('请求已更新', 'success');
    return;
  }
  openSaveDialog(existing);
}

/** 「保存到集合」对话框。 */
function openSaveDialog(existing) {
  const targets = flattenTargets(state.collections);
  const options = ['<option value="">（顶层）</option>'].concat(
    targets.map(
      (t) => `<option value="${t.id}">${'　'.repeat(t.depth)}${escapeHtml(t.name)}</option>`
    )
  );

  const backdrop = document.createElement('div');
  backdrop.className = 'modal-backdrop';
  backdrop.innerHTML = `
    <div class="modal">
      <div class="modal-title">${existing ? '另存为' : '保存请求'}</div>
      <div class="modal-body">
        <div>
          <div class="hint" style="margin-bottom:6px">名称</div>
          <input id="save-name" class="input" value="${escapeAttr(state.request.name || '新建请求')}" />
        </div>
        <div>
          <div class="hint" style="margin-bottom:6px">保存到</div>
          <select id="save-target" class="select">${options.join('')}</select>
        </div>
        ${
          targets.length
            ? ''
            : '<div class="hint">还没有任何集合，请求会保存到顶层。先在左侧新建一个集合会更方便。</div>'
        }
      </div>
      <div class="modal-actions">
        <button type="button" class="btn" data-act="cancel">取消</button>
        <button type="button" class="btn btn-primary" data-act="ok">保存</button>
      </div>
    </div>`;

  const close = () => backdrop.remove();
  backdrop.addEventListener('click', (e) => {
    const act = e.target.closest('[data-act]')?.dataset.act;
    if (act === 'cancel' || e.target === backdrop) {
      close();
      return;
    }
    if (act !== 'ok') return;

    const name = backdrop.querySelector('#save-name').value.trim() || '未命名请求';
    const targetId = backdrop.querySelector('#save-target').value;

    const req = normalizeRequest({ ...state.request, name });
    // 对话框总是新建一个节点，因此请求 ID 也要重新生成；
    // 否则「另存为」出来的副本会与原请求共用 ID。
    req.id = newId();

    const node = {
      id: newId(),
      type: 'request',
      name,
      request: req,
    };

    const parent = targetId ? findItem(state.collections, targetId).item : null;
    if (parent) {
      parent.items = parent.items || [];
      parent.items.push(node);
    } else {
      state.collections.push(node);
    }

    state.request = req;
    emit('collections');
    renderTree();
    updatePathLabel();
    close();
    toast(`已保存到「${parent ? parent.name : '顶层'}」`, 'success');
  });

  document.body.appendChild(backdrop);
  backdrop.querySelector('#save-name').focus();
  backdrop.querySelector('#save-name').select();
}

/** 更新请求标题栏上的保存位置。 */
function updatePathLabel() {
  const label = $('#req-path');
  if (!state.request.id) {
    label.textContent = '未保存';
    return;
  }
  const { item } = findItem(state.collections, state.request.id);
  label.textContent = item ? `已保存：${item.name}` : '未保存';
}

/** 设置对话框。 */
function openSettings() {
  const s = state.settings;
  const backdrop = document.createElement('div');
  backdrop.className = 'modal-backdrop';
  backdrop.innerHTML = `
    <div class="modal">
      <div class="modal-title">设置</div>
      <div class="modal-body">
        <div class="setting-row">
          <div>
            <div class="label">默认超时</div>
            <div class="desc">单位毫秒，超时后中止请求</div>
          </div>
          <input id="set-timeout" class="input" type="number" min="100" step="500" value="${s.timeoutMs || 30000}" />
        </div>
        <div class="setting-row">
          <div>
            <div class="label">跟随重定向</div>
            <div class="desc">关闭后只返回首个响应，并在响应头中显示 Location</div>
          </div>
          <label class="checkbox-row"><input id="set-redirect" type="checkbox" ${s.followRedirects ? 'checked' : ''} /></label>
        </div>
        <div class="setting-row">
          <div>
            <div class="label">跳过 TLS 证书校验</div>
            <div class="desc">仅用于自签名证书的测试环境，请谨慎开启</div>
          </div>
          <label class="checkbox-row"><input id="set-insecure" type="checkbox" ${s.insecureTls ? 'checked' : ''} /></label>
        </div>
        <div class="setting-row">
          <div><div class="label">主题</div></div>
          <select id="set-theme" class="select">
            <option value="dark" ${s.theme !== 'light' ? 'selected' : ''}>暗色</option>
            <option value="light" ${s.theme === 'light' ? 'selected' : ''}>亮色</option>
          </select>
        </div>
        <div class="setting-row">
          <div>
            <div class="label">数据文件</div>
            <div class="desc">${escapeHtml(state.appInfo.dataFile || '未知')}</div>
          </div>
        </div>
      </div>
      <div class="modal-actions">
        <button type="button" class="btn" data-act="cancel">取消</button>
        <button type="button" class="btn btn-primary" data-act="ok">保存</button>
      </div>
    </div>`;

  const close = () => backdrop.remove();
  backdrop.addEventListener('click', (e) => {
    const act = e.target.closest('[data-act]')?.dataset.act;
    if (act === 'cancel' || e.target === backdrop) {
      close();
      return;
    }
    if (act !== 'ok') return;

    const timeout = Number(backdrop.querySelector('#set-timeout').value);
    state.settings = {
      ...state.settings,
      timeoutMs: Number.isFinite(timeout) && timeout > 0 ? timeout : 30000,
      followRedirects: backdrop.querySelector('#set-redirect').checked,
      insecureTls: backdrop.querySelector('#set-insecure').checked,
      theme: backdrop.querySelector('#set-theme').value,
    };
    applyTheme(state.settings.theme);
    bridge.saveSettings(state.settings).catch((e) => toast(`保存设置失败：${e}`, 'error'));
    close();
    toast('设置已保存', 'success');
  });

  document.body.appendChild(backdrop);
}

/** 请求区与响应区之间的可拖拽分割条。 */
function initSplitter() {
  const splitter = $('#splitter');
  const main = document.querySelector('.main');
  let dragging = false;

  splitter.addEventListener('mousedown', (e) => {
    dragging = true;
    e.preventDefault();
    document.body.style.cursor = 'row-resize';
    document.body.style.userSelect = 'none';
  });

  window.addEventListener('mousemove', (e) => {
    if (!dragging) return;
    const rect = main.getBoundingClientRect();
    const pct = ((e.clientY - rect.top) / rect.height) * 100;
    main.style.setProperty('--req-h', `${Math.min(85, Math.max(15, pct))}%`);
  });

  window.addEventListener('mouseup', () => {
    if (!dragging) return;
    dragging = false;
    document.body.style.cursor = '';
    document.body.style.userSelect = '';
  });
}

/** 键盘快捷键。 */
function initShortcuts() {
  document.addEventListener('keydown', (e) => {
    const ctrl = e.ctrlKey || e.metaKey;
    if (!ctrl) return;

    if (e.key === 'Enter') {
      e.preventDefault();
      $('#btn-send').click();
    } else if (e.key.toLowerCase() === 's') {
      e.preventDefault();
      $('#btn-save').click();
    } else if (e.key.toLowerCase() === 'k') {
      e.preventDefault();
      $('#url-input').focus();
      $('#url-input').select();
    }
  });
}

/** 生成 ID。 */
function newId() {
  return Math.random().toString(16).slice(2, 10) + Date.now().toString(16).slice(-6);
}

boot();
