// sidebar.js —— 侧边栏：集合树、环境管理与历史记录。

import { $, $$, escapeHtml, formatDuration, formatTime, promptDialog, confirmDialog } from './util.js';
import { state, emit, findItem, removeItem, flattenTargets, countRequests } from './store.js';
import { createKVEditor } from './kv.js';
import { loadRequest } from './request.js';

/** 已展开的集合树节点 ID。 */
const expanded = new Set();
/** 正在编辑变量的环境 ID。 */
let editingEnvId = '';
/** 正在重命名的环境 ID。 */
let renamingEnvId = '';
/** 正在重命名的节点 ID。 */
let renamingId = '';
/**
 * 渲染结束后需要聚焦的输入框。
 * 节点在插入文档之前调用 focus() 是无效的，所以先记下来，等整棵树挂载完再聚焦。
 */
let pendingFocus = null;

/** 初始化侧边栏。 */
export function initSidebar() {
  $$('.side-tab').forEach((tab) => {
    tab.addEventListener('click', () => {
      $$('.side-tab').forEach((t) => t.classList.toggle('active', t === tab));
      $$('.sidebar-panel').forEach((p) => p.classList.toggle('active', p.id === `panel-${tab.dataset.panel}`));
    });
  });

  $('#collection-search').addEventListener('input', renderTree);
  $('#history-search').addEventListener('input', renderHistory);

  $('#btn-new-collection').addEventListener('click', () => addCollection());
  $('#btn-new-env').addEventListener('click', () => addEnvironment(false));
  $('#btn-new-global-env').addEventListener('click', () => addEnvironment(true));
  $('#btn-clear-history').addEventListener('click', clearHistory);

  renderTree();
  renderEnvs();
  renderHistory();
  renderEnvSelect();
}

/* ============================================================ 集合树 == */

/** 新建一个顶层集合。 */
export function addCollection() {
  const item = { id: newId(), type: 'folder', name: '新建集合', items: [] };
  state.collections.push(item);
  expanded.add(item.id);
  renamingId = item.id;
  emit('collections');
  renderTree();
}

/** 在指定父节点下新建请求。 */
export function addRequest(parentId = '') {
  const req = {
    id: newId(),
    name: '新建请求',
    method: 'GET',
    url: '',
    params: [],
    headers: [],
    body: { type: 'none', raw: '', form: [] },
    auth: {
      type: 'none',
      basic: { username: '', password: '' },
      bearer: '',
      apiKey: { key: '', value: '', addTo: 'header' },
    },
  };
  const node = { id: newId(), type: 'request', name: req.name, request: req };
  const parent = parentId ? findItem(state.collections, parentId).item : null;
  if (parent) {
    parent.items = parent.items || [];
    parent.items.push(node);
    expanded.add(parent.id);
  } else {
    state.collections.push(node);
  }
  renamingId = node.id;
  emit('collections');
  renderTree();
}

/** 新建一个文件夹。 */
function addFolder(parentId) {
  const node = { id: newId(), type: 'folder', name: '新建文件夹', items: [] };
  const parent = parentId ? findItem(state.collections, parentId).item : null;
  if (parent) {
    parent.items = parent.items || [];
    parent.items.push(node);
    expanded.add(parent.id);
  } else {
    state.collections.push(node);
  }
  renamingId = node.id;
  emit('collections');
  renderTree();
}

/** 重命名节点。 */
function renameItem(id, name) {
  const { item } = findItem(state.collections, id);
  if (!item || !name) return;
  item.name = name;
  if (item.type === 'request' && item.request) item.request.name = name;
  emit('collections');
  renderTree();
}

/** 删除节点。 */
async function deleteItem(id) {
  const { item } = findItem(state.collections, id);
  if (!item) return;
  const ok = await confirmDialog(`确定删除「${item.name}」吗？此操作不可撤销。`, '删除确认');
  if (!ok) return;
  removeItem(state.collections, id);
  emit('collections');
  renderTree();
}

/** 复制一个请求节点。 */
function duplicateItem(id) {
  const { parent, item } = findItem(state.collections, id);
  if (!item) return;
  const copy = JSON.parse(JSON.stringify(item));
  reId(copy);
  copy.name = `${item.name} 副本`;
  if (copy.type === 'request') copy.request.name = copy.name;
  const siblings = parent ? parent.items : state.collections;
  siblings.splice(siblings.indexOf(item) + 1, 0, copy);
  emit('collections');
  renderTree();
}

/** 递归为复制出来的节点换上新的 ID。 */
function reId(node) {
  node.id = newId();
  if (node.request) node.request.id = newId();
  (node.items || []).forEach(reId);
}

/** 渲染集合树。 */
function renderTree() {
  const box = $('#collection-tree');
  const query = $('#collection-search').value.trim().toLowerCase();
  const tree = query ? filterTree(state.collections, query) : state.collections;

  if (!state.collections.length) {
    box.innerHTML = `
      <div class="side-empty">
        <b>还没有集合</b>
        点击上方 ＋ 新建一个集合，<br />把常用请求收进去。
      </div>`;
    return;
  }
  if (!tree.length) {
    box.innerHTML = '<div class="side-empty">没有匹配的集合或请求。</div>';
    return;
  }

  box.innerHTML = '';
  tree.forEach((node) => box.appendChild(renderNode(node, null, true)));
  focusPending();
}

/** 按名称过滤集合树，保留命中项及其祖先。 */
function filterTree(items, query) {
  const out = [];
  for (const item of items || []) {
    const selfHit = String(item.name || '').toLowerCase().includes(query);
    const children = filterTree(item.items, query);
    if (selfHit || children.length) {
      out.push({ ...item, items: selfHit ? item.items || [] : children });
    }
  }
  return out;
}

/** 渲染一个树节点。 */
function renderNode(node, parentId, isRoot) {
  const wrap = document.createElement('div');
  wrap.className = 'tree-node';

  const isRequest = node.type === 'request';
  const hasChildren = !isRequest && (node.items || []).length > 0;
  const isOpen = expanded.has(node.id);

  const row = document.createElement('div');
  row.className = 'tree-row';
  if (state.request.id && node.request?.id === state.request.id) row.classList.add('selected');

  const caret = document.createElement('span');
  caret.className = `tree-caret${hasChildren ? (isOpen ? ' open' : '') : ' leaf'}`;
  caret.textContent = '▶';
  if (hasChildren) {
    caret.addEventListener('click', (e) => {
      e.stopPropagation();
      if (expanded.has(node.id)) expanded.delete(node.id);
      else expanded.add(node.id);
      renderTree();
    });
  }

  const icon = document.createElement('span');
  icon.className = 'tree-icon';
  icon.textContent = isRequest ? methodIcon(node.request?.method) : isRoot ? '🗂' : '📁';

  const label = document.createElement('span');
  label.className = 'tree-label';
  label.textContent = node.name;

  row.append(caret, icon, label);

  if (isRequest && node.request) {
    const badge = document.createElement('span');
    badge.className = `method method-${(node.request.method || 'GET').toUpperCase()}`;
    badge.textContent = (node.request.method || 'GET').toUpperCase();
    row.appendChild(badge);
  }

  const actions = document.createElement('span');
  actions.className = 'tree-actions';
  actions.append(
    iconButton('＋', '新建请求', (e) => {
      e.stopPropagation();
      addRequest(node.id);
    }),
    iconButton('✎', '重命名', (e) => {
      e.stopPropagation();
      renamingId = node.id;
      renderTree();
    }),
    isRequest
      ? iconButton('⧉', '复制', (e) => {
          e.stopPropagation();
          duplicateItem(node.id);
        })
      : iconButton('🗀', '新建文件夹', (e) => {
          e.stopPropagation();
          addFolder(node.id);
        }),
    iconButton('✕', '删除', (e) => {
      e.stopPropagation();
      deleteItem(node.id);
    })
  );
  row.appendChild(actions);

  if (renamingId === node.id) {
    const input = document.createElement('input');
    input.value = node.name;
    row.replaceChild(input, label);
    pendingFocus = input;
    const commit = () => {
      renamingId = '';
      renameItem(node.id, input.value.trim());
    };
    input.addEventListener('blur', commit);
    input.addEventListener('keydown', (e) => {
      if (e.key === 'Enter') {
        e.preventDefault();
        input.blur();
      }
      if (e.key === 'Escape') {
        renamingId = '';
        renderTree();
      }
    });
  } else {
    row.addEventListener('click', () => {
      if (isRequest && node.request) {
        loadRequest(node.request);
        emit('request-selected');
      } else {
        if (expanded.has(node.id)) expanded.delete(node.id);
        else expanded.add(node.id);
        renderTree();
      }
    });
  }

  wrap.appendChild(row);

  if (!isRequest && (node.items || []).length && expanded.has(node.id)) {
    const children = document.createElement('div');
    children.className = 'tree-children';
    node.items.forEach((child) => children.appendChild(renderNode(child, node.id, false)));
    wrap.appendChild(children);
  }
  return wrap;
}

/** 小图标按钮。 */
function iconButton(glyph, title, handler) {
  const btn = document.createElement('button');
  btn.type = 'button';
  btn.className = 'icon-btn tree-action';
  btn.title = title;
  btn.textContent = glyph;
  btn.addEventListener('click', handler);
  return btn;
}

/** 方法对应的图标。 */
function methodIcon(method) {
  switch ((method || 'GET').toUpperCase()) {
    case 'POST':
      return '↑';
    case 'PUT':
      return '⇧';
    case 'PATCH':
      return '✎';
    case 'DELETE':
      return '✕';
    default:
      return '→';
  }
}

/* ============================================================== 环境 == */

/** 新建环境。 */
function addEnvironment(isGlobal) {
  const name = isGlobal ? '全局变量' : '新环境';
  const env = { id: newId(), name, values: [], isGlobal: !!isGlobal };
  state.environments.push(env);
  editingEnvId = env.id;
  renamingEnvId = env.id;
  emit('environments');
  renderEnvs();
  renderEnvSelect();
}

/** 渲染环境列表。 */
function renderEnvs() {
  const box = $('#env-list');
  if (!state.environments.length) {
    box.innerHTML = `
      <div class="side-empty">
        <b>还没有环境</b>
        新建环境后即可使用 <code>{{变量}}</code>，<br />同一份请求可切换不同环境。
      </div>`;
    return;
  }

  box.innerHTML = '';
  state.environments.forEach((env) => {
    env.values = Array.isArray(env.values) ? env.values : [];
    const card = document.createElement('div');
    card.className = `env-card${env.id === state.activeEnvId ? ' active' : ''}${env.id === editingEnvId ? ' open' : ''}`;

    const head = document.createElement('div');
    head.className = 'env-head';

    const name = document.createElement('span');
    name.className = 'env-name';
    name.textContent = env.name;
    name.title = '双击重命名';

    const count = document.createElement('span');
    count.className = 'tree-count';
    count.textContent = `${(env.values || []).filter((v) => v.enabled && v.key).length} 个变量`;

    const badge = document.createElement('span');
    badge.className = 'badge';
    badge.textContent = env.isGlobal ? '全局' : env.id === state.activeEnvId ? '使用中' : '点击启用';

    const actions = document.createElement('span');
    actions.append(
      iconButton('✎', '编辑变量', (e) => {
        e.stopPropagation();
        editingEnvId = editingEnvId === env.id ? '' : env.id;
        renderEnvs();
      }),
      iconButton('✕', '删除环境', async (e) => {
        e.stopPropagation();
        const ok = await confirmDialog(`确定删除环境「${env.name}」吗？`, '删除确认');
        if (!ok) return;
        state.environments = state.environments.filter((x) => x.id !== env.id);
        if (state.activeEnvId === env.id) state.activeEnvId = '';
        editingEnvId = '';
        emit('environments');
        renderEnvs();
        renderEnvSelect();
      })
    );

    head.append(name, count, badge, actions);
    head.addEventListener('click', (e) => {
      if (e.target.closest('button')) return;
      if (env.isGlobal) {
        editingEnvId = editingEnvId === env.id ? '' : env.id;
      } else {
        state.activeEnvId = env.id;
        emit('env-changed');
      }
      renderEnvs();
      renderEnvSelect();
    });

    // 双击环境名进入重命名。
    name.addEventListener('dblclick', (e) => {
      e.stopPropagation();
      renamingEnvId = env.id;
      renderEnvs();
    });

    if (renamingEnvId === env.id) {
      const input = document.createElement('input');
      input.className = 'env-name-input';
      input.value = env.name;
      head.replaceChild(input, name);
      pendingFocus = input;
      const commit = () => {
        renamingEnvId = '';
        const next = input.value.trim();
        if (next) env.name = next;
        emit('environments');
        renderEnvs();
        renderEnvSelect();
      };
      input.addEventListener('blur', commit);
      input.addEventListener('keydown', (e) => {
        if (e.key === 'Enter') {
          e.preventDefault();
          input.blur();
        }
        if (e.key === 'Escape') {
          renamingEnvId = '';
          renderEnvs();
        }
      });
    }

    card.appendChild(head);

    if (env.id === editingEnvId) {
      const body = document.createElement('div');
      body.className = 'env-body';
      const editor = createKVEditor(body, {
        getRows: () => env.values,
        onChange: () => {
          emit('environments');
          renderEnvSelect();
        },
        keyPlaceholder: '变量名',
        valuePlaceholder: '变量值',
        descPlaceholder: '说明',
      });
      card.appendChild(body);
      // 首次渲染后同步一次计数
      setTimeout(() => editor.render(), 0);
    }

    box.appendChild(card);
  });

  focusPending();
}

/** 聚焦挂载完成后待处理的重命名输入框。 */
function focusPending() {
  if (!pendingFocus) return;
  const input = pendingFocus;
  pendingFocus = null;
  input.focus();
  input.select();
}

/** 渲染顶栏的环境下拉框。 */
function renderEnvSelect() {
  const select = $('#env-select');
  const current = state.activeEnvId;
  const options = ['<option value="">不指定环境</option>'];
  for (const env of state.environments) {
    if (env.isGlobal) continue;
    options.push(`<option value="${env.id}">${escapeHtml(env.name)}</option>`);
  }
  select.innerHTML = options.join('');
  select.value = current;

  const hint = $('#env-hint');
  const global = state.environments.find((e) => e.isGlobal);
  const globalCount = global ? (global.values || []).filter((v) => v.enabled && v.key).length : 0;
  hint.textContent = globalCount ? `含 ${globalCount} 个全局变量` : '';
}

/* ============================================================== 历史 == */

/** 渲染历史记录。 */
function renderHistory() {
  const box = $('#history-list');
  const query = $('#history-search').value.trim().toLowerCase();
  const list = (state.history || []).filter((h) => {
    if (!query) return true;
    return (
      String(h.request?.url || '').toLowerCase().includes(query) ||
      String(h.request?.name || '').toLowerCase().includes(query)
    );
  });

  if (!state.history?.length) {
    box.innerHTML = '<div class="side-empty"><b>还没有历史</b>发送过的请求会记录在这里。</div>';
    return;
  }
  if (!list.length) {
    box.innerHTML = '<div class="side-empty">没有匹配的历史记录。</div>';
    return;
  }

  box.innerHTML = '';
  list.forEach((entry) => {
    const item = document.createElement('div');
    item.className = 'history-item';

    const method = document.createElement('span');
    method.className = `method method-${(entry.request?.method || 'GET').toUpperCase()}`;
    method.textContent = (entry.request?.method || 'GET').toUpperCase();

    const main = document.createElement('div');
    main.className = 'history-main';
    const url = document.createElement('div');
    url.className = 'history-url';
    url.textContent = entry.request?.url || '(空地址)';
    const meta = document.createElement('div');
    meta.className = 'history-meta';
    meta.textContent = `${formatTime(entry.timestamp)} · ${formatDuration(entry.durationMs)}`;
    main.append(url, meta);

    const status = document.createElement('span');
    if (entry.error) {
      status.className = 'status-pill status-err';
      status.textContent = '失败';
    } else {
      status.className = `status-pill status-${String(entry.status || 0)[0]}xx`;
      status.textContent = entry.status;
    }

    item.append(method, main, status);
    item.addEventListener('click', () => {
      loadRequest(entry.request);
      emit('request-selected');
    });
    box.appendChild(item);
  });
}

/** 清空历史。 */
async function clearHistory() {
  if (!state.history?.length) return;
  const ok = await confirmDialog('确定清空全部历史记录吗？', '清空历史');
  if (!ok) return;
  state.history = [];
  emit('history');
  renderHistory();
}

/** 生成 ID。 */
function newId() {
  return Math.random().toString(16).slice(2, 10) + Date.now().toString(16).slice(-6);
}

export { renderTree, renderEnvs, renderHistory, renderEnvSelect };
