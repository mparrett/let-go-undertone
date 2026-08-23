// Lazy loader for xsofy's audio module. Injected into the bundle's
// index.html; everything else (the ~2.9 MB wasm and its wasm_exec.js) is
// fetched only once the game actually emits its first audio frame.
//
// Lazy on purpose, three reasons:
//   - a player who never starts a run never pays for the download;
//   - the module must load AFTER the game's own Go runtime has booted,
//     because wasm_exec.js defines globalThis.Go and the second definition
//     would otherwise race the first (xsofy's instance already exists by the
//     time a frame is emitted, so redefining the class is harmless then);
//   - the first frame proves the emit path works, so nothing is fetched for a
//     build whose bridge is broken.
//
// Frames that arrive during the fetch are simply dropped: every frame is a
// complete state snapshot rather than a delta, so the next turn re-establishes
// everything. That is also why the module needs no replay of what it missed.
(function () {
  var loading = false;

  function boot() {
    if (loading) return;
    loading = true;
    var s = document.createElement('script');
    s.src = 'xsofy-audio-exec.js';
    s.onload = function () {
      var go = new Go();
      WebAssembly.instantiateStreaming(fetch('xsofy-audio.wasm'), go.importObject)
        .then(function (r) { go.run(r.instance); })
        .catch(function (e) { console.warn('xsofy-audio: load failed', e); });
    };
    s.onerror = function () { console.warn('xsofy-audio: exec shim missing'); };
    document.head.appendChild(s);
  }

  // The module installs its own 'xsofy/audio' listener once it runs; this one
  // exists only to notice the first frame and then get out of the way.
  window.addEventListener('xsofy/audio', boot, { once: true });
})();
