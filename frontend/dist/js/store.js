// store.js —— 应用状态、订阅机制与集合树辅助函数。

/** 创建一个空白请求。 */
export function emptyRequest() {
  return {
    id: '',
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
}

export const state = {
  collections: [],
  environments: [],
  history: [],
  settings: { timeoutMs: 30000, followRedirects: true, insecureTls: false, theme: 'dark' },
  activeEnvId: '',
  request: emptyRequest(),
  result: null,
  sending: false,
  panel: 'collections',
  responseView: 'pretty',
  appInfo: {},
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
