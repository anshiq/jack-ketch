try {
  var p = JSON.parse(localStorage.getItem('ar.prefs.v1') || '{}');
  var t = p.theme || 'system';
  var r = t === 'system' ? (window.matchMedia('(prefers-color-scheme: light)').matches ? 'light' : 'dark') : t;
  document.documentElement.dataset.theme = r;
  if (p.density) document.documentElement.dataset.density = p.density;
} catch (e) {
  document.documentElement.dataset.theme = 'dark';
}
