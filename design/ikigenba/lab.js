// Design-lab toolbar for the ikigenba theme: switch page.
// Lives in a shadow root so the theme's CSS never reaches it.
(() => {
  const PAGES = ["specimen", "app", "login", "profile", "landing", "prose"];
  const page = (location.pathname.split("/").pop() || "").replace(/\.html$/, "");

  const host = document.createElement("div");
  const shadow = host.attachShadow({ mode: "open" });
  const opts = (list, cur) =>
    list.map((v) => `<option${v === cur ? " selected" : ""}>${v}</option>`).join("");
  shadow.innerHTML = `
    <style>
      :host { all: initial; }
      .bar { position: fixed; right: 12px; bottom: 12px; z-index: 2147483647;
        display: flex; gap: 6px; align-items: center; padding: 6px;
        font: 12px/1 system-ui, sans-serif; background: #111; color: #eee;
        border-radius: 8px; box-shadow: 0 4px 16px rgba(0,0,0,.3); opacity: .6; transition: opacity .15s; }
      .bar:hover, .bar:focus-within { opacity: 1; }
      select, a { font: inherit; color: inherit; background: #242424; border: 1px solid #3a3a3a;
        border-radius: 5px; padding: 4px 6px; text-decoration: none; }
    </style>
    <div class="bar">
      <a href="../index.html" title="Design lab index">▦</a>
      <select id="p" aria-label="Page">${opts(PAGES, page)}</select>
    </div>`;
  shadow.getElementById("p").onchange = (e) => (location.href = `${e.target.value}.html`);
  document.body.appendChild(host);
})();
