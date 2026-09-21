"use strict";

document.querySelectorAll("[data-copy-token]").forEach((button) => {
  button.addEventListener("click", () => navigator.clipboard.writeText(button.dataset.copyToken));
});
