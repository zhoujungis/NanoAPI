// headers.js —— 响应头格式化。

/** 按名称或值筛选响应头，大小写不敏感。 */
export function filterHeaders(headers, query) {
  const entries = Object.entries(headers || {});
  const q = String(query || '').trim().toLowerCase();
  if (!q) return entries;

  return entries.filter(([key, raw]) => {
    if (String(key).toLowerCase().includes(q)) return true;
    const values = Array.isArray(raw) ? raw : [raw];
    return values.some((value) => String(value ?? '').toLowerCase().includes(q));
  });
}

/** 把响应头对象格式化为每行一个 "Name: value" 的文本。 */
export function formatHeaders(headers) {
  const lines = [];
  for (const [key, raw] of Object.entries(headers || {})) {
    const values = Array.isArray(raw) ? raw : [raw];
    for (const value of values) {
      if (value === undefined || value === null) continue;
      lines.push(`${key}: ${value}`);
    }
  }
  return lines.join('\n');
}
