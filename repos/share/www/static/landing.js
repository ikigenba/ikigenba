(function () {
  "use strict";

  const pageSize = 10;

  function filterRepos(rows, query) {
    const needle = String(query).toLowerCase();
    if (needle === "") {
      return rows.slice();
    }
    return rows.filter((row) => {
      const key = row.key.toLowerCase();
      let index = 0;
      for (const character of key) {
        if (character === needle[index]) {
          index += 1;
        }
      }
      return index === needle.length;
    });
  }

  function sortRows(rows, dir) {
    const direction = dir === "desc" ? -1 : 1;
    return rows.slice().sort((left, right) => {
      if (left.key < right.key) {
        return -direction;
      }
      if (left.key > right.key) {
        return direction;
      }
      return 0;
    });
  }

  function paginate(rows, page, size) {
    return rows.slice((page - 1) * size, page * size);
  }

  function nextSort(state) {
    return { ...state, dir: state.dir === "asc" ? "desc" : "asc" };
  }

  function defaultState() {
    return { query: "", dir: "asc", page: 1 };
  }

  function reduce(state, action) {
    switch (action.type) {
      case "setQuery":
        return { ...state, query: action.query, page: 1 };
      case "setSort":
        return { ...state, dir: action.dir || nextSort(state).dir, page: 1 };
      case "setPage":
        return { ...state, page: action.page };
      case "clear":
        return defaultState();
      default:
        return { ...state };
    }
  }

  function computeView(rows, state) {
    const filtered = filterRepos(rows, state.query);
    const sorted = sortRows(filtered, state.dir);
    const total = sorted.length;
    const pageCount = Math.ceil(total / pageSize);
    const page = Math.min(Math.max(state.page, 1), Math.max(pageCount, 1));
    const visible = paginate(sorted, page, pageSize);
    const rangeFrom = total === 0 ? 0 : (page - 1) * pageSize + 1;
    const rangeTo = Math.min(page * pageSize, total);

    return {
      rows: visible,
      showControls: rows.length > 0,
      empty: rows.length === 0,
      noMatch: rows.length > 0 && total === 0,
      showPager: total > pageSize,
      page,
      pageCount,
      pageLabel: `Page ${page} of ${pageCount}`,
      rangeFrom,
      rangeTo,
      rangeLabel: `${rangeFrom}–${rangeTo} of ${total}`,
      total,
      dir: state.dir,
    };
  }

  function cloneURL(origin, clonePath) {
    return `${origin.replace(/\/$/, "")}/${clonePath.replace(/^\//, "")}`;
  }

  globalThis.ReposLanding = {
    filterRepos,
    sortRows,
    paginate,
    nextSort,
    defaultState,
    reduce,
    computeView,
    cloneURL,
  };

  function copyToClipboard(value) {
    if (globalThis.isSecureContext && navigator.clipboard) {
      return navigator.clipboard.writeText(value);
    }
    const textarea = document.createElement("textarea");
    textarea.value = value;
    textarea.setAttribute("aria-hidden", "true");
    textarea.style.position = "fixed";
    textarea.style.opacity = "0";
    document.body.appendChild(textarea);
    textarea.select();
    document.execCommand("copy");
    textarea.remove();
    return Promise.resolve();
  }

  function copyIcon() {
    const namespace = "http://www.w3.org/2000/svg";
    const svg = document.createElementNS(namespace, "svg");
    const rect = document.createElementNS(namespace, "rect");
    const path = document.createElementNS(namespace, "path");
    svg.setAttribute("aria-hidden", "true");
    svg.setAttribute("viewBox", "0 0 24 24");
    for (const [name, value] of Object.entries({ x: "9", y: "9", width: "11", height: "11", rx: "1" })) {
      rect.setAttribute(name, value);
    }
    path.setAttribute("d", "M15 9V5a1 1 0 0 0-1-1H5a1 1 0 0 0-1 1v9a1 1 0 0 0 1 1h4");
    svg.append(rect, path);
    return svg;
  }

  function initController() {
    const rows = JSON.parse(document.querySelector("#repos-data").textContent);
    const root = document.documentElement;
    const controls = document.querySelector(".controls");
    const search = document.querySelector("#repo-search");
    const clear = document.querySelector("#repo-clear");
    const nameHeader = document.querySelector("th[data-sort-key='key']");
    const tbody = document.querySelector(".repo-table tbody");
    const noMatch = document.querySelector(".no-match");
    const pager = document.querySelector(".pager");
    const pagerLabel = document.querySelector("#pager-label");
    const previous = document.querySelector("#pager-prev");
    const next = document.querySelector("#pager-next");
    let state = defaultState();

    root.classList.replace("no-js", "js");

    function render() {
      const view = computeView(rows, state);
      controls.hidden = !view.showControls;
      search.hidden = !view.showControls;
      clear.hidden = !view.showControls;
      pager.hidden = !view.showPager;
      noMatch.hidden = !view.noMatch;
      pagerLabel.textContent = view.pageLabel;
      previous.disabled = view.page <= 1;
      next.disabled = view.page >= view.pageCount;
      nameHeader.setAttribute("aria-sort", view.dir === "asc" ? "ascending" : "descending");
      tbody.replaceChildren(...view.rows.map((row) => {
        const tableRow = document.createElement("tr");
        const key = document.createElement("td");
        const sha = document.createElement("td");
        const copy = document.createElement("td");
        const button = document.createElement("button");
        const label = document.createElement("span");
        key.dataset.label = "Name";
        key.textContent = row.key;
        sha.dataset.label = "Sha";
        sha.className = "sha";
        sha.textContent = row.sha || "—";
        copy.dataset.label = "Copy";
        button.type = "button";
        button.className = "copy-btn";
        button.dataset.cloneUrl = cloneURL(location.origin, row.clonePath);
        label.className = "copy-label";
        label.textContent = "Copy";
        button.append(copyIcon(), label);
        copy.append(button);
        tableRow.append(key, sha, copy);
        return tableRow;
      }));
    }

    function dispatch(action) {
      state = reduce(state, action);
      render();
    }

    search.addEventListener("input", () => dispatch({ type: "setQuery", query: search.value }));
    search.addEventListener("keydown", (event) => {
      if (event.key === "Escape") {
        search.value = "";
        dispatch({ type: "setQuery", query: "" });
      }
    });
    nameHeader.addEventListener("click", () => dispatch({ type: "setSort" }));
    previous.addEventListener("click", () => dispatch({ type: "setPage", page: state.page - 1 }));
    next.addEventListener("click", () => dispatch({ type: "setPage", page: state.page + 1 }));
    clear.addEventListener("click", () => {
      search.value = "";
      dispatch({ type: "clear" });
    });
    tbody.addEventListener("click", (event) => {
      const button = event.target.closest(".copy-btn");
      if (!button) {
        return;
      }
      copyToClipboard(button.dataset.cloneUrl).then(() => {
        const label = button.querySelector(".copy-label");
        label.textContent = "Copied";
        button.classList.add("is-copied");
        setTimeout(() => {
          label.textContent = "Copy";
          button.classList.remove("is-copied");
        }, 1600);
      });
    });

    render();
  }

  if (typeof document !== "undefined") {
    document.addEventListener("DOMContentLoaded", () => initController());
  }
}());
