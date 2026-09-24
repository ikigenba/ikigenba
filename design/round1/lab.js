// Design-lab toolbar: switch theme and page, flip light/dark.
// Lives in a shadow root so no theme's CSS reaches it.
(() => {
  const THEMES = ["console", "blueprint", "signal"];
  const PAGES = ["specimen", "app", "login"];
  const parts = location.pathname.split("/");
  const page = (parts.pop() || "index.html").replace(/\.html$/, "");
  const theme = parts.pop();
  const root = document.documentElement;

  let mode = null;
  try { mode = localStorage.getItem("lab-mode"); } catch {}
  if (mode) root.dataset.theme = mode;

  const host = document.createElement("div");
  const shadow = host.attachShadow({ mode: "open" });
  const opts = (list, cur) =>
    list.map((v) => `<option${v === cur ? " selected" : ""}>${v}</option>`).join("");
  shadow.innerHTML = `
    <style>
      :host { all: initial; }
      .bar { position: fixed; right: 12px; bottom: 12px; z-index: 2147483647;
        display: flex; gap: 6px; align-items: center; padding: 6px;
        font: 12px/1 ui-monospace, SFMono-Regular, Menlo, monospace;
        background: #111; color: #eee; border: 1px solid #444; border-radius: 6px;
        box-shadow: 0 4px 16px rgba(0,0,0,.35); opacity: .55; transition: opacity .15s; }
      .bar:hover, .bar:focus-within { opacity: 1; }
      select, button, a { font: inherit; color: inherit; background: #222;
        border: 1px solid #444; border-radius: 4px; padding: 4px 6px; text-decoration: none; }
      button { cursor: pointer; }
    </style>
    <div class="bar">
      <a href="../index.html" title="All themes">▦</a>
      <select id="t" aria-label="Theme">${opts(THEMES, theme)}</select>
      <select id="p" aria-label="Page">${opts(PAGES, page)}</select>
      <button id="m" title="Cycle light / dark / system"></button>
    </div>`;

  const label = () => (shadow.getElementById("m").textContent = root.dataset.theme || "system");
  const go = () =>
    (location.href = `../${shadow.getElementById("t").value}/${shadow.getElementById("p").value}.html`);
  shadow.getElementById("t").onchange = go;
  shadow.getElementById("p").onchange = go;
  shadow.getElementById("m").onclick = () => {
    const next = { undefined: "light", light: "dark", dark: undefined }[root.dataset.theme];
    if (next) root.dataset.theme = next; else delete root.dataset.theme;
    try { next ? localStorage.setItem("lab-mode", next) : localStorage.removeItem("lab-mode"); } catch {}
    label();
  };
  label();
  document.body.appendChild(host);
})();
