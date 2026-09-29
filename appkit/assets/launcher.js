// The service launcher (see banner.html). The panel is a popover the banner's
// button opens; this script only filters it. Typing keeps the tiles whose
// service name contains the query, ignoring case; with none left it says so.
// Enter opens the first tile still shown that is a link.
(() => {
  const nav = document.getElementById("services");
  if (!nav) return;
  const q = nav.querySelector("input");
  const empty = nav.querySelector("p");
  const tiles = [...nav.querySelectorAll("li")].map((li) => {
    const a = li.querySelector("a");
    const name = [...a.childNodes]
      .filter((n) => n.nodeType === Node.TEXT_NODE)
      .map((n) => n.data).join("").trim().toLowerCase();
    return { li, a, name };
  });
  q.addEventListener("input", () => {
    const t = q.value.trim();
    const lt = t.toLowerCase();
    let shown = 0;
    for (const tile of tiles) {
      tile.li.hidden = !tile.name.includes(lt);
      if (!tile.li.hidden) shown++;
    }
    empty.hidden = shown > 0;
    empty.querySelector("q").textContent = t;
  });
  q.addEventListener("keydown", (e) => {
    if (e.key !== "Enter") return;
    const first = tiles.find((tile) => !tile.li.hidden && tile.a.hasAttribute("href"));
    if (first) first.a.click();
  });
})();
