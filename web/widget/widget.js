/*!
 * FreeSupp embed widget.
 *
 * Usage: <script src="https://support.example.com/widget.js" defer></script>
 *
 * Injects a bubble button that toggles an iframe panel. Every style is applied
 * inline with !important and every element lives in one namespaced container,
 * so the host page's CSS cannot reach in and this cannot leak out.
 */
(function () {
  'use strict';

  var FLAG = '__freesuppWidget';
  if (window[FLAG]) return;
  window[FLAG] = true;

  // Just below the 32-bit signed max, leaving room for host-page overlays.
  var Z_INDEX = '2147483000';
  var ACCENT = '#2563eb';
  var PANEL_W = 380;
  var PANEL_H = 560;
  var ANIM_MS = 180;

  var script = currentScript();
  var base = resolveBase(script);
  var panelURL = base + '/widget/';
  var panelOrigin = originOf(panelURL);

  var isOpen = false;
  var loaded = false;
  var hideTimer = null;
  var root, panel, frame, button, iconOpen, iconClose;

  onReady(init);

  function init() {
    root = element('div');
    root.setAttribute('data-freesupp', 'root');
    style(root, {
      position: 'fixed',
      bottom: '0',
      right: '0',
      width: '0',
      height: '0',
      margin: '0',
      padding: '0',
      'z-index': Z_INDEX,
      direction: 'ltr',
      'color-scheme': 'light'
    });

    panel = element('div');
    panel.setAttribute('data-freesupp', 'panel');
    style(panel, {
      position: 'absolute',
      bottom: '84px',
      right: '20px',
      display: 'none',
      overflow: 'hidden',
      'border-radius': '14px',
      background: '#ffffff',
      'box-shadow': '0 12px 40px rgba(15, 23, 42, .22)',
      opacity: '0',
      transform: 'translateY(12px) scale(.98)',
      'transform-origin': 'bottom right',
      transition: 'opacity ' + ANIM_MS + 'ms ease, transform ' + ANIM_MS + 'ms ease',
      'pointer-events': 'none'
    });

    frame = element('iframe');
    frame.setAttribute('title', 'Support');
    frame.setAttribute('frameborder', '0');
    style(frame, {
      display: 'block',
      width: '100%',
      height: '100%',
      border: '0',
      background: '#ffffff'
    });
    panel.appendChild(frame);

    button = element('button');
    button.type = 'button';
    button.setAttribute('data-freesupp', 'button');
    button.setAttribute('aria-label', label('data-label', 'Contact support'));
    button.setAttribute('aria-expanded', 'false');
    style(button, {
      position: 'absolute',
      bottom: '20px',
      right: '20px',
      width: '56px',
      height: '56px',
      margin: '0',
      padding: '0',
      border: '0',
      'border-radius': '50%',
      background: label('data-color', ACCENT),
      color: '#ffffff',
      cursor: 'pointer',
      display: 'flex',
      'align-items': 'center',
      'justify-content': 'center',
      'box-shadow': '0 6px 20px rgba(15, 23, 42, .28)',
      transition: 'transform ' + ANIM_MS + 'ms ease',
      '-webkit-appearance': 'none',
      appearance: 'none',
      outline: 'none'
    });

    iconOpen = icon('M4 4h16v10H7l-3 3V4z');
    iconClose = icon('M6 6l12 12M18 6L6 18');
    style(iconClose, { display: 'none' });
    button.appendChild(iconOpen);
    button.appendChild(iconClose);

    button.addEventListener('click', toggle);
    document.addEventListener('keydown', function (e) {
      if (isOpen && (e.key === 'Escape' || e.keyCode === 27)) toggle();
    });
    window.addEventListener('resize', size);
    // The visitor app asks to be closed after a successful submission.
    window.addEventListener('message', function (e) {
      if (e.origin !== panelOrigin || !e.data || e.data.source !== 'freesupp') return;
      if (e.data.type === 'close' && isOpen) toggle();
    });

    root.appendChild(panel);
    root.appendChild(button);
    size();
    document.body.appendChild(root);
  }

  function toggle() {
    isOpen = !isOpen;
    button.setAttribute('aria-expanded', isOpen ? 'true' : 'false');
    style(iconOpen, { display: isOpen ? 'none' : 'block' });
    style(iconClose, { display: isOpen ? 'block' : 'none' });

    if (hideTimer) {
      clearTimeout(hideTimer);
      hideTimer = null;
    }

    if (isOpen) {
      if (!loaded) {
        loaded = true;
        frame.src = panelURL;
      }
      size();
      style(panel, { display: 'block', 'pointer-events': 'auto' });
      // Force a reflow so the transition runs from the hidden state.
      void panel.offsetHeight;
      style(panel, { opacity: '1', transform: 'translateY(0) scale(1)' });
      return;
    }

    style(panel, {
      opacity: '0',
      transform: 'translateY(12px) scale(.98)',
      'pointer-events': 'none'
    });
    hideTimer = setTimeout(function () {
      style(panel, { display: 'none' });
      hideTimer = null;
    }, ANIM_MS);
  }

  // size keeps the panel inside small viewports; media queries are unavailable
  // to inline styles.
  function size() {
    var w = Math.min(PANEL_W, Math.max(280, window.innerWidth - 40));
    var h = Math.min(PANEL_H, Math.max(320, window.innerHeight - 120));
    style(panel, { width: w + 'px', height: h + 'px' });
  }

  function icon(path) {
    var svg = document.createElementNS('http://www.w3.org/2000/svg', 'svg');
    svg.setAttribute('viewBox', '0 0 24 24');
    svg.setAttribute('width', '26');
    svg.setAttribute('height', '26');
    svg.setAttribute('fill', 'none');
    svg.setAttribute('stroke', 'currentColor');
    svg.setAttribute('stroke-width', '2');
    svg.setAttribute('stroke-linecap', 'round');
    svg.setAttribute('stroke-linejoin', 'round');
    svg.setAttribute('aria-hidden', 'true');
    var p = document.createElementNS('http://www.w3.org/2000/svg', 'path');
    p.setAttribute('d', path);
    svg.appendChild(p);
    style(svg, { display: 'block', 'pointer-events': 'none' });
    return svg;
  }

  function element(tag) {
    return document.createElement(tag);
  }

  // style applies declarations as !important so host-page rules cannot win.
  function style(el, decls) {
    for (var key in decls) {
      if (Object.prototype.hasOwnProperty.call(decls, key)) {
        el.style.setProperty(key, decls[key], 'important');
      }
    }
  }

  function label(attr, fallback) {
    var v = script && script.getAttribute && script.getAttribute(attr);
    return v || fallback;
  }

  function currentScript() {
    if (document.currentScript) return document.currentScript;
    var all = document.getElementsByTagName('script');
    return all[all.length - 1];
  }

  // resolveBase derives the deployment URL from this script's own src, so the
  // embed snippet stays a single tag with no configuration.
  function resolveBase(s) {
    var override = s && s.getAttribute && s.getAttribute('data-base-url');
    var src = override || (s && s.src) || '';
    var a = document.createElement('a');
    a.href = src;
    var url = a.href.split('#')[0].split('?')[0];
    if (!override) url = url.replace(/\/widget\.js$/, '');
    return url.replace(/\/+$/, '');
  }

  function originOf(url) {
    var a = document.createElement('a');
    a.href = url;
    return a.protocol + '//' + a.host;
  }

  function onReady(fn) {
    if (document.readyState === 'loading') {
      document.addEventListener('DOMContentLoaded', fn);
      return;
    }
    fn();
  }
})();
