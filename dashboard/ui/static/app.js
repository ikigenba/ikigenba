// dashboard scripts — keeps the logged-in index's live-grants block fresh.
//
// The block carries data-stream (an SSE endpoint) and data-fragment (the
// grants-list HTML partial). We open an EventSource on the stream; on each
// "chains" event we re-fetch the fragment and swap it into the block, so token
// issuance / refresh / revocation reflect without a page reload.
(() => {
  const block = document.getElementById("grants-block");
  if (!block || !("EventSource" in window)) return;

  const stream = block.dataset.stream;
  const fragURL = block.dataset.fragment;
  if (!stream || !fragURL) return;

  const es = new EventSource(stream);
  es.addEventListener("chains", async () => {
    try {
      const res = await fetch(fragURL, { credentials: "same-origin" });
      if (res.ok) {
        block.innerHTML = await res.text();
      }
    } catch (_) {
      // Leave the stale block in place; the next event will try again.
    }
  });
})();
