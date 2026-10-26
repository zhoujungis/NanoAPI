// store.js —— 应用状态、订阅机制与集合树辅助函数。

export const state = {
  collections: [],
  environments: [],
  history: [],
  settings: { timeoutMs: 30000, followRedirects: true, insecureTls: false, theme: 'dark' },
  activeEnvId: '',
  tabs: [],
  activeTabKey: '',
  sending: false,
  panel: 'collections',
  responseView: 'pretty',
  appInfo: {},

  get activeTab() {
    return this.tabs.find((tab) => tab.key === this.activeTabKey) || null;
  },
  get request() {
    return this.activeTab ? this.activeTab.request : null;
  },
  set request(req) {
    const tab = this.activeTab;
    if (tab) tab.request = req;
  },
  get dirty() {
    return this.activeTab ? Boolean(this.activeTab.dirty) : false;
  },
  set dirty(value) {
    const tab = this.activeTab;
    if (tab) tab.dirty = Boolean(value);
  },
  get result() {
    return this.activeTab ? this.activeTab.result : null;
  },
  set result(value) {
    const tab = this.activeTab;
    if (tab) tab.result = value ?? null;
  },
  get resultRequestId() {
    return this.activeTab ? this.activeTab.resultRequestId || '' : '';
  },
  set resultRequestId(value) {
    const tab = this.activeTab;
    if (tab) tab.resultRequestId = value || '';
  },
};

const listeners = new Map();

/** 订阅某个主题的变化，返回取消订阅的函数。 */
export function on(topic, fn) {
  if (!listeners.has(topic)) listeners.set(topic, new Set());
  listeners.get(topic).add(fn);
  return () => listeners.get(topic).delete(fn);
}

/** 通知某个主题的订阅者。 */
export function emit(topic, payload) {
  const set = listeners.get(topic);
  if (!set) return;
  for (const fn of Array.from(set)) fn(payload);
}

/**
 * 在集合树中按 ID 查找节点。
 * 同时匹配树节点自身的 ID 与其中保存的请求 ID —— 两者是不同的一套标识。
 */
export function findItem(items, id) {
  for (const item of items || []) {
    if (!item) continue;
    if (item.id === id || item.request?.id === id) return { parent: null, item };
    if (item.items?.length) {
      const found = findItem(item.items, id);
      if (found.item) return { parent: item, item: found.item };
    }
  }
  return { parent: null, item: null };
}

/** 从集合树中删除指定 ID 的节点。 */
export function removeItem(items, id) {
  const list = items || [];
  for (let i = 0; i < list.length; i += 1) {
    if (list[i].id === id) {
      list.splice(i, 1);
      return true;
    }
    if (removeItem(list[i].items, id)) return true;
  }
  return false;
}

/**
 * 在集合树中移动节点。
 * @param {Array} items 根节点数组（通常传 state.collections）
 * @param {string} draggedId 被拖动节点 ID
 * @param {string} targetId 目标节点 ID；position 为 root 时忽略
 * @param {'inside'|'before'|'after'|'root'} position 落点位置
 * @returns {boolean} 是否发生了移动
 */
export function moveItem(items, draggedId, targetId, position) {
  if (!Array.isArray(items) || !draggedId) return false;
  if (position !== 'root' && !targetId) return false;

  const dragged = locateItem(items, draggedId);
  if (!dragged) return false;

  if (position === 'root') {
    dragged.list.splice(dragged.index, 1);
    items.push(dragged.node);
    return true;
  }

  if (position !== 'inside' && position !== 'before' && position !== 'after') {
    return false;
  }

  const target = locateItem(items, targetId);
  if (!target || target.node === dragged.node) return false;
  if (nodeContains(dragged.node, targetId)) return false;
  if (position === 'inside' && target.node.type === 'request') return false;

  dragged.list.splice(dragged.index, 1);

  if (position === 'inside') {
    target.node.items = target.node.items || [];
    target.node.items.push(dragged.node);
    return true;
  }

  // 同层移动时，移除被拖动节点后目标索引会变化，重新定位一次。
  const current = locateItem(items, targetId);
  if (!current) {
    dragged.list.splice(Math.min(dragged.index, dragged.list.length), 0, dragged.node);
    return false;
  }
  const index = position === 'before' ? current.index : current.index + 1;
  current.list.splice(index, 0, dragged.node);
  return true;
}

function locateItem(items, id, parent = null) {
  for (let i = 0; i < (items || []).length; i += 1) {
    const item = items[i];
    if (!item) continue;
    if (item.id === id) {
      return { node: item, parent, list: items, index: i };
    }
    if (item.items?.length) {
      const found = locateItem(item.items, id, item);
      if (found) return found;
    }
  }
  return null;
}

function nodeContains(node, id) {
  if (!node) return false;
  if (node.id === id) return true;
  return (node.items || []).some((child) => nodeContains(child, id));
}

/** 把集合树中的集合与文件夹拍平成可用于下拉框的列表。 */
export function flattenTargets(items, depth = 0, out = []) {
  for (const item of items || []) {
    if (!item) continue;
    if (item.type === 'request') continue;
    out.push({ id: item.id, name: item.name, depth, type: item.type });
    if (item.items?.length) flattenTargets(item.items, depth + 1, out);
  }
  return out;
}

/** 统计集合树中的请求数量。 */
export function countRequests(items) {
  let n = 0;
  for (const item of items || []) {
    if (!item) continue;
    if (item.type === 'request') n += 1;
    n += countRequests(item.items);
  }
  return n;
}
