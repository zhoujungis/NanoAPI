// bridge.js —— 与 Go 后端通信的封装。
//
// Wails v2 在运行时把绑定的方法挂到 window.go.main.App 上，
// 每个方法都返回一个 Promise。

function backend() {
  const app = window.go?.main?.App;
  if (!app) throw new Error('后端尚未就绪');
  return app;
}

/**
 * 调用后端方法，统一把同步异常转成 rejected promise，
 * 避免调用方漏掉 .catch 时抛出未捕获错误。
 */
function call(method, ...args) {
  try {
    return Promise.resolve(backend()[method](...args));
  } catch (err) {
    return Promise.reject(err);
  }
}

/** 后端是否已经可以调用。 */
export function backendReady() {
  return Boolean(window.go?.main?.App);
}

export const bridge = {
  loadAll: () => call('LoadAll'),
  saveCollections: (collections) => call('SaveCollections', collections),
  saveEnvironments: (environments) => call('SaveEnvironments', environments),
  saveSettings: (settings) => call('SaveSettings', settings),
  clearHistory: () => call('ClearHistory'),
  sendRequest: (request, envId, options) => call('SendRequest', request, envId, options),
  pickFile: () => call('PickFile'),
  appInfo: () => call('AppInfo'),
};
