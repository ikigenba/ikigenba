// Button feedback for the ikigenba theme: every click springs its button back
// (.pulse), and a copy button in a .secret copies its code and confirms with a
// toast. On failure the code is selected so the keyboard shortcut still works.
(() => {
  const region = document.createElement("div");
  region.className = "toasts";
  region.setAttribute("aria-live", "polite");
  document.body.appendChild(region);

  const toast = (text, kind) => {
    region.replaceChildren();
    const t = document.createElement("div");
    t.className = "toast";
    if (kind) t.dataset.kind = kind;
    t.textContent = text;
    t.addEventListener("animationend", () => t.remove());
    region.appendChild(t);
  };

  const pulse = (b) => {
    b.classList.remove("pulse");
    void b.offsetWidth;
    b.classList.add("pulse");
  };

  const keys = /Mac|iPhone|iPad/.test(navigator.platform) ? "⌘C" : "Ctrl+C";

  document.addEventListener("click", (e) => {
    const b = e.target.closest("button, .button");
    if (!b || b.disabled || b.getAttribute("aria-disabled") === "true") return;
    pulse(b);
    if (!b.matches(".secret > button")) return;
    const code = b.parentNode.querySelector("code");
    const write = navigator.clipboard ? navigator.clipboard.writeText(code.textContent) : Promise.reject();
    write.then(
      () => toast("Copied to clipboard"),
      () => {
        getSelection().selectAllChildren(code);
        toast(`Couldn't copy. The text is selected; press ${keys}.`, "err");
      },
    );
  });
})();
