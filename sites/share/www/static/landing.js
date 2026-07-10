// SitesLanding contains the landing page's data transformations.  Keeping these
// functions free of DOM state makes the rendered view deterministic and testable.
(function () {
  "use strict";

  function filterSites(rows, query) {
    var needle = String(query || "").toLowerCase();
    if (!needle) return rows.slice();
    return rows.filter(function (row) {
      var slug = String(row.slug || "").toLowerCase();
      var position = 0;
      for (var i = 0; i < slug.length && position < needle.length; i++) {
        if (slug[i] === needle[position]) position++;
      }
      return position === needle.length;
    });
  }

  function sortRows(rows, key, dir) {
    var field = key === "name" ? "slug" : key === "createdAt" ? "createdAtSort" : "createdBy";
    var direction = dir === "desc" ? -1 : 1;
    return rows.slice().sort(function (left, right) {
      var a = String(left[field] || "");
      var b = String(right[field] || "");
      var compare = a < b ? -1 : a > b ? 1 : 0;
      if (compare) return compare * direction;
      return String(left.slug || "") < String(right.slug || "") ? -1 : String(left.slug || "") > String(right.slug || "") ? 1 : 0;
    });
  }

  function paginate(rows, page, size) {
    return rows.slice((page - 1) * size, page * size);
  }

  function nextSort(state, key) {
    if (state.sortKey === key) {
      return { sortKey: key, dir: state.dir === "asc" ? "desc" : "asc" };
    }
    return { sortKey: key, dir: "asc" };
  }

  function defaultState() {
    return { query: "", sortKey: "createdAt", dir: "desc", page: 1 };
  }

  function reduce(state, action) {
    switch (action.type) {
      case "setQuery":
        return { query: action.query, sortKey: state.sortKey, dir: state.dir, page: 1 };
      case "setSort": {
        var sort = nextSort(state, action.key);
        return { query: state.query, sortKey: sort.sortKey, dir: sort.dir, page: 1 };
      }
      case "setPage":
        return { query: state.query, sortKey: state.sortKey, dir: state.dir, page: action.page };
      case "clear":
        return defaultState();
      default:
        return state;
    }
  }

  function computeView(rows, state) {
    var inputCount = rows.length;
    var filtered = filterSites(rows, state.query);
    var sorted = sortRows(filtered, state.sortKey, state.dir);
    var pageCount = Math.max(1, Math.ceil(sorted.length / 10));
    var page = Math.max(1, Math.min(state.page, pageCount));
    var visible = paginate(sorted, page, 10);
    var rangeFrom = sorted.length ? (page - 1) * 10 + 1 : 0;
    var rangeTo = sorted.length ? rangeFrom + visible.length - 1 : 0;
    return {
      rows: visible,
      showControls: inputCount > 0,
      empty: inputCount === 0,
      noMatch: inputCount > 0 && sorted.length === 0,
      showPager: sorted.length > 10,
      page: page,
      pageCount: pageCount,
      rangeFrom: rangeFrom,
      rangeTo: rangeTo,
      sortKey: state.sortKey,
      dir: state.dir,
    };
  }

  globalThis.SitesLanding = { filterSites: filterSites, sortRows: sortRows, paginate: paginate, nextSort: nextSort, defaultState: defaultState, reduce: reduce, computeView: computeView };

  function initController() {}
  if (typeof document !== "undefined") {
    document.addEventListener("DOMContentLoaded", function () { initController(); });
  }
}());
