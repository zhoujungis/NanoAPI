// request.js —— 请求构造器：方法、地址、参数、请求头、Body 与认证。

import { $, $$ } from './util.js';
import { state, emit } from './store.js';
import { createKVEditor } from './kv.js';
import { bridge } from './bridge.js';

const METHODS = ['GET', 'POST', 'PUT', 'PATCH', 'DELETE', 'HEAD', 'OPTIONS'];

const BODY_TYPES = [
  { value: 'none', label: '无 Body' },
  { value: 'json', label: 'JSON' },
  { value: 'text', label: '文本 (text)' },
  { value: 'form', label: '表单 (x-www-form-urlencoded)' },
  { value: 'multipart', label: '表单 (multipart/form-data)' },
];

const AUTH_TYPES = [
  { value: 'none', label: '无认证' },
  { value: 'basic', label: 'Basic' },
  { value: 'bearer', label: 'Bearer Token' },
  { value: 'apikey', label: 'API Key' },
];

const editors = {};

let jsonValidateTimer = null;

/** 标记当前请求有未保存修改。 */
export function markDirty() {
  if (!state.dirty) {
    state.dirty = true;
    emit('dirty-changed');
  }
  // 即使已经处于脏状态也要通知持久化层，避免后续输入没有触发标签页保存。
  emit('request-touched');
}

/** 标记当前请求已与集合同步。 */
export function markClean() {
  if (!state.dirty) return;
  state.dirty = false;
  emit('dirty-changed');
}

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

/** 深拷贝 multipart 文件行。 */
function cloneFiles(files) {
  if (!Array.isArray(files)) return [];
  return files.map((f) => ({
    key: f?.key ?? '',
    path: f?.path ?? '',
    enabled: f?.enabled !== false,
  }));
}

/** 补齐缺失字段，保证编辑器拿到结构完整的请求。 */
export function normalizeRequest(raw) {
  const req = raw && typeof raw === 'object' ? { ...raw } : {};
  req.id = req.id || '';
  req.name = req.name || '新建请求';

  const method = String(req.method || 'GET').toUpperCase();
  req.method = METHODS.includes(method) ? method : 'GET';
  req.url = req.url || '';
  req.params = cloneRows(req.params);
  req.headers = cloneRows(req.headers);

  req.body = req.body && typeof req.body === 'object' ? { ...req.body } : {};
  req.body.type = BODY_TYPES.some((t) => t.value === req.body.type) ? req.body.type : 'none';
  req.body.raw = req.body.raw || '';
  req.body.form = cloneRows(req.body.form);
  req.body.files = cloneFiles(req.body.files);

  req.auth = req.auth && typeof req.auth === 'object' ? { ...req.auth } : {};
  req.auth.type = AUTH_TYPES.some((t) => t.value === req.auth.type) ? req.auth.type : 'none';
  req.auth.basic = { username: '', password: '', ...(req.auth.basic || {}) };
  req.auth.bearer = req.auth.bearer || '';
  req.auth.apiKey = { key: '', value: '', addTo: 'header', ...(req.auth.apiKey || {}) };
  req.auth.apiKey.addTo = req.auth.apiKey.addTo === 'query' ? 'query' : 'header';
  return req;
}

/** 初始化请求构造器。 */
export function initRequest() {
  const methodSelect = $('#method-select');
  methodSelect.innerHTML = METHODS.map((m) => `<option value="${m}">${m}</option>`).join('');
  methodSelect.value = state.request.method;
  methodSelect.addEventListener('change', () => {
    state.request.method = methodSelect.value;
    markDirty();
    emit('tab-meta-changed');
  });

  const nameInput = $('#req-name');
  nameInput.value = state.request.name;
  nameInput.addEventListener('input', () => {
    state.request.name = nameInput.value;
    markDirty();
    emit('tab-meta-changed');
  });

  const urlInput = $('#url-input');
  urlInput.value = state.request.url;
  urlInput.addEventListener('input', () => {
    state.request.url = urlInput.value;
    markDirty();
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
      markDirty();
      emit('request');
    },
    keyPlaceholder: '参数名',
    valuePlaceholder: '参数值',
    descPlaceholder: '说明',
  });
  addImportParamsButton();

  editors.headers = createKVEditor($('#tab-headers'), {
    getRows: () => state.request.headers,
    onChange: () => {
      updateCounts();
      markDirty();
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

/** 把当前活动标签页的请求同步到编辑器。 */
export function syncRequestEditor() {
  const req = state.request;
  if (!req) return;

  $('#method-select').value = req.method;
  $('#req-name').value = req.name;
  $('#url-input').value = req.url;
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

/** 在参数表底部添加「从地址栏导入」按钮。 */
function addImportParamsButton() {
  const footer = $('#tab-params .kv-footer');
  if (!footer) return;
  const btn = document.createElement('button');
  btn.type = 'button';
  btn.className = 'btn btn-sm';
  btn.textContent = '从地址栏导入';
  btn.title = '解析地址栏里的查询参数，并移动到参数表';
  btn.addEventListener('click', importParamsFromUrl);
  footer.prepend(btn);
}

/** 把地址栏中的查询参数导入参数表，并从地址栏移除。 */
function importParamsFromUrl() {
  const url = String(state.request.url || '');
  const qIndex = url.indexOf('?');
  if (qIndex < 0) {
    emit('toast', { message: '地址栏中没有查询参数', kind: 'info' });
    return;
  }
  const hashIndex = url.indexOf('#', qIndex);
  const query = url.slice(qIndex + 1, hashIndex >= 0 ? hashIndex : undefined);
  if (!query.trim()) {
    emit('toast', { message: '地址栏中没有查询参数', kind: 'info' });
    return;
  }

  // ?flag 这种无等号参数无法放进键值表，保留在地址栏里。
  const valued = [];
  const valueless = [];
  for (const seg of query.split('&')) {
    if (!seg) continue;
    if (seg.includes('=')) valued.push(seg);
    else valueless.push(seg);
  }

  const parsed = [];
  for (const [key, value] of new URLSearchParams(valued.join('&'))) {
    if (!key) continue;
    parsed.push({ key, value, enabled: true, desc: '' });
  }
  if (!parsed.length) {
    emit('toast', {
      message: valueless.length ? '没有可导入的键值参数，无值参数仍保留在地址栏' : '地址栏中没有可导入的查询参数',
      kind: 'info',
    });
    return;
  }

  const rows = state.request.params;
  const seen = new Set();
  let added = 0;
  let updated = 0;
  for (const p of parsed) {
    if (!seen.has(p.key)) {
      seen.add(p.key);
      const existing = rows.find((r) => r.key === p.key);
      if (existing) {
        existing.value = p.value;
        existing.enabled = true;
        updated += 1;
        continue;
      }
    }
    rows.push(p);
    added += 1;
  }

  const remaining = valueless.join('&');
  state.request.url =
    url.slice(0, qIndex) +
    (remaining ? `?${remaining}` : '') +
    (hashIndex >= 0 ? url.slice(hashIndex) : '');
  $('#url-input').value = state.request.url;
  editors.params?.render();
  updateCounts();
  markDirty();
  emit('request');

  const parts = [];
  if (added) parts.push(`新增 ${added} 个`);
  if (updated) parts.push(`更新 ${updated} 个`);
  if (valueless.length) parts.push(`保留 ${valueless.length} 个无值参数`);
  emit('toast', { message: `已从地址栏导入：${parts.join('，')}`, kind: 'success' });
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
  clearTimeout(jsonValidateTimer);
  jsonValidateTimer = null;

  const tab = $('#tab-body');
  tab.innerHTML = `
    <div class="body-type-row">
      <select id="body-type" class="select"></select>
      <div class="body-toolbar" id="json-toolbar">
        <button type="button" class="btn btn-sm" data-act="format">格式化</button>
        <button type="button" class="btn btn-sm" data-act="minify">压缩成一行</button>
        <span id="json-hint" class="hint json-hint"></span>
      </div>
    </div>
    <textarea id="body-raw" class="textarea" spellcheck="false" placeholder="在此输入请求体"></textarea>
    <div id="body-form" class="hidden"></div>
    <div id="body-multipart" class="hidden">
      <div class="section-label">文件字段</div>
      <div id="body-file-list"></div>
      <div class="kv-footer">
        <button type="button" class="btn btn-sm" data-act="add-file">＋ 添加文件</button>
      </div>
      <div class="hint">文件在发送时从本机读取，路径支持 {{变量}}。单文件上限 20 MB，总计 50 MB；Content-Type 与 boundary 会自动生成。</div>
    </div>`;

  const typeSelect = $('#body-type');
  typeSelect.innerHTML = BODY_TYPES.map((t) => `<option value="${t.value}">${t.label}</option>`).join('');
  typeSelect.value = state.request.body.type;
  typeSelect.addEventListener('change', () => {
    state.request.body.type = typeSelect.value;
    markDirty();
    syncBodyVisibility();
    validateJSON();
    emit('request');
  });

  const raw = $('#body-raw');
  const hint = $('#json-hint');
  raw.value = state.request.body.raw;

  const validateJSON = () => {
    if (state.request.body.type !== 'json') {
      hint.textContent = '';
      hint.className = 'hint json-hint';
      raw.classList.remove('invalid');
      return;
    }
    if (!raw.value.trim()) {
      hint.textContent = '';
      hint.className = 'hint json-hint';
      raw.classList.remove('invalid');
      return;
    }
    if (raw.value.length > 512 * 1024) {
      hint.textContent = '内容较大，已跳过实时校验';
      hint.className = 'hint json-hint json-muted';
      raw.classList.remove('invalid');
      return;
    }
    try {
      JSON.parse(raw.value);
      hint.textContent = 'JSON 有效';
      hint.className = 'hint json-hint json-ok';
      raw.classList.remove('invalid');
    } catch (err) {
      hint.textContent = `JSON 无效：${err.message}`;
      hint.className = 'hint json-hint json-bad';
      raw.classList.add('invalid');
    }
  };

  raw.addEventListener('input', () => {
    state.request.body.raw = raw.value;
    markDirty();
    if (state.request.body.type === 'json') {
      clearTimeout(jsonValidateTimer);
      jsonValidateTimer = setTimeout(validateJSON, 200);
    }
  });

  $('#json-toolbar').addEventListener('click', (e) => {
    const act = e.target.closest('[data-act]')?.dataset.act;
    if (!act) return;
    const textarea = $('#body-raw');
    try {
      const parsed = JSON.parse(textarea.value);
      textarea.value = act === 'format' ? JSON.stringify(parsed, null, 2) : JSON.stringify(parsed);
      state.request.body.raw = textarea.value;
      markDirty();
      validateJSON();
    } catch (err) {
      emit('toast', { message: `JSON 解析失败：${err.message}`, kind: 'error' });
    }
  });

  createKVEditor($('#body-form'), {
    getRows: () => state.request.body.form,
    onChange: () => {
      markDirty();
      emit('request');
    },
    keyPlaceholder: '字段名',
    valuePlaceholder: '字段值',
    descPlaceholder: '说明',
  });

  const fileList = $('#body-file-list');
  const renderFiles = () => {
    fileList.innerHTML = '';
    const files = state.request.body.files || [];
    if (!files.length) {
      fileList.innerHTML = '<div class="hint">还没有文件，点击下方添加。</div>';
      return;
    }

    files.forEach((file, index) => {
      const row = document.createElement('div');
      row.className = 'file-row';

      const check = document.createElement('input');
      check.type = 'checkbox';
      check.className = 'kv-check';
      check.checked = file.enabled !== false;
      check.title = '是否启用';
      check.addEventListener('change', () => {
        file.enabled = check.checked;
        markDirty();
      });

      const key = document.createElement('input');
      key.className = 'input file-key';
      key.placeholder = '字段名，如 file';
      key.value = file.key || '';
      key.addEventListener('input', () => {
        file.key = key.value;
        markDirty();
      });

      const filePath = document.createElement('input');
      filePath.className = 'input file-path';
      filePath.placeholder = '文件路径，可使用 {{变量}}';
      filePath.spellcheck = false;
      filePath.value = file.path || '';
      filePath.addEventListener('input', () => {
        file.path = filePath.value;
        markDirty();
      });

      const pick = document.createElement('button');
      pick.type = 'button';
      pick.className = 'btn btn-sm';
      pick.textContent = '选择文件';
      pick.addEventListener('click', async () => {
        try {
          const selected = await bridge.pickFile();
          if (!selected) return;
          file.path = selected;
          filePath.value = selected;
          if (!file.key.trim()) {
            file.key = 'file';
            key.value = 'file';
          }
          markDirty();
        } catch (err) {
          emit('toast', { message: `选择文件失败：${err}`, kind: 'error' });
        }
      });

      const del = document.createElement('button');
      del.type = 'button';
      del.className = 'icon-btn';
      del.title = '删除文件';
      del.textContent = '✕';
      del.addEventListener('click', () => {
        state.request.body.files.splice(index, 1);
        renderFiles();
        markDirty();
      });

      row.append(check, key, filePath, pick, del);
      fileList.appendChild(row);
    });
  };

  $('#body-multipart [data-act="add-file"]').addEventListener('click', () => {
    state.request.body.files.push({ key: '', path: '', enabled: true });
    markDirty();
    renderFiles();
    const keys = fileList.querySelectorAll('.file-key');
    keys[keys.length - 1]?.focus();
  });
  renderFiles();

  syncBodyVisibility();
  validateJSON();
}

function syncBodyVisibility() {
  const type = state.request.body.type;
  const usesForm = type === 'form' || type === 'multipart';
  $('#body-raw').classList.toggle('hidden', type === 'none' || type === 'form' || type === 'multipart');
  $('#body-form').classList.toggle('hidden', !usesForm);
  $('#body-multipart').classList.toggle('hidden', type !== 'multipart');
  $('#json-toolbar').classList.toggle('hidden', type !== 'json');
  if (type !== 'json') {
    clearTimeout(jsonValidateTimer);
    jsonValidateTimer = null;
    const hint = $('#json-hint');
    if (hint) {
      hint.textContent = '';
      hint.className = 'hint json-hint';
    }
    $('#body-raw').classList.remove('invalid');
  }
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
    markDirty();
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
      markDirty();
    });
    p.addEventListener('input', () => {
      auth.basic.password = p.value;
      markDirty();
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
      markDirty();
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
    markDirty();
  });
  v.addEventListener('input', () => {
    auth.apiKey.value = v.value;
    markDirty();
  });
  a.addEventListener('change', () => {
    auth.apiKey.addTo = a.value;
    markDirty();
  });
}
