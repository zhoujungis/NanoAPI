// envbulk.js —— 环境变量批量编辑：KEY=value 文本与变量数组互转。

/** 把一组变量格式化为文本；空变量名会被忽略。 */
export function formatEnvValues(values) {
  return (values || [])
    .filter((value) => String(value?.key ?? '').trim())
    .map((value) => {
      const key = String(value.key).trim();
      const text = String(value.value ?? '');
      return needsQuote(text) ? `${key}="${escapeEnvValue(text)}"` : `${key}=${text}`;
    })
    .join('\n');
}

/**
 * 解析 KEY=value 文本。
 * @param {string} text
 * @param {Array} existingValues 当前变量，用于保留同名变量的启用状态和说明
 * @returns {{rows: Array, ignored: number}}
 */
export function parseEnvText(text, existingValues = []) {
  const previous = new Map();
  for (const value of existingValues || []) {
    const key = String(value?.key ?? '').trim();
    if (key) previous.set(key, value);
  }

  const rows = [];
  const byKey = new Map();
  let ignored = 0;

  for (const rawLine of String(text ?? '').replace(/^\uFEFF/, '').split(/\r?\n/)) {
    let line = rawLine.trim();
    if (!line || line.startsWith('#')) continue;
    line = line.replace(/^export\s+/i, '');

    const index = line.indexOf('=');
    if (index <= 0) {
      ignored += 1;
      continue;
    }

    const key = line.slice(0, index).trim();
    if (!key) {
      ignored += 1;
      continue;
    }

    let value = line.slice(index + 1).trim();
    if (
      (value.startsWith('"') && value.endsWith('"')) ||
      (value.startsWith("'") && value.endsWith("'"))
    ) {
      const quote = value[0];
      value = value.slice(1, -1);
      if (quote === '"') {
        value = value.replace(/\\([nrt"\\])/g, (_, ch) => {
          if (ch === 'n') return '\n';
          if (ch === 'r') return '\r';
          if (ch === 't') return '\t';
          if (ch === '"') return '"';
          return '\\';
        });
      }
    }

    const prior = previous.get(key);
    if (byKey.has(key)) {
      // 同名变量以最后一行为准，和 .env 的常见行为一致。
      byKey.get(key).value = value;
      continue;
    }

    const row = {
      key,
      value,
      enabled: prior ? prior.enabled !== false : true,
      desc: prior?.desc || '',
    };
    byKey.set(key, row);
    rows.push(row);
  }

  return { rows, ignored };
}

function needsQuote(value) {
  if (/[\r\n\t"]/.test(value)) return true;
  if (value !== value.trim()) return true;
  if (value.length >= 2) {
    const first = value[0];
    const last = value[value.length - 1];
    if ((first === '"' && last === '"') || (first === "'" && last === "'")) return true;
  }
  return false;
}

function escapeEnvValue(value) {
  return value
    .replace(/\\/g, '\\\\')
    .replace(/"/g, '\\"')
    .replace(/\r/g, '\\r')
    .replace(/\n/g, '\\n')
    .replace(/\t/g, '\\t');
}
