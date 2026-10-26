// app.js —— 应用装配：启动加载、发送请求、保存集合、设置与快捷键。

import { $, $$, escapeHtml, escapeAttr, toast, uid, copyText } from './util.js';
import { state, on, emit, findItem, flattenTargets } from './store.js';
import { bridge, backendReady } from './bridge.js';
import { initRequest, normalizeRequest, markClean, markDirty } from './request.js';
import { initResponse, renderResult } from './response.js';
import { initSidebar, renderTree, renderEnvs, renderHistory, renderEnvSelect, revealActiveRequest } from './sidebar.js';
import { initTabs, renderTabs, openInTab, restoreTabs, tabsSnapshot } from './tabs.js';

let tabsSaveTimer = null;
let tabsSaveFailed = false;

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
    state.settings = { ...state.settings, ...(data.settings || {}) };
    state.activeEnvId = state.settings.activeEnvId || '';
    restoreTabs(data.tabs || [], data.activeTabKey || '');
    state.appInfo = (await bridge.appInfo()) || {};
  } catch (err) {
    toast(`加载数据失败：${err}`, 'error');
  }

  // 上次选中的环境可能已经被删除，回退到「不指定环境」。
  if (!state.environments.some((e) => !e.isGlobal && e.id === state.activeEnvId)) {
    state.activeEnvId = '';
  }

  subscribe();
  initTabs();
  applyTheme(state.settings.theme);
  initRequest();
  initResponse();
  initSidebar();
  initTopbar();
  initSplitter();
  initShortcuts();
  initDialogs();
  renderResult(null);
  updatePathLabel();
  renderTabs();
  $('#url-input').focus();

  window.addEventListener('beforeunload', flushTabsSave);
  document.addEventListener('visibilitychange', () => {
    if (document.hidden) flushTabsSave();
  });
}

/** 订阅状态变化，触发持久化与重绘。 */
function subscribe() {
  on('collections', () => {
    bridge.saveCollections(state.collections).catch((e) => toast(`保存集合失败：${e}`, 'error'));
  });
  on('environments', () => {
    bridge.saveEnvironments(state.environments).catch((e) => toast(`保存环境失败：${e}`, 'error'));
  });
  on('history', () => renderHistory());
  on('toast', ({ message, kind }) => toast(message, kind));
  on('dirty-changed', () => {
    updatePathLabel();
    renderTabs();
    queueTabsSave();
  });
  on('tab-meta-changed', () => {
    renderTabs();
    queueTabsSave();
  });
  on('request-touched', () => queueTabsSave());
  // 标签页结构/活动标签变化需要尽快落盘，避免刚切换就退出时丢失。
  on('tabs-changed', () => flushTabsSave());

  // 集合树、历史记录都通过标签页打开请求。
  on('open-request', ({ request }) => openInTab(request));

  // 环境切换统一在这里重绘并记住选择。
  on('env-changed', () => {
    state.settings.activeEnvId = state.activeEnvId;
    renderEnvs();
    renderEnvSelect();
    bridge.saveSettings(state.settings).catch((e) => toast(`保存环境选择失败：${e}`, 'error'));
  });

  on('request-selected', () => {
    updatePathLabel();
    revealActiveRequest();
    renderTree();
    renderTabs();
  });
}

/** 合并短时间内的标签页变化，再统一写入后端。 */
function queueTabsSave() {
  if (tabsSaveTimer) clearTimeout(tabsSaveTimer);
  tabsSaveTimer = setTimeout(flushTabsSave, 400);
}

/** 立即持久化当前标签页状态。 */
function flushTabsSave() {
  if (tabsSaveTimer) {
    clearTimeout(tabsSaveTimer);
    tabsSaveTimer = null;
  }
  if (!state.tabs.length) return;

  const snapshot = tabsSnapshot();
  bridge
    .saveTabs(snapshot.tabs, snapshot.activeTabKey)
    .then(() => {
      tabsSaveFailed = false;
    })
    .catch((err) => {
      if (!tabsSaveFailed) {
        tabsSaveFailed = true;
        toast(`保存标签页失败：${err}`, 'error');
      }
    });
}

/** 顶栏控件。 */
function initTopbar() {
  $('#env-select').addEventListener('change', (e) => {
    state.activeEnvId = e.target.value;
    emit('env-changed');
  });

  $('#btn-new-request').addEventListener('click', () => {
    openInTab(normalizeRequest({}));
    $('#url-input').focus();
  });

  $('#btn-send').addEventListener('click', send);
  $('#btn-save').addEventListener('click', () => saveRequest(false));
  $('#btn-save-as').addEventListener('click', () => saveRequest(true));
  $('#btn-settings').addEventListener('click', openSettings);
  $('#btn-import-curl').addEventListener('click', openCurlImport);
  $('#btn-copy-curl').addEventListener('click', copyCurl);
}

/** 应用主题。 */
function applyTheme(theme) {
  document.documentElement.setAttribute('data-theme', theme === 'light' ? 'light' : 'dark');
}

/** 发送请求。 */
async function send() {
  if (state.sending) return;
  const btn = $('#btn-send');
  const tabKey = state.activeTabKey;
  const tab = state.tabs.find((t) => t.key === tabKey);
  if (!tab) return;

  state.sending = true;
  btn.disabled = true;
  btn.innerHTML = '<span class="spinner"></span> 发送中';

  try {
    const res = await bridge.sendRequest(tab.request, state.activeEnvId, {
      timeoutMs: state.settings.timeoutMs,
      followRedirects: state.settings.followRedirects,
      insecureTls: state.settings.insecureTls,
    });

    // 结果写回发起请求的标签页；发送期间切换标签页也不会串结果。
    const target = state.tabs.find((t) => t.key === tabKey);
    if (target) {
      target.result = res;
      target.resultRequestId = target.request.id || '';
      if (state.activeTabKey === tabKey) {
        renderResult(res);
      }
    }
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
    // 名称输入框被清空时，用集合里的旧名字回填，保持界面一致。
    state.request.name = existing.name;
    const nameInput = $('#req-name');
    if (nameInput) nameInput.value = existing.name;
    if (existing.request) {
      existing.request = normalizeRequest({ ...state.request, id: existing.request.id });
      existing.request.name = existing.name;
    }
    emit('collections');
    renderTree();
    markClean();
    updatePathLabel();
    renderTabs();
    flushTabsSave();
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
      (t) => `<option value="${escapeAttr(t.id)}">${'　'.repeat(t.depth)}${escapeHtml(t.name)}</option>`
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
  backdrop.addEventListener('keydown', (e) => {
    if (e.key === 'Enter' && e.target.tagName === 'INPUT') {
      e.preventDefault();
      backdrop.querySelector('[data-act="ok"]').click();
    }
  });
  backdrop.addEventListener('click', (e) => {
    const act = e.target.closest('[data-act]')?.dataset.act;
    if (act === 'cancel' || e.target === backdrop) {
      close();
      return;
    }
    if (act !== 'ok') return;

    const name = backdrop.querySelector('#save-name').value.trim() || '未命名请求';
    const targetId = backdrop.querySelector('#save-target').value;
    const previousId = state.request.id || '';

    const req = normalizeRequest({ ...state.request, name });
    // 对话框总是新建一个节点，因此请求 ID 也要重新生成；
    // 否则「另存为」出来的副本会与原请求共用 ID。
    req.id = uid();

    const node = {
      id: uid(),
      type: 'request',
      name,
      // 集合里存一份独立副本，避免之后编辑标签页时直接改动已保存节点。
      request: normalizeRequest(req),
    };

    const parent = targetId ? findItem(state.collections, targetId).item : null;
    if (parent) {
      parent.items = parent.items || [];
      parent.items.push(node);
    } else {
      state.collections.push(node);
    }

    state.request = req;
    const nameInput = $('#req-name');
    if (nameInput) nameInput.value = req.name;
    if (state.result && state.resultRequestId === previousId) {
      state.resultRequestId = req.id;
    }
    emit('collections');
    renderTree();
    markClean();
    updatePathLabel();
    renderTabs();
    flushTabsSave();
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
  const dirty = Boolean(state.dirty);
  label.classList.toggle('dirty', dirty);
  label.title = dirty ? '当前请求有未保存的修改，按 Ctrl+S 保存' : '';

  if (!state.request.id) {
    label.textContent = dirty ? '未保存的草稿' : '未保存';
    return;
  }
  const { item } = findItem(state.collections, state.request.id);
  if (!item) {
    label.textContent = dirty ? '未保存的草稿' : '未保存';
    return;
  }
  label.textContent = dirty ? `已保存：${item.name} · 有未保存修改` : `已保存：${item.name}`;
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
          <div>
            <div class="label">会话 Cookie</div>
            <div class="desc">登录产生的 Cookie 会在本次运行期间自动保留</div>
          </div>
          <button type="button" class="btn btn-sm" id="btn-clear-cookies">清空</button>
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
            <div class="label">数据备份</div>
            <div class="desc">导出或导入集合与环境；导入会生成新 ID 并合并现有数据</div>
          </div>
          <div class="settings-actions">
            <button type="button" class="btn btn-sm" id="btn-export-bundle">导出</button>
            <button type="button" class="btn btn-sm" id="btn-import-bundle">导入</button>
          </div>
        </div>
        <div class="setting-row">
          <div>
            <div class="label">数据文件</div>
            <div class="desc">${escapeHtml(state.appInfo.dataFile || '未知')}</div>
          </div>
        </div>
        <div class="setting-row">
          <div>
            <div class="label">版本</div>
            <div class="desc">NanoAPI ${escapeHtml(state.appInfo.version || '未知版本')}</div>
          </div>
        </div>
      </div>
      <div class="modal-actions">
        <button type="button" class="btn" data-act="cancel">取消</button>
        <button type="button" class="btn btn-primary" data-act="ok">保存</button>
      </div>
    </div>`;

  const close = () => backdrop.remove();
  backdrop.addEventListener('keydown', (e) => {
    if (e.key === 'Enter' && e.target.tagName === 'INPUT') {
      e.preventDefault();
      backdrop.querySelector('[data-act="ok"]').click();
    }
  });
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
      activeEnvId: state.activeEnvId,
    };
    applyTheme(state.settings.theme);
    bridge.saveSettings(state.settings).catch((e) => toast(`保存设置失败：${e}`, 'error'));
    close();
    toast('设置已保存', 'success');
  });

  backdrop.querySelector('#btn-clear-cookies').addEventListener('click', async (e) => {
    e.stopPropagation();
    const btn = e.currentTarget;
    btn.disabled = true;
    try {
      await bridge.clearCookies();
      toast('Cookie 已清空', 'success');
    } catch (err) {
      toast(`清空 Cookie 失败：${err}`, 'error');
    } finally {
      btn.disabled = false;
    }
  });

  const exportBtn = backdrop.querySelector('#btn-export-bundle');
  exportBtn.addEventListener('click', async (e) => {
    e.stopPropagation();
    exportBtn.disabled = true;
    try {
      const result = await bridge.exportBundle();
      if (result.canceled) return;
      toast(
        `已导出 ${result.collections} 个集合 / ${result.requests} 个请求 / ${result.environments} 个环境`,
        'success'
      );
    } catch (err) {
      toast(`导出备份失败：${err}`, 'error');
    } finally {
      exportBtn.disabled = false;
    }
  });

  const importBtn = backdrop.querySelector('#btn-import-bundle');
  importBtn.addEventListener('click', async (e) => {
    e.stopPropagation();
    importBtn.disabled = true;
    try {
      const result = await bridge.importBundle();
      if (result.canceled) return;

      const data = await bridge.loadAll();
      state.collections = data.collections || [];
      state.environments = data.environments || [];
      renderTree();
      renderEnvs();
      renderEnvSelect();
      updatePathLabel();

      toast(
        `已导入 ${result.collections} 个集合 / ${result.requests} 个请求 / ${result.environments} 个环境`,
        'success'
      );
      const warnings = result.warnings || [];
      warnings.slice(0, 3).forEach((w) => toast(`导入提示：${w}`, 'warn'));
      if (warnings.length > 3) {
        toast(`还有 ${warnings.length - 3} 条导入提示`, 'warn');
      }
    } catch (err) {
      toast(`导入备份失败：${err}`, 'error');
    } finally {
      importBtn.disabled = false;
    }
  });

  document.body.appendChild(backdrop);
}

/** 导入 cURL 命令。 */
function openCurlImport() {
  const backdrop = document.createElement('div');
  backdrop.className = 'modal-backdrop';
  backdrop.innerHTML = `
    <div class="modal">
      <div class="modal-title">导入 cURL</div>
      <div class="modal-body">
        <div class="hint">粘贴 curl 命令，支持 -X / -H / -d / --data-raw / --data-binary / --json / -F / -u 等常用参数。</div>
        <textarea id="curl-input" class="textarea curl-textarea" spellcheck="false" placeholder="curl 'https://api.example.com/users' -H 'Accept: application/json'"></textarea>
        <div id="curl-error" class="modal-error hidden"></div>
      </div>
      <div class="modal-actions">
        <button type="button" class="btn" data-act="cancel">取消</button>
        <button type="button" class="btn btn-primary" data-act="ok">导入</button>
      </div>
    </div>`;

  const textarea = backdrop.querySelector('#curl-input');
  const errorBox = backdrop.querySelector('#curl-error');
  const okBtn = backdrop.querySelector('[data-act="ok"]');
  let cancelled = false;
  const close = () => {
    cancelled = true;
    backdrop.remove();
  };

  const showError = (message) => {
    errorBox.textContent = message;
    errorBox.classList.remove('hidden');
  };

  const doImport = async () => {
    if (okBtn.disabled) return;
    const command = textarea.value.trim();
    if (!command) {
      showError('请先粘贴 cURL 命令');
      textarea.focus();
      return;
    }
    errorBox.classList.add('hidden');
    okBtn.disabled = true;
    try {
      const result = await bridge.parseCurl(command);
      if (cancelled) return;

      close();
      openInTab(result.request);
      markDirty();
      flushTabsSave();
      $('#url-input').focus();
      toast('cURL 已导入', 'success');

      const warnings = result.warnings || [];
      warnings.slice(0, 3).forEach((w) => toast(`导入提示：${w}`, 'warn'));
      if (warnings.length > 3) {
        toast(`还有 ${warnings.length - 3} 条导入提示`, 'warn');
      }
    } catch (err) {
      showError(String(err?.message || err));
    } finally {
      okBtn.disabled = false;
    }
  };

  backdrop.addEventListener('keydown', (e) => {
    if ((e.ctrlKey || e.metaKey) && e.key === 'Enter') {
      e.preventDefault();
      doImport();
    }
  });
  backdrop.addEventListener('click', (e) => {
    const act = e.target.closest('[data-act]')?.dataset.act;
    if (act === 'cancel' || e.target === backdrop) {
      close();
      return;
    }
    if (act === 'ok') doImport();
  });

  document.body.appendChild(backdrop);
  textarea.focus();
}

/** 使用当前环境解析变量并复制 cURL 命令。 */
async function copyCurl() {
  const btn = $('#btn-copy-curl');
  btn.disabled = true;
  try {
    const result = await bridge.buildCurl(state.request, state.activeEnvId);
    const ok = await copyText(result.command || '');
    if (!ok) {
      toast('复制 cURL 失败，请重试', 'error');
      return;
    }
    toast('cURL 已复制到剪贴板', 'success');

    const warnings = result.warnings || [];
    warnings.slice(0, 2).forEach((w) => toast(`cURL 提示：${w}`, 'warn'));
    if (warnings.length > 2) {
      toast(`还有 ${warnings.length - 2} 条提示`, 'warn');
    }
  } catch (err) {
    toast(`生成 cURL 失败：${err}`, 'error');
  } finally {
    btn.disabled = false;
  }
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

  // 双击分割条恢复默认比例。
  splitter.addEventListener('dblclick', () => {
    main.style.removeProperty('--req-h');
  });
}

/** 键盘快捷键。 */
function initShortcuts() {
  document.addEventListener('keydown', (e) => {
    const ctrl = e.ctrlKey || e.metaKey;
    if (!ctrl) return;
    // 弹窗打开时，避免快捷键操作到背后的请求。
    if (document.querySelector('.modal-backdrop')) return;

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

/** 全局 Escape 关闭最上层弹窗。 */
function initDialogs() {
  document.addEventListener('keydown', (e) => {
    if (e.key !== 'Escape') return;
    const backdrops = $$('.modal-backdrop');
    if (!backdrops.length) return;
    const top = backdrops[backdrops.length - 1];
    const cancel = top.querySelector('[data-act="cancel"]');
    if (cancel) cancel.click();
    else top.remove();
  });
}

boot();
