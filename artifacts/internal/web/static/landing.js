(function () {
  "use strict";

  function filteredRows(rows, query, visibility) {
    var needle = String(query || "").toLowerCase();
    return rows.filter(function (row) {
      var matchesText = !needle || String(row.filename || "").toLowerCase().includes(needle) || String(row.description || "").toLowerCase().includes(needle);
      var matchesVisibility = visibility === "all" || row.visibility === visibility;
      return matchesText && matchesVisibility;
    });
  }

  function sortedRows(rows, key, direction) {
    var field = key === "size" ? "sizeBytes" : key === "created" ? "createdAtSort" : key;
    var numeric = key === "size" || key === "downloads";
    var multiplier = direction === "desc" ? -1 : 1;
    return rows.slice().sort(function (left, right) {
      var a = numeric ? Number(left[field]) : String(left[field] || "").toLowerCase();
      var b = numeric ? Number(right[field]) : String(right[field] || "").toLowerCase();
      var comparison = a < b ? -1 : a > b ? 1 : 0;
      if (comparison) return comparison * multiplier;
      return String(left.id).localeCompare(String(right.id));
    });
  }

  function init() {
    var rows = JSON.parse(document.querySelector("#artifacts-data").textContent);
    var body = document.querySelector(".artifact-table tbody");
    var query = document.querySelector("#artifact-filter");
    var visibility = document.querySelector("#visibility-filter");
    var controls = document.querySelector(".controls");
    var empty = document.querySelector(".empty-state");
    var noMatch = document.querySelector(".no-match");
    var headers = document.querySelectorAll("button[data-sort-key]");
    var state = { key: "created", direction: "desc" };

    function cell(label, value) {
      var element = document.createElement("td");
      element.dataset.label = label;
      element.textContent = value;
      return element;
    }

    function render() {
      var visible = sortedRows(filteredRows(rows, query.value, visibility.value), state.key, state.direction);
      body.replaceChildren();
      visible.forEach(function (row) {
        var tr = document.createElement("tr");
        tr.dataset.artifactId = row.id;
        var filename = cell("Filename", "");
        var anchor = document.createElement("a");
        anchor.href = row.url;
        anchor.textContent = row.filename;
        filename.appendChild(anchor);
        tr.append(filename, cell("Description", row.description), cell("Visibility", row.visibility), cell("Size", row.size), cell("Created", row.createdAt), cell("Creator", row.createdBy), cell("Downloads", String(row.downloads)));
        body.appendChild(tr);
      });
      controls.toggleAttribute("hidden", rows.length === 0);
      empty.toggleAttribute("hidden", rows.length !== 0);
      noMatch.toggleAttribute("hidden", rows.length === 0 || visible.length !== 0);
      headers.forEach(function (header) {
        if (header.dataset.sortKey === state.key) header.setAttribute("aria-sort", state.direction === "asc" ? "ascending" : "descending");
        else header.removeAttribute("aria-sort");
      });
    }

    query.addEventListener("input", render);
    visibility.addEventListener("change", render);
    headers.forEach(function (header) {
      header.addEventListener("click", function () {
        var key = header.dataset.sortKey;
        if (state.key === key) state.direction = state.direction === "desc" ? "asc" : "desc";
        else {
          state.key = key;
          state.direction = key === "filename" ? "asc" : "desc";
        }
        render();
      });
    });
    render();
  }

  document.addEventListener("DOMContentLoaded", init);
}());
