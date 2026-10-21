# Engineering

- Values lightweight tools: explicitly asks for "轻量化" software; prefers minimal dependency footprints, single-binary delivery, and no heavy build toolchains. Confidence: 0.7
- Prefers a scoped core/MVP feature set for the first version of a new project, with the rest deferred to later iterations. Confidence: 0.6
- For native desktop apps on Windows, prefers Go + Wails v2 (real window, system WebView2, single .exe, zero CGO) over Electron or browser-only approaches. Confidence: 0.6
- Likes small frontends built with plain HTML/CSS/ES modules, embedded into the binary, with no npm install and no bundler step. Confidence: 0.6
- Wants design decisions settled up front (architecture, scope, storage location) before implementation starts. Confidence: 0.6
