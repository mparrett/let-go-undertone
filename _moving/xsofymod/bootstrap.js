// Lazy loader and mute control for xsofy's audio module. Injected into the
// bundle's index.html; everything else (the ~2.9 MB wasm and its wasm_exec.js)
// is fetched only once the game emits its first audio frame AND sound is on.
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
//
// The mute control lives here rather than in the shell for the same reason the
// module does: the audio lane must stay addable and removable without touching
// xsofy/tools/xsofy-shell.html (ADR-010/012). It renders into the shell's own
// bar when that bar exists and falls back to a corner button when it does not,
// so the tuner and any bare host page get a control too.
(function () {
  var KEY = 'xsofy.audio.muted';
  var loading = false, sawFrame = false, btn = null;

  function stored() {
    try { return localStorage.getItem(KEY) === '1'; } catch (e) { return false; }
  }
  function remember(v) {
    try { localStorage.setItem(KEY, v ? '1' : '0'); } catch (e) {}
  }

  // The module reads this on start and the setter below drives it after that,
  // so mute survives both orders: toggled before the wasm loads, or after.
  var api = window.XsofyAudio = { muted: stored() };

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

  api.setMuted = function (v) {
    v = !!v;
    api.muted = v;
    remember(v);
    paint();
    if (window.XsofyAudioSetMuted) window.XsofyAudioSetMuted(v);
    // Unmuting is also the trigger to fetch: a player who starts muted never
    // downloads the module at all.
    if (!v && sawFrame) boot();
  };

  function paint() {
    if (!btn) return;
    var off = api.muted;
    btn.setAttribute('aria-pressed', off ? 'true' : 'false');
    btn.title = off ? 'Unmute sound' : 'Mute sound';
    btn.setAttribute('aria-label', btn.title);
    btn.firstChild.style.textDecoration = off ? 'line-through' : 'none';
    btn.style.opacity = off ? '0.45' : '1';
  }

  function mount() {
    if (btn) return;
    var css = document.createElement('style');
    // Theme variables come from the shell when it is present; the fallbacks
    // keep the control legible on a bare page that defines none of them.
    css.textContent =
      '#xs-audio-toggle{display:inline-flex;align-items:center;justify-content:center;' +
      'min-width:2.2rem;min-height:2.2rem;padding:0 .55rem;cursor:pointer;' +
      'background:transparent;color:var(--bar-dim,#9aa0a6);font-size:15px;line-height:1;' +
      'border:1px solid var(--bar-rule,#3a3a3a);border-radius:5px;' +
      '-webkit-tap-highlight-color:transparent;}' +
      '#xs-audio-toggle[aria-pressed="false"]{color:var(--bar-accent,#f5b041);}' +
      '#xs-audio-toggle.floating{position:fixed;top:calc(.5rem + env(safe-area-inset-top,0px));' +
      'right:calc(.5rem + env(safe-area-inset-right,0px));z-index:40;' +
      'background:var(--bar-bg,rgba(20,20,20,.85));}';
    document.head.appendChild(css);

    btn = document.createElement('button');
    btn.id = 'xs-audio-toggle';
    btn.type = 'button';
    btn.appendChild(document.createElement('span')).textContent = '♪';
    btn.addEventListener('click', function (e) {
      e.preventDefault();
      api.setMuted(!api.muted);
    });

    // The shell's right-hand control row is visible in every pane and
    // orientation, which is the whole point: a sound control you can see is
    // how a player learns the game has sound at all.
    var row = document.querySelector('#xsofy-shell .debug-row');
    var sw = document.getElementById('xs-layer-switch');
    if (row && sw) row.insertBefore(btn, sw);
    else if (row) row.appendChild(btn);
    else { btn.className = 'floating'; document.body.appendChild(btn); }
    paint();
  }

  if (document.readyState === 'loading') {
    document.addEventListener('DOMContentLoaded', mount, { once: true });
  } else {
    mount();
  }

  // The module installs its own 'xsofy/audio' listener once it runs; this one
  // exists only to notice the first frame and then get out of the way.
  window.addEventListener('xsofy/audio', function () {
    sawFrame = true;
    if (!api.muted) boot();
  }, { once: true });
})();
