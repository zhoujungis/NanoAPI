// kv.js —— 键值对表格编辑器。
//
// 参数、请求头、表单字段和环境变量都复用这个组件，
// 因此它只负责编辑一组 {key, value, enabled, desc} 行。

import { escapeAttr } from './util.js';

/**
 * 创建键值表格编辑器。
 *
 * @param {HTMLElement} container 挂载容器
 * @param {object} opts
 * @param {() => Array} opts.getRows 读取当前数据
 * @param {(rows: Array) => void} opts.onChange 数据变化回调
 * @param {string} [opts.keyPlaceholder] 键列占位符
 * @param {string} [opts.valuePlaceholder] 值列占位符
 * @param {string} [opts.descPlaceholder] 说明列占位符
 * @param {boolean} [opts.allowDesc] 是否显示说明列
 */
export function createKVEditor(container, opts) {
  const {
    getRows,
    onChange,
    keyPlaceholder = '键',
    valuePlaceholder = '值',
    descPlaceholder = '说明',
    allowDesc = true,
  } = opts;

  container.classList.add('kv-editor');
  container.innerHTML = `
    <table class="kv-table">
      <thead>
        <tr>
          <th class="col-check"></th>
          <th class="col-key">${keyPlaceholder}</th>
          <th class="col-value">${valuePlaceholder}</th>
          ${allowDesc ? `<th class="col-desc">${descPlaceholder}</th>` : ''}
          <th class="col-act"></th>
        </tr>
      </thead>
      <tbody></tbody>
    </table>
    <div class="kv-footer">
      <button type="button" class="btn btn-sm" data-act="add">＋ 添加一行</button>
    </div>`;

  const tbody = container.querySelector('tbody');

  /** 取得模型数组；若尚无任何行则先造一行，避免输入无处落笔。 */
  function ensureRow(index) {
    const list = getRows();
    if (!Array.isArray(list)) return null;
    if (index >= list.length) {
      list.push({ key: '', value: '', enabled: true, desc: '' });
    }
    return list;
  }

  function emitChange() {
    onChange(getRows());
  }

  function render() {
    const list = getRows() || [];
    tbody.innerHTML = '';
    const shown = list.length ? list : [{ key: '', value: '', enabled: true, desc: '' }];
    shown.forEach((row, index) => tbody.appendChild(buildRow(row, index)));
  }

  function buildRow(row, index) {
    const tr = document.createElement('tr');
    tr.innerHTML = `
      <td class="col-check"><input type="checkbox" class="kv-check" ${row.enabled ? 'checked' : ''} /></td>
      <td class="col-key"><input class="input kv-key" value="${escapeAttr(row.key)}" placeholder="${escapeAttr(keyPlaceholder)}" spellcheck="false" /></td>
      <td class="col-value"><input class="input kv-value" value="${escapeAttr(row.value)}" placeholder="${escapeAttr(valuePlaceholder)}" spellcheck="false" /></td>
      ${allowDesc ? `<td class="col-desc"><input class="input kv-desc" value="${escapeAttr(row.desc || '')}" placeholder="${escapeAttr(descPlaceholder)}" /></td>` : ''}
      <td class="col-act"><button type="button" class="icon-btn kv-del" title="删除这一行">✕</button></td>`;

    const check = tr.querySelector('.kv-check');
    const key = tr.querySelector('.kv-key');
    const value = tr.querySelector('.kv-value');
    const desc = tr.querySelector('.kv-desc');

    check.addEventListener('change', () => {
      const list = ensureRow(index);
      if (!list) return;
      list[index].enabled = check.checked;
      emitChange();
    });

    const bindInput = (input, field) => {
      input.addEventListener('input', () => {
        const list = ensureRow(index);
        if (!list) return;
        list[index][field] = input.value;
        emitChange();
      });
    };
    bindInput(key, 'key');
    bindInput(value, 'value');
    if (desc) bindInput(desc, 'desc');

    tr.querySelector('.kv-del').addEventListener('click', () => {
      const list = getRows() || [];
      if (index < list.length) list.splice(index, 1);
      render();
      emitChange();
    });

    return tr;
  }

  container.querySelector('[data-act="add"]').addEventListener('click', () => {
    const list = getRows();
    list.push({ key: '', value: '', enabled: true, desc: '' });
    render();
    const inputs = tbody.querySelectorAll('.kv-key');
    inputs[inputs.length - 1]?.focus();
    emitChange();
  });

  render();
  return { render };
}
