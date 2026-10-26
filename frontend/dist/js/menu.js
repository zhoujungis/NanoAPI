// menu.js —— 通用右键菜单。

let menuEl = null;
let listenersBound = false;

/** 关闭当前右键菜单。 */
export function closeContextMenu() {
  if (!menuEl) return;
  menuEl.remove();
  menuEl = null;
}

function bindGlobalClose() {
  if (listenersBound) return;
  listenersBound = true;

  document.addEventListener(
    'mousedown',
    (e) => {
      if (menuEl && !menuEl.contains(e.target)) closeContextMenu();
    },
    true
  );
  document.addEventListener('keydown', (e) => {
    if (e.key === 'Escape' && menuEl) closeContextMenu();
  });
  document.addEventListener(
    'scroll',
    (e) => {
      if (menuEl && !menuEl.contains(e.target)) closeContextMenu();
    },
    true
  );
  window.addEventListener('resize', closeContextMenu);
  window.addEventListener('blur', closeContextMenu);
}

/**
 * 在鼠标位置打开右键菜单，并限制在视口内。
 * @param {number} x
 * @param {number} y
 * @param {Array<{label?: string, action?: Function, danger?: boolean, disabled?: boolean, separator?: boolean}>} items
 * @param {string} title
 */
export function showContextMenu(x, y, items, title = '') {
  closeContextMenu();
  if (!items?.length) return;
  bindGlobalClose();

  const menu = document.createElement('div');
  menu.className = 'context-menu';

  if (title) {
    const head = document.createElement('div');
    head.className = 'context-menu-title';
    head.textContent = title;
    menu.appendChild(head);
  }

  for (const item of items) {
    if (!item) continue;
    if (item.separator) {
      const sep = document.createElement('div');
      sep.className = 'context-menu-sep';
      menu.appendChild(sep);
      continue;
    }
    const btn = document.createElement('button');
    btn.type = 'button';
    btn.className = `context-menu-item${item.danger ? ' danger' : ''}`;
    btn.textContent = item.label;
    btn.disabled = Boolean(item.disabled);
    if (!item.disabled) {
      btn.addEventListener('click', () => {
        closeContextMenu();
        item.action();
      });
    }
    menu.appendChild(btn);
  }

  document.body.appendChild(menu);
  menuEl = menu;

  const rect = menu.getBoundingClientRect();
  const left = Math.max(8, Math.min(x, window.innerWidth - rect.width - 8));
  const top = Math.max(8, Math.min(y, window.innerHeight - rect.height - 8));
  menu.style.left = `${left}px`;
  menu.style.top = `${top}px`;

  menu.querySelector('.context-menu-item:not(:disabled)')?.focus();
}
