// request.js —— 请求构造器：方法、地址、参数、请求头、Body 与认证。

import { $, $$ } from './util.js';
import { state, emit } from './store.js';
import { createKVEditor } from './kv.js';

const METHODS = ['GET', 'POST', 'PUT', 'PATCH', 'DELETE', 'HEAD', 'OPTIONS'];

const BODY_TYPES = [
  { value: 'none', label: '无 Body' },
  { value: 'json', label: 'JSON' },
  { value: 'text', label: '文本 (text)' },
  { value: 'form', label: '表单 (x-www-form-urlencoded)' },
];

const AUTH_TYPES = [
  { value: 'none', label: '无认证' },
  { value: 'basic', label: 'Basic' },
  { value: 'bearer', label: 'Bearer Token' },
  { value: 'apikey', label: 'API Key' },
];

const editors = {};

/** 深拷贝一组键值行，避免编辑时直接改动集合里已保存的数据。 */
function cloneRows(rows) {
  if (!Array.isArray(rows)) return [];
  return rows.map((r) => ({
    key: r?.key ?? '',
    value: r?.value ?? '',
    enabled: r?.enabled !== false,
    desc: r?.desc ?? '',
  }));
}

/** 补齐缺失字段，保证编辑器拿到结构完整的请求。 */
export function normalizeRequest(raw) {
  const req = raw && typeof raw === 'object' ? { ...raw } : {};
  req.id = req.id || '';
  req.name = req.name || '新建请求';
  req.method = (req.method || 'GET').toUpperCase();
  req.url = req.url || '';
  req.params = cloneRows(req.params);
  req.headers = cloneRows(req.headers);
  req.body = req.body && typeof req.body === 'object' ? { ...req.body } : {};
  req.body.type = req.body.type || 'none';
  req.body.raw = req.body.raw || '';
  req.body.form = cloneRows(req.body.form);
  req.auth = req.auth && typeof req.auth === 'object' ? { ...req.auth } : {};
  req.auth.type = req.auth.type || 'none';
  req.auth.basic = { username: '', password: '', ...(req.auth.basic || {}) };
  req.auth.bearer = req.auth.bearer || '';
  req.auth.apiKey = { key: '', value: '', addTo: 'header', ...(req.auth.apiKey || {}) };
  req.auth.apiKey.addTo = req.auth.apiKey.addTo || 'header';
  return req;
}

/** 初始化请求构造器。 */
export function initRequest() {
  const methodSelect = $('#method-select');
  methodSelect.innerHTML = METHODS.map((m) => `<option value="${m}">${m}</option>`).join('');
  methodSelect.value = state.request.method;
  methodSelect.addEventListener('change', () => {
    state.request.method = methodSelect.value;
  });

  const nameInput = $('#req-name');
  nameInput.value = state.request.name;
  nameInput.addEventListener('input', () => {
    state.request.name = nameInput.value;
  });

  const urlInput = $('#url-input');
  urlInput.value = state.request.url;
  urlInput.addEventListener('input', () => {
    state.request.url = urlInput.value;
  });
  urlInput.addEventListener('keydown', (e) => {
    if (e.key === 'Enter') {
      e.preventDefault();
      $('#btn-send').click();
    }
  });

  $$('.req-tab').forEach((tab) => {
    tab.addEventListener('click', () => selectTab(tab.dataset.tab));
  });

  editors.params = createKVEditor($('#tab-params'), {
    getRows: () => state.request.params,
    onChange: () => {
      updateCounts();
      emit('request');
    },
    keyPlaceholder: '参数名',
    valuePlaceholder: '参数值',
    descPlaceholder: '说明',
  });

  editors.headers = createKVEditor($('#tab-headers'), {
    getRows: () => state.request.headers,
    onChange: () => {
      updateCounts();
      emit('request');
    },
    keyPlaceholder: '请求头',
    valuePlaceholder: '值',
    descPlaceholder: '说明',
  });

  renderBodyTab();
  renderAuthTab();
  updateCounts();
}

/** 把一个请求载入编辑器。 */
export function loadRequest(req) {
  state.request = normalizeRequest(req);
  $('#method-select').value = state.request.method;
  $('#req-name').value = state.request.name;
  $('#url-input').value = state.request.url;
  editors.params?.render();
  editors.headers?.render();
  renderBodyTab();
  renderAuthTab();
  updateCounts();
  emit('request');
}

/** 切换请求区 Tab。 */
export function selectTab(name) {
  $$('.req-tab').forEach((tab) => tab.classList.toggle('active', tab.dataset.tab === name));
  $$('.req-tab-body').forEach((panel) => panel.classList.toggle('active', panel.id === `tab-${name}`));
}

/** 更新各 Tab 上的启用项计数。 */
export function updateCounts() {
  const countOn = (rows) => (rows || []).filter((r) => r.enabled && String(r.key || '').trim()).length;
  const params = countOn(state.request.params);
  const headers = countOn(state.request.headers);
  const paramsTab = $('.req-tab[data-tab="params"]');
  const headersTab = $('.req-tab[data-tab="headers"]');
  if (paramsTab) paramsTab.textContent = params ? `参数 ${params}` : '参数';
  if (headersTab) headersTab.textContent = headers ? `请求头 ${headers}` : '请求头';
}

/* -------------------------------------------------------------- Body -- */

function renderBodyTab() {
  const tab = $('#tab-body');
  tab.innerHTML = `
    <div class="body-type-row">
      <select id="body-type" class="select"></select>
      <div class="body-toolbar" id="json-toolbar">
        <button type="button" class="btn btn-sm" data-act="format">格式化</button>
        <button type="button" class="btn btn-sm" data-act="minify">压缩成一行</button>
      </div>
    </div>
    <textarea id="body-raw" class="textarea" spellcheck="false" placeholder="在此输入请求体"></textarea>
    <div id="body-form" class="hidden"></div>`;

  const typeSelect = $('#body-type');
  typeSelect.innerHTML = BODY_TYPES.map((t) => `<option value="${t.value}">${t.label}</option>`).join('');
  typeSelect.value = state.request.body.type;
  typeSelect.addEventListener('change', () => {
    state.request.body.type = typeSelect.value;
    syncBodyVisibility();
    emit('request');
  });

  const raw = $('#body-raw');
  raw.value = state.request.body.raw;
  raw.addEventListener('input', () => {
    state.request.body.raw = raw.value;
  });

  $('#json-toolbar').addEventListener('click', (e) => {
    const act = e.target.closest('[data-act]')?.dataset.act;
    if (!act) return;
    const textarea = $('#body-raw');
    try {
      const parsed = JSON.parse(textarea.value);
      textarea.value = act === 'format' ? JSON.stringify(parsed, null, 2) : JSON.stringify(parsed);
      state.request.body.raw = textarea.value;
    } catch (err) {
      emit('toast', { message: `JSON 解析失败：${err.message}`, kind: 'error' });
    }
  });

  createKVEditor($('#body-form'), {
    getRows: () => state.request.body.form,
    onChange: () => emit('request'),
    keyPlaceholder: '字段名',
    valuePlaceholder: '字段值',
    descPlaceholder: '说明',
  });

  syncBodyVisibility();
}

function syncBodyVisibility() {
  const type = state.request.body.type;
  $('#body-raw').classList.toggle('hidden', type === 'none' || type === 'form');
  $('#body-form').classList.toggle('hidden', type !== 'form');
  $('#json-toolbar').classList.toggle('hidden', type !== 'json');
}

/* -------------------------------------------------------------- 认证 -- */

function renderAuthTab() {
  const tab = $('#tab-auth');
  tab.innerHTML = `
    <div class="body-type-row">
      <select id="auth-type" class="select"></select>
    </div>
    <div id="auth-fields"></div>`;

  const typeSelect = $('#auth-type');
  typeSelect.innerHTML = AUTH_TYPES.map((t) => `<option value="${t.value}">${t.label}</option>`).join('');
  typeSelect.value = state.request.auth.type;
  typeSelect.addEventListener('change', () => {
    state.request.auth.type = typeSelect.value;
    renderAuthFields();
    emit('request');
  });

  renderAuthFields();
}

function renderAuthFields() {
  const box = $('#auth-fields');
  const auth = state.request.auth;

  if (auth.type === 'none') {
    box.innerHTML = '<div class="hint">此请求不携带任何认证信息。</div>';
    return;
  }
  if (auth.type === 'basic') {
    box.innerHTML = `
      <div class="auth-grid">
        <label>用户名</label>
        <input id="auth-username" class="input" placeholder="username" />
        <label>密码</label>
        <input id="auth-password" class="input" type="password" placeholder="password" />
      </div>`;
    const u = $('#auth-username');
    const p = $('#auth-password');
    u.value = auth.basic.username;
    p.value = auth.basic.password;
    u.addEventListener('input', () => {
      auth.basic.username = u.value;
    });
    p.addEventListener('input', () => {
      auth.basic.password = p.value;
    });
    return;
  }
  if (auth.type === 'bearer') {
    box.innerHTML = `
      <div class="auth-grid">
        <label>Token</label>
        <input id="auth-bearer" class="input" placeholder="eyJhbGciOi..." />
      </div>
      <div class="hint" style="margin-top: 10px">将以 <code>Authorization: Bearer &lt;token&gt;</code> 发送。</div>`;
    const t = $('#auth-bearer');
    t.value = auth.bearer;
    t.addEventListener('input', () => {
      auth.bearer = t.value;
    });
    return;
  }

  box.innerHTML = `
    <div class="auth-grid">
      <label>键</label>
      <input id="apikey-key" class="input" placeholder="X-Api-Key" />
      <label>值</label>
      <input id="apikey-value" class="input" placeholder="••••••" />
      <label>添加到</label>
      <select id="apikey-addto" class="select">
        <option value="header">请求头 Header</option>
        <option value="query">查询参数 Query</option>
      </select>
    </div>`;
  const k = $('#apikey-key');
  const v = $('#apikey-value');
  const a = $('#apikey-addto');
  k.value = auth.apiKey.key;
  v.value = auth.apiKey.value;
  a.value = auth.apiKey.addTo;
  k.addEventListener('input', () => {
    auth.apiKey.key = k.value;
  });
  v.addEventListener('input', () => {
    auth.apiKey.value = v.value;
  });
  a.addEventListener('change', () => {
    auth.apiKey.addTo = a.value;
  });
}
