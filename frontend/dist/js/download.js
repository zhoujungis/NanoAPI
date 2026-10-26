// download.js —— 根据响应信息生成保存文件名。

const EXTENSIONS = {
  'application/json': 'json',
  'text/json': 'json',
  'text/html': 'html',
  'application/xhtml+xml': 'html',
  'text/plain': 'txt',
  'text/xml': 'xml',
  'application/xml': 'xml',
  'text/csv': 'csv',
  'text/markdown': 'md',
  'text/javascript': 'js',
  'application/javascript': 'js',
  'application/pdf': 'pdf',
  'application/zip': 'zip',
  'application/gzip': 'gz',
  'application/x-tar': 'tar',
  'application/octet-stream': 'bin',
  'application/wasm': 'wasm',
  'application/msword': 'doc',
  'application/vnd.ms-excel': 'xls',
  'application/rtf': 'rtf',
  'application/vnd.openxmlformats-officedocument.wordprocessingml.document': 'docx',
  'application/vnd.openxmlformats-officedocument.spreadsheetml.sheet': 'xlsx',
  'application/vnd.openxmlformats-officedocument.presentationml.presentation': 'pptx',
  'image/png': 'png',
  'image/jpeg': 'jpg',
  'image/gif': 'gif',
  'image/webp': 'webp',
  'image/svg+xml': 'svg',
  'image/bmp': 'bmp',
  'image/tiff': 'tiff',
  'image/x-icon': 'ico',
  'image/avif': 'avif',
  'audio/mpeg': 'mp3',
  'audio/wav': 'wav',
  'audio/ogg': 'ogg',
  'video/mp4': 'mp4',
  'video/webm': 'webm',
  'font/woff': 'woff',
  'font/woff2': 'woff2',
};

/** 根据 Content-Type 推断扩展名。 */
export function extensionFor(contentType, isBinary = false) {
  const type = String(contentType || '').split(';')[0].trim().toLowerCase();
  if (!type) return isBinary ? 'bin' : 'txt';
  if (EXTENSIONS[type]) return EXTENSIONS[type];
  if (type.endsWith('+json')) return 'json';
  if (type.endsWith('+xml')) return 'xml';
  if (type.startsWith('text/')) return 'txt';
  return isBinary ? 'bin' : 'txt';
}

/** 从 Content-Disposition 中读取文件名，优先支持 RFC 5987 的 filename*。 */
export function filenameFromDisposition(headers) {
  if (!headers) return '';

  let raw = '';
  for (const [key, values] of Object.entries(headers)) {
    if (key.toLowerCase() !== 'content-disposition') continue;
    raw = Array.isArray(values) ? values.filter(Boolean).join('; ') : String(values || '');
    break;
  }
  if (!raw) return '';

  const star = raw.match(/filename\*\s*=\s*("([^"]*)"|[^;]+)/i);
  if (star) {
    let value = (star[2] ?? star[1] ?? '').trim();
    if (value.startsWith('"') && value.endsWith('"')) value = value.slice(1, -1);
    const parts = value.split("'");
    if (parts.length >= 3) {
      const encoded = parts.slice(2).join("'");
      try {
        value = decodeURIComponent(encoded);
      } catch {
        value = encoded;
      }
    } else {
      try {
        value = decodeURIComponent(value);
      } catch {
        /* 保留原值 */
      }
    }
    if (value) return value;
  }

  const plain = raw.match(/filename\s*=\s*("([^"]*)"|([^;]+))/i);
  if (plain) {
    const value = (plain[2] ?? plain[3] ?? '').trim();
    if (value) return value;
  }
  return '';
}

/** 清理文件名，去掉路径和 Windows 不允许的字符。 */
export function safeFilename(name, fallback = 'response') {
  let out = String(name ?? '').trim();
  if (!out) return fallback;

  const cut = Math.max(out.lastIndexOf('/'), out.lastIndexOf('\\'));
  if (cut >= 0) out = out.slice(cut + 1);

  out = out.replace(/[\\/:*?"<>|\u0000-\u001f]/g, '_');
  out = out.replace(/[. ]+$/g, '').trim();
  if (!out) return fallback;

  const chars = Array.from(out);
  if (chars.length > 120) {
    const dot = out.lastIndexOf('.');
    const ext = dot > 0 && chars.length - dot <= 12 ? out.slice(dot) : '';
    const base = ext ? out.slice(0, dot) : out;
    const keep = Math.max(1, 120 - Array.from(ext).length);
    out = Array.from(base).slice(0, keep).join('') + ext;
  }
  return out;
}

/** 在基础文件名后追加后缀，并限制总长度。 */
function withSuffix(name, suffix, fallback) {
  const cleaned = safeFilename(name, 'response.txt');
  const dot = cleaned.lastIndexOf('.');
  let base = dot > 0 ? cleaned.slice(0, dot) : cleaned;

  const maxBase = 120 - Array.from(suffix).length;
  const chars = Array.from(base);
  if (chars.length > maxBase) base = chars.slice(0, maxBase).join('');
  return safeFilename(`${base}${suffix}`, fallback);
}

/** 生成“保存选中内容”的文件名，选中文本统一保存为 .txt。 */
export function selectionFilename(name) {
  return withSuffix(name, '-selected.txt', 'response-selected.txt');
}

/** 生成“保存响应头”的文件名。 */
export function headersFilename(name) {
  return withSuffix(name, '-headers.txt', 'response-headers.txt');
}

/** 生成响应体的默认保存文件名。 */
export function suggestResponseFilename(res, request) {
  const ext = extensionFor(res?.contentType, Boolean(res?.isBinary));
  const fallback = `response.${ext}`;

  const disposition = filenameFromDisposition(res?.headers);
  if (disposition) {
    const name = safeFilename(disposition, '');
    if (name) return name;
  }

  const rawUrl = String(res?.url || request?.url || '').trim();
  let segment = '';
  if (rawUrl) {
    try {
      const path = new URL(rawUrl, 'http://nanoapi.local').pathname || '';
      const last = path.split('/').filter(Boolean).pop() || '';
      if (last) {
        try {
          segment = decodeURIComponent(last);
        } catch {
          segment = last;
        }
      }
    } catch {
      /* 非标准 URL 时退回请求名 */
    }
  }

  const base = safeFilename(segment || request?.name || 'response', '');
  if (!base) return fallback;
  if (/\.[a-z0-9]{1,10}$/i.test(base)) return base;
  return safeFilename(`${base}.${ext}`, fallback);
}
