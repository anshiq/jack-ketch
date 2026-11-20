(function () {
  if (window.__jackKetchConsole) return;
  window.__jackKetchConsole = true;
  var endpoint = '/__jack-ketch/console';
  var levels = { error: true, warn: __JK_WARN__ };
  var queue = [];
  var timer = null;
  var sent = 0;
  var windowStart = Date.now();
  var seen = {};

  function text(v) {
    if (v instanceof Error) return (v.name || 'Error') + ': ' + v.message;
    if (typeof v === 'string') return v;
    try {
      return JSON.stringify(v, function (_k, x) {
        if (x instanceof Node) return '<' + (x.nodeName || 'node').toLowerCase() + '>';
        return x;
      });
    } catch (e) {
      return String(v);
    }
  }

  function stackOf(args) {
    for (var i = 0; i < args.length; i++) {
      if (args[i] instanceof Error && args[i].stack) return String(args[i].stack);
    }
    return '';
  }

  function flush() {
    timer = null;
    if (!queue.length) return;
    var batch = queue.splice(0, queue.length);
    var body = JSON.stringify(batch);
    try {
      if (navigator.sendBeacon && navigator.sendBeacon(endpoint, new Blob([body], { type: 'application/json' }))) return;
    } catch (e) {}
    try {
      origFetch(endpoint, { method: 'POST', body: body, headers: { 'Content-Type': 'application/json' }, keepalive: true, credentials: 'same-origin' });
    } catch (e) {}
  }

  function push(entry) {
    var now = Date.now();
    if (now - windowStart > 1000) {
      windowStart = now;
      sent = 0;
    }
    if (++sent > 40) return;
    var key = entry.level + '|' + entry.message + '|' + (entry.source || '');
    if (seen[key] && now - seen[key] < 2000) return;
    seen[key] = now;
    entry.page = location.pathname + location.search;
    entry.ts = new Date(now).toISOString();
    queue.push(entry);
    if (!timer) timer = setTimeout(flush, 150);
  }

  ['error', 'warn'].forEach(function (level) {
    var orig = console[level];
    console[level] = function () {
      try {
        if (levels[level]) {
          var args = Array.prototype.slice.call(arguments);
          push({ kind: 'console', level: level, message: args.map(text).join(' ').slice(0, 4000), stack: stackOf(args).slice(0, 8000) });
        }
      } catch (e) {}
      return orig.apply(console, arguments);
    };
  });

  window.addEventListener('error', function (ev) {
    if (ev.target && ev.target !== window && (ev.target.src || ev.target.href)) {
      push({ kind: 'resource', level: 'error', message: 'failed to load ' + (ev.target.tagName || 'resource').toLowerCase() + ' ' + (ev.target.src || ev.target.href) });
      return;
    }
    var err = ev.error;
    push({
      kind: 'exception',
      level: 'error',
      message: err ? text(err) : String(ev.message || 'Script error'),
      stack: err && err.stack ? String(err.stack).slice(0, 8000) : '',
      source: ev.filename ? ev.filename + ':' + ev.lineno + ':' + ev.colno : ''
    });
  }, true);

  window.addEventListener('unhandledrejection', function (ev) {
    var r = ev.reason;
    push({ kind: 'rejection', level: 'error', message: 'Unhandled promise rejection: ' + text(r), stack: r && r.stack ? String(r.stack).slice(0, 8000) : '' });
  });

  var origFetch = window.fetch ? window.fetch.bind(window) : null;
  if (origFetch) {
    window.fetch = function (input, init) {
      var url = typeof input === 'string' ? input : input && input.url;
      var method = (init && init.method) || (input && input.method) || 'GET';
      return origFetch(input, init).then(function (res) {
        if (res.status >= 500 && String(url).indexOf(endpoint) < 0) push({ kind: 'network', level: 'error', message: method + ' ' + url + ' -> ' + res.status + ' ' + res.statusText });
        return res;
      }, function (err) {
        if (String(url).indexOf(endpoint) < 0) push({ kind: 'network', level: 'error', message: method + ' ' + url + ' failed: ' + text(err) });
        throw err;
      });
    };
  }

  var XHR = window.XMLHttpRequest;
  if (XHR) {
    var open = XHR.prototype.open;
    var send = XHR.prototype.send;
    XHR.prototype.open = function (m, u) {
      this.__jk = { m: m, u: u };
      return open.apply(this, arguments);
    };
    XHR.prototype.send = function () {
      var x = this;
      x.addEventListener('loadend', function () {
        if (!x.__jk) return;
        if (x.status >= 500) push({ kind: 'network', level: 'error', message: x.__jk.m + ' ' + x.__jk.u + ' -> ' + x.status + ' ' + x.statusText });
        else if (x.status === 0) push({ kind: 'network', level: 'error', message: x.__jk.m + ' ' + x.__jk.u + ' failed (network error)' });
      });
      return send.apply(this, arguments);
    };
  }

  window.addEventListener('pagehide', flush);
  document.addEventListener('visibilitychange', function () {
    if (document.visibilityState === 'hidden') flush();
  });
})();
