// sidebar.js —— 侧边栏：集合树、环境管理与历史记录。

import { $, $$, escapeHtml, escapeAttr, formatDuration, formatTime, confirmDialog, uid } from './util.js';
import { state, emit, findItem, removeItem, countRequests, moveItem } from './store.js';
import { createKVEditor } from './kv.js';
import { formatEnvValues, parseEnvText } from './envbulk.js';
import { closeContextMenu, showContextMenu } from './menu.js';
import { normalizeRequest } from './request.js';
import { bridge } from './bridge.js';

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
/** 当前正在拖动的集合树节点 ID。 */
let draggedNodeId = '';
/** 拖动结束后的短时间窗口内忽略点击，避免误触发展开/加载。 */
let suppressClickUntil = 0;

/** 统计环境里已启用的变量数量。 */
function countEnabled(env) {
  return (env?.values || []).filter((v) => v.enabled && String(v.key || '').trim()).length;
}

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

  // 拖到集合面板空白处 = 移到顶层。
  const panel = $('#panel-collections');
  const treeBox = $('#collection-tree');
  panel.addEventListener('dragover', (e) => {
    if (!draggedNodeId || e.target.closest('.tree-row')) return;
    e.preventDefault();
    if (e.dataTransfer) e.dataTransfer.dropEffect = 'move';
    treeBox.classList.add('drop-root');
  });
  panel.addEventListener('dragleave', (e) => {
    if (!panel.contains(e.relatedTarget)) {
      treeBox.classList.remove('drop-root');
    }
  });
  panel.addEventListener('drop', (e) => {
    if (!draggedNodeId || e.target.closest('.tree-row')) return;
    e.preventDefault();
    const id = draggedNodeId;
    suppressClickUntil = Date.now() + 250;
    clearDragState();
    if (moveItem(state.collections, id, '', 'root')) {
      emit('collections');
      revealActiveRequest();
      renderTree();
    }
  });

  // 集合面板空白处右键：新建集合 / 顶层请求。
  panel.addEventListener('contextmenu', (e) => {
    if (e.target.closest('.tree-row, input, textarea, select, .side-toolbar')) return;
    e.preventDefault();
    showContextMenu(e.clientX, e.clientY, [
      { label: '新建集合', action: addCollection },
      { label: '新建请求', action: () => addRequest('') },
    ]);
  });

  $('#btn-new-collection').addEventListener('click', () => addCollection());
  $('#btn-new-env').addEventListener('click', () => addEnvironment(false));
  $('#btn-new-global-env').addEventListener('click', () => addEnvironment(true));
  $('#btn-clear-history').addEventListener('click', clearHistory);

  revealActiveRequest();
  renderTree();
  renderEnvs();
  renderHistory();
  renderEnvSelect();
}

/** 展开集合树，让当前活动请求可见。 */
export function revealActiveRequest() {
  const id = state.request?.id;
  if (!id) return false;

  let changed = false;
  const walk = (items, ancestors) => {
    for (const item of items || []) {
      if (item.request?.id === id) {
        for (const ancestor of ancestors) {
          if (!expanded.has(ancestor)) {
            expanded.add(ancestor);
            changed = true;
          }
        }
        return true;
      }
      if (item.items?.length && walk(item.items, [...ancestors, item.id])) {
        return true;
      }
    }
    return false;
  };
  walk(state.collections, []);
  return changed;
}

function treeContainsNode(node, id) {
  if (!node) return false;
  if (node.id === id) return true;
  return (node.items || []).some((child) => treeContainsNode(child, id));
}

function clearDropMarkers() {
  $$('.tree-row.drop-before, .tree-row.drop-after, .tree-row.drop-inside').forEach((row) => {
    row.classList.remove('drop-before', 'drop-after', 'drop-inside');
    delete row.dataset.dropPosition;
  });
  const tree = $('#collection-tree');
  if (tree) tree.classList.remove('drop-root');
}

function clearDragState() {
  draggedNodeId = '';
  $$('.tree-row.dragging').forEach((row) => row.classList.remove('dragging'));
  clearDropMarkers();
}

/** 节点右键菜单的条目。 */
function nodeContextMenuItems(node, isRequest) {
  const items = [];

  if (isRequest) {
    items.push({
      label: '打开',
      action: () => {
        if (!node.request) return;
        emit('open-request', { request: node.request });
        $('#url-input').focus();
      },
    });
  } else {
    if ((node.items || []).length) {
      const open = expanded.has(node.id);
      items.push({
        label: open ? '折叠' : '展开',
        action: () => {
          if (open) expanded.delete(node.id);
          else expanded.add(node.id);
          renderTree();
        },
      });
    }
    items.push({ label: '新建请求', action: () => addRequest(node.id) });
    items.push({ label: '新建文件夹', action: () => addFolder(node.id) });
  }

  items.push({ separator: true });
  items.push({
    label: '重命名',
    action: () => {
      renamingId = node.id;
      renderTree();
    },
  });
  items.push({ label: '复制', action: () => duplicateItem(node.id) });
  items.push({ separator: true });
  items.push({ label: '删除', danger: true, action: () => deleteItem(node.id) });
  return items;
}

/** 在节点上打开右键菜单。 */
function showNodeContextMenu(x, y, node, isRequest) {
  const title = node.name || (isRequest ? '未命名请求' : '未命名文件夹');
  showContextMenu(x, y, nodeContextMenuItems(node, isRequest), title);
}

/** 清空集合搜索，确保新建的节点可见。 */
function clearCollectionSearch() {
  const search = $('#collection-search');
  if (search && search.value) search.value = '';
}

/* ============================================================ 集合树 == */

/** 新建一个顶层集合。 */
export function addCollection() {
  clearCollectionSearch();
  const item = { id: uid(), type: 'folder', name: '新建集合', items: [] };
  state.collections.push(item);
  expanded.add(item.id);
  renamingId = item.id;
  emit('collections');
  renderTree();
}

/** 在指定父节点下新建请求，并立即打开它。 */
export function addRequest(parentId = '') {
  clearCollectionSearch();
  const req = normalizeRequest({});
  req.id = uid();
  const node = { id: uid(), type: 'request', name: req.name, request: req };

  const parent = parentId ? findItem(state.collections, parentId).item : null;
  if (parent && parent.type !== 'request') {
    parent.items = parent.items || [];
    parent.items.push(node);
    expanded.add(parent.id);
  } else {
    state.collections.push(node);
  }
  renamingId = node.id;
  emit('collections');

  // open-request 的订阅者会打开标签页，request-selected 再重绘集合树并聚焦重命名输入框。
  emit('open-request', { request: req });
}

/** 新建一个文件夹。 */
function addFolder(parentId) {
  clearCollectionSearch();
  const node = { id: uid(), type: 'folder', name: '新建文件夹', items: [] };
  const parent = parentId ? findItem(state.collections, parentId).item : null;
  if (parent && parent.type !== 'request') {
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

/** 重命名节点。名称为空时保持原名，但仍退出编辑状态。 */
function renameItem(id, name) {
  const { item } = findItem(state.collections, id);
  if (!item) return;

  const next = String(name || '').trim();
  if (next) {
    item.name = next;
    if (item.type === 'request' && item.request) {
      item.request.name = next;

      // 同步所有打开了这个请求的标签页。
      let touchedTab = false;
      for (const tab of state.tabs) {
        if (tab.request?.id === item.request.id) {
          tab.request.name = next;
          touchedTab = true;
        }
      }
      if (state.request?.id === item.request.id) {
        const input = $('#req-name');
        if (input) input.value = next;
        emit('request-selected');
      }
      if (touchedTab) emit('tab-meta-changed');
    }
    emit('collections');
  }
  renderTree();
}

/** 判断节点自身或后代中是否包含指定请求。 */
function containsRequest(node, requestId) {
  if (!requestId || !node) return false;
  if (node.request?.id === requestId) return true;
  return (node.items || []).some((child) => containsRequest(child, requestId));
}

/** 删除节点。若删掉了当前打开的请求，则回到空白草稿。 */
async function deleteItem(id) {
  const { item } = findItem(state.collections, id);
  if (!item) return;

  const count = item.type === 'folder' ? countRequests(item.items || []) : 0;
  const extra = count ? `，其中包含 ${count} 个请求` : '';
  const removedCurrent = containsRequest(item, state.request?.id);
  const dirtyNote = removedCurrent && state.dirty ? '当前请求还有未保存修改，' : '';
  const ok = await confirmDialog(
    `确定删除「${item.name}」吗${extra}？${dirtyNote}此操作不可撤销。`,
    '删除确认'
  );
  if (!ok) return;

  // 引用被删请求的标签页保留内容，但变为未保存草稿。
  const affectedTabs = state.tabs.filter(
    (tab) => tab.request?.id && containsRequest(item, tab.request.id)
  );

  removeItem(state.collections, id);
  emit('collections');

  if (affectedTabs.length) {
    for (const tab of affectedTabs) tab.dirty = true;
    emit('dirty-changed');
  }
  emit('request-selected');
  renderTree();
}

/** 复制一个请求或文件夹节点。 */
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
  node.id = uid();
  if (node.request) node.request.id = uid();
  (node.items || []).forEach(reId);
}

/** 渲染集合树。 */
function renderTree() {
  closeContextMenu();
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
  tree.forEach((node) => box.appendChild(renderNode(node, true)));
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
function renderNode(node, isRoot) {
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

  const renameAction = iconButton('✎', '重命名', (e) => {
    e.stopPropagation();
    renamingId = node.id;
    renderTree();
  });

  const actions = document.createElement('span');
  actions.className = 'tree-actions';
  if (isRequest) {
    actions.append(
      renameAction,
      iconButton('⧉', '复制', (e) => {
        e.stopPropagation();
        duplicateItem(node.id);
      }),
      iconButton('✕', '删除', (e) => {
        e.stopPropagation();
        deleteItem(node.id);
      })
    );
  } else {
    actions.append(
      iconButton('＋', '新建请求', (e) => {
        e.stopPropagation();
        addRequest(node.id);
      }),
      renameAction,
      iconButton('🗀', '新建文件夹', (e) => {
        e.stopPropagation();
        addFolder(node.id);
      }),
      iconButton('✕', '删除', (e) => {
        e.stopPropagation();
        deleteItem(node.id);
      })
    );
  }
  row.appendChild(actions);

  if (renamingId === node.id) {
    const input = document.createElement('input');
    input.className = 'tree-rename-input';
    input.value = node.name;
    row.replaceChild(input, label);
    pendingFocus = input;

    let cancelled = false;
    const commit = () => {
      if (cancelled) return;
      renamingId = '';
      renameItem(node.id, input.value);
    };
    input.addEventListener('blur', commit);
    input.addEventListener('keydown', (e) => {
      if (e.key === 'Enter') {
        e.preventDefault();
        input.blur();
      }
      if (e.key === 'Escape') {
        e.preventDefault();
        cancelled = true;
        renamingId = '';
        renderTree();
      }
    });
  } else {
    row.addEventListener('click', () => {
      if (Date.now() < suppressClickUntil) return;
      if (isRequest && node.request) {
        emit('open-request', { request: node.request });
        $('#url-input').focus();
      } else {
        if (expanded.has(node.id)) expanded.delete(node.id);
        else expanded.add(node.id);
        renderTree();
      }
    });
  }

  row.addEventListener('contextmenu', (e) => {
    if (e.target.closest('input')) return;
    e.preventDefault();
    e.stopPropagation();
    showNodeContextMenu(e.clientX, e.clientY, node, isRequest);
  });

  row.draggable = true;
  row.addEventListener('dragstart', (e) => {
    if (renamingId) {
      e.preventDefault();
      return;
    }
    draggedNodeId = node.id;
    row.classList.add('dragging');
    if (e.dataTransfer) {
      e.dataTransfer.effectAllowed = 'move';
      e.dataTransfer.setData('text/plain', node.id);
    }
  });
  row.addEventListener('dragend', () => {
    suppressClickUntil = Date.now() + 250;
    clearDragState();
  });
  row.addEventListener('dragover', (e) => {
    if (!draggedNodeId || draggedNodeId === node.id) return;
    const dragged = findItem(state.collections, draggedNodeId).item;
    if (!dragged || treeContainsNode(dragged, node.id)) {
      clearDropMarkers();
      return;
    }
    e.preventDefault();
    e.stopPropagation();
    if (e.dataTransfer) e.dataTransfer.dropEffect = 'move';
    const rect = row.getBoundingClientRect();
    const y = e.clientY - rect.top;
    const position =
      !isRequest && y > rect.height * 0.25 && y < rect.height * 0.75
        ? 'inside'
        : y < rect.height / 2
          ? 'before'
          : 'after';
    clearDropMarkers();
    row.classList.add(`drop-${position}`);
    row.dataset.dropPosition = position;
  });
  row.addEventListener('dragleave', (e) => {
    if (row.contains(e.relatedTarget)) return;
    row.classList.remove('drop-before', 'drop-after', 'drop-inside');
    delete row.dataset.dropPosition;
  });
  row.addEventListener('drop', (e) => {
    if (!draggedNodeId) return;
    e.preventDefault();
    e.stopPropagation();
    const position = row.dataset.dropPosition;
    if (!position) return;

    const id = draggedNodeId;
    suppressClickUntil = Date.now() + 250;
    const moved = moveItem(state.collections, id, node.id, position);
    clearDragState();
    if (!moved) return;

    if (position === 'inside') expanded.add(node.id);
    emit('collections');
    revealActiveRequest();
    renderTree();
  });

  wrap.appendChild(row);

  if (!isRequest && (node.items || []).length && expanded.has(node.id)) {
    const children = document.createElement('div');
    children.className = 'tree-children';
    node.items.forEach((child) => children.appendChild(renderNode(child, false)));
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

/** 新建环境；全局变量只保留一份，重复点击时直接展开已有项。 */
function addEnvironment(isGlobal) {
  if (isGlobal) {
    const existing = state.environments.find((e) => e.isGlobal);
    if (existing) {
      editingEnvId = existing.id;
      renderEnvs();
      return;
    }
  }

  const name = isGlobal ? '全局变量' : '新环境';
  const env = { id: uid(), name, values: [], isGlobal: !!isGlobal };
  state.environments.push(env);
  editingEnvId = env.id;
  renamingEnvId = env.id;
  emit('environments');
  renderEnvs();
  renderEnvSelect();
}

/** 切换当前环境，由 app.js 统一重绘并持久化。 */
function activateEnv(id) {
  if (state.activeEnvId === id) return;
  state.activeEnvId = id;
  emit('env-changed');
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
    head.title = env.isGlobal ? '点击编辑全局变量，双击重命名' : '点击启用此环境，双击重命名';

    const name = document.createElement('span');
    name.className = 'env-name';
    name.textContent = env.name;
    name.title = '双击重命名';

    const count = document.createElement('span');
    count.className = 'tree-count';
    count.textContent = `${countEnabled(env)} 个变量`;

    const actions = document.createElement('span');
    actions.className = 'env-actions';
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
        if (editingEnvId === env.id) editingEnvId = '';
        if (renamingEnvId === env.id) renamingEnvId = '';
        emit('environments');
        emit('env-changed');
      })
    );

    head.append(name, count);
    if (env.isGlobal || env.id === state.activeEnvId) {
      const badge = document.createElement('span');
      badge.className = 'badge';
      badge.textContent = env.isGlobal ? '全局' : '使用中';
      head.appendChild(badge);
    }
    head.appendChild(actions);

    head.addEventListener('click', (e) => {
      if (e.target.closest('button')) return;
      if (env.isGlobal) {
        editingEnvId = editingEnvId === env.id ? '' : env.id;
        renderEnvs();
      } else {
        activateEnv(env.id);
      }
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

      let cancelled = false;
      const commit = () => {
        if (cancelled) return;
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
          e.preventDefault();
          cancelled = true;
          renamingEnvId = '';
          renderEnvs();
        }
      });
    }

    card.appendChild(head);

    if (env.id === editingEnvId) {
      const body = document.createElement('div');
      body.className = 'env-body';

      const toolbar = document.createElement('div');
      toolbar.className = 'env-toolbar';
      const bulk = document.createElement('button');
      bulk.type = 'button';
      bulk.className = 'btn btn-sm';
      bulk.textContent = '批量编辑';
      bulk.title = '以 KEY=value 文本批量编辑变量';
      bulk.addEventListener('click', () => openEnvBulkEditor(env));
      toolbar.appendChild(bulk);
      body.appendChild(toolbar);

      const editorHost = document.createElement('div');
      createKVEditor(editorHost, {
        getRows: () => env.values,
        onChange: () => {
          count.textContent = `${countEnabled(env)} 个变量`;
          emit('environments');
          renderEnvSelect();
        },
        keyPlaceholder: '变量名',
        valuePlaceholder: '变量值',
        descPlaceholder: '说明',
      });
      body.appendChild(editorHost);

      card.appendChild(body);
    }

    box.appendChild(card);
  });

  focusPending();
}

/** 打开环境变量的批量编辑弹窗。 */
function openEnvBulkEditor(env) {
  const backdrop = document.createElement('div');
  backdrop.className = 'modal-backdrop';
  backdrop.innerHTML = `
    <div class="modal">
      <div class="modal-title">批量编辑变量 · ${escapeHtml(env.name)}</div>
      <div class="modal-body">
        <div class="hint">每行一个 KEY=value，行首 # 为注释。同名变量会保留原来的启用状态和说明。</div>
        <textarea id="env-bulk-input" class="textarea bulk-textarea" spellcheck="false" placeholder="baseUrl=https://api.example.com&#10;token={{token}}"></textarea>
      </div>
      <div class="modal-actions">
        <button type="button" class="btn" data-act="cancel">取消</button>
        <button type="button" class="btn btn-primary" data-act="ok">应用</button>
      </div>
    </div>`;

  const textarea = backdrop.querySelector('#env-bulk-input');
  textarea.value = formatEnvValues(env.values);
  const close = () => backdrop.remove();

  const apply = () => {
    const { rows, ignored } = parseEnvText(textarea.value, env.values);
    env.values = rows;
    editingEnvId = env.id;
    emit('environments');
    renderEnvs();
    renderEnvSelect();
    close();

    const enabled = rows.filter(
      (row) => row.enabled && String(row.key || '').trim()
    ).length;
    const summary =
      enabled === rows.length ? `${rows.length} 个变量` : `${rows.length} 条（${enabled} 个启用）`;
    emit('toast', { message: `变量已更新：${summary}`, kind: 'success' });
    if (ignored) {
      emit('toast', { message: `已忽略 ${ignored} 行无法解析的内容`, kind: 'warn' });
    }
  };

  backdrop.addEventListener('keydown', (e) => {
    if ((e.ctrlKey || e.metaKey) && e.key === 'Enter') {
      e.preventDefault();
      apply();
    }
  });
  backdrop.addEventListener('click', (e) => {
    const act = e.target.closest('[data-act]')?.dataset.act;
    if (act === 'cancel' || e.target === backdrop) {
      close();
      return;
    }
    if (act === 'ok') apply();
  });

  document.body.appendChild(backdrop);
  textarea.focus();
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
    options.push(`<option value="${escapeAttr(env.id)}">${escapeHtml(env.name)}</option>`);
  }
  select.innerHTML = options.join('');
  select.value = current;

  const hint = $('#env-hint');
  const active = state.environments.find((e) => e.id === current);
  const globalCount = state.environments
    .filter((e) => e.isGlobal)
    .reduce((sum, env) => sum + countEnabled(env), 0);
  const parts = [];
  if (active) parts.push(`当前环境 ${countEnabled(active)} 个变量`);
  if (globalCount) parts.push(`全局 ${globalCount} 个变量`);
  hint.textContent = parts.join(' · ');
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
      emit('open-request', { request: entry.request });
      $('#url-input').focus();
    });
    box.appendChild(item);
  });
}

/** 清空历史：先写后端，再更新界面，避免刷新后记录又出现。 */
async function clearHistory() {
  if (!state.history?.length) return;
  const ok = await confirmDialog('确定清空全部历史记录吗？', '清空历史');
  if (!ok) return;

  try {
    await bridge.clearHistory();
  } catch (err) {
    emit('toast', { message: `清空历史失败：${err}`, kind: 'error' });
    return;
  }
  state.history = [];
  emit('history');
  renderHistory();
}

export { renderTree, renderEnvs, renderHistory, renderEnvSelect };
