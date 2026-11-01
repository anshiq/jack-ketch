(function () {
  var root = document.documentElement;

  function flash(btn, text) {
    var prev = btn.textContent;
    btn.textContent = text;
    btn.classList.add("done");
    setTimeout(function () {
      btn.textContent = prev;
      btn.classList.remove("done");
    }, 1400);
  }

  function copy(text, btn) {
    var done = function () { flash(btn, "Copied"); };
    if (navigator.clipboard && navigator.clipboard.writeText) {
      navigator.clipboard.writeText(text).then(done, function () { flash(btn, "Failed"); });
      return;
    }
    var ta = document.createElement("textarea");
    ta.value = text;
    ta.style.position = "fixed";
    ta.style.opacity = "0";
    document.body.appendChild(ta);
    ta.select();
    try { document.execCommand("copy"); done(); } catch (e) { flash(btn, "Failed"); }
    document.body.removeChild(ta);
  }

  document.querySelectorAll("[data-theme-toggle]").forEach(function (b) {
    b.addEventListener("click", function () {
      var next = root.getAttribute("data-theme") === "light" ? "dark" : "light";
      root.setAttribute("data-theme", next);
      try { localStorage.setItem("jk-theme", next); } catch (e) {}
    });
  });

  var menu = document.querySelector("[data-menu-toggle]");
  if (menu) {
    menu.addEventListener("click", function () {
      var open = document.body.classList.toggle("nav-open");
      menu.setAttribute("aria-expanded", open ? "true" : "false");
    });
    document.addEventListener("click", function (e) {
      if (document.body.classList.contains("nav-open") && !e.target.closest(".sidebar") && !e.target.closest("[data-menu-toggle]")) {
        document.body.classList.remove("nav-open");
        menu.setAttribute("aria-expanded", "false");
      }
    });
  }

  document.querySelectorAll(".install").forEach(function (box) {
    var b = box.querySelector("button");
    if (b) b.addEventListener("click", function () { copy(box.getAttribute("data-copy"), b); });
  });

  document.querySelectorAll(".prose .highlight, .prose > pre").forEach(function (block) {
    var pre = block.tagName === "PRE" ? block : block.querySelector("pre");
    if (!pre) return;
    var host = block;
    if (block.tagName === "PRE") {
      host = document.createElement("div");
      host.className = "pre-wrap";
      block.parentNode.insertBefore(host, block);
      host.appendChild(block);
    }
    var b = document.createElement("button");
    b.type = "button";
    b.className = "copy-btn";
    b.textContent = "Copy";
    b.setAttribute("aria-label", "Copy code");
    b.addEventListener("click", function () { copy(pre.innerText.replace(/\n$/, ""), b); });
    host.appendChild(b);
  });

  document.querySelectorAll("[data-tabs]").forEach(function (group) {
    var tabs = Array.prototype.slice.call(group.querySelectorAll(":scope > .tab"));
    if (!tabs.length) return;
    var bar = document.createElement("div");
    bar.className = "tab-bar";
    bar.setAttribute("role", "tablist");
    var key = "jk-tab-" + tabs.map(function (t) { return t.getAttribute("data-tab"); }).join("|");
    var saved = null;
    try { saved = localStorage.getItem(key); } catch (e) {}
    function select(name) {
      tabs.forEach(function (t) { t.classList.toggle("on", t.getAttribute("data-tab") === name); });
      Array.prototype.forEach.call(bar.children, function (b) { b.classList.toggle("on", b.textContent === name); b.setAttribute("aria-selected", b.textContent === name ? "true" : "false"); });
      try { localStorage.setItem(key, name); } catch (e) {}
    }
    tabs.forEach(function (t) {
      var b = document.createElement("button");
      b.type = "button";
      b.setAttribute("role", "tab");
      b.textContent = t.getAttribute("data-tab");
      b.addEventListener("click", function () { select(b.textContent); });
      bar.appendChild(b);
    });
    group.insertBefore(bar, group.firstChild);
    var names = tabs.map(function (t) { return t.getAttribute("data-tab"); });
    select(saved && names.indexOf(saved) > -1 ? saved : names[0]);
  });

  var tocLinks = document.querySelectorAll(".toc a");
  if (tocLinks.length && "IntersectionObserver" in window) {
    var map = {};
    tocLinks.forEach(function (a) { map[decodeURIComponent(a.getAttribute("href").slice(1))] = a; });
    var heads = Array.prototype.slice.call(document.querySelectorAll(".prose h2[id], .prose h3[id]"));
    var current = null;
    var io = new IntersectionObserver(function (entries) {
      entries.forEach(function (en) {
        if (en.isIntersecting) {
          if (current) current.classList.remove("on");
          current = map[en.target.id] || null;
          if (current) current.classList.add("on");
        }
      });
    }, { rootMargin: "-70px 0px -72% 0px", threshold: 0 });
    heads.forEach(function (h) { io.observe(h); });
  }

  var active = document.querySelector(".sb-link.on");
  if (active && active.scrollIntoView) {
    var sc = document.querySelector(".sb-scroll");
    if (sc) sc.scrollTop = Math.max(0, active.offsetTop - sc.clientHeight / 3);
  }

  var modal = document.getElementById("search");
  var input = document.getElementById("search-q");
  var list = document.getElementById("search-results");
  if (!modal || !input || !list) return;

  var docs = null;
  var loading = null;
  var results = [];
  var sel = 0;

  function load() {
    if (docs) return Promise.resolve(docs);
    if (loading) return loading;
    loading = fetch(input.getAttribute("data-index"))
      .then(function (r) { return r.json(); })
      .then(function (d) { docs = d; return d; })
      .catch(function () { docs = []; return docs; });
    return loading;
  }

  function esc(s) {
    return s.replace(/[&<>"]/g, function (c) { return { "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;" }[c]; });
  }

  function terms(q) {
    return q.toLowerCase().split(/\s+/).filter(Boolean);
  }

  function score(d, ts) {
    var title = d.title.toLowerCase();
    var desc = (d.desc || "").toLowerCase();
    var text = d.text.toLowerCase();
    var s = 0;
    for (var i = 0; i < ts.length; i++) {
      var t = ts[i];
      var hit = false;
      if (title.indexOf(t) > -1) { s += title === t ? 40 : 12; hit = true; }
      if (desc.indexOf(t) > -1) { s += 5; hit = true; }
      var at = text.indexOf(t);
      if (at > -1) { s += 2 + Math.min(4, text.split(t).length - 1); hit = true; }
      if (!hit) return 0;
    }
    return s;
  }

  function snippet(d, ts) {
    var low = d.text.toLowerCase();
    var at = -1;
    for (var i = 0; i < ts.length && at < 0; i++) at = low.indexOf(ts[i]);
    if (at < 0) return esc((d.desc || d.text).slice(0, 130));
    var from = Math.max(0, at - 40);
    var raw = d.text.slice(from, from + 150);
    var out = esc(raw);
    ts.forEach(function (t) {
      out = out.replace(new RegExp("(" + t.replace(/[.*+?^${}()|[\]\\]/g, "\\$&") + ")", "ig"), "<mark>$1</mark>");
    });
    return (from > 0 ? "…" : "") + out + "…";
  }

  function render(q) {
    var ts = terms(q);
    if (!ts.length) {
      list.innerHTML = '<li class="search-empty">Type to search guides, reference and agent setup.</li>';
      results = [];
      return;
    }
    results = docs
      .map(function (d) { return { d: d, s: score(d, ts) }; })
      .filter(function (x) { return x.s > 0; })
      .sort(function (a, b) { return b.s - a.s; })
      .slice(0, 12);
    sel = 0;
    if (!results.length) {
      list.innerHTML = '<li class="search-empty">No results for “' + esc(q) + '”.</li>';
      return;
    }
    list.innerHTML = results.map(function (x, i) {
      return '<li><a href="' + x.d.url + '" class="' + (i === 0 ? "sel" : "") + '" role="option"><span class="r-top"><span class="r-title">' + esc(x.d.title) + '</span><span class="r-sec">' + esc(x.d.section) + '</span></span><span class="r-snip">' + snippet(x.d, ts) + "</span></a></li>";
    }).join("");
  }

  function mark() {
    var links = list.querySelectorAll("a");
    links.forEach(function (a, i) { a.classList.toggle("sel", i === sel); });
    if (links[sel]) links[sel].scrollIntoView({ block: "nearest" });
  }

  function open() {
    modal.hidden = false;
    document.body.style.overflow = "hidden";
    input.value = "";
    list.innerHTML = '<li class="search-empty">Loading…</li>';
    load().then(function () { render(input.value); });
    setTimeout(function () { input.focus(); }, 10);
  }

  function close() {
    modal.hidden = true;
    document.body.style.overflow = "";
  }

  document.querySelectorAll("[data-search-open]").forEach(function (b) { b.addEventListener("click", open); });
  modal.querySelectorAll("[data-search-close]").forEach(function (b) { b.addEventListener("click", close); });
  input.addEventListener("input", function () { if (docs) render(input.value); });

  document.addEventListener("keydown", function (e) {
    var typing = /^(input|textarea|select)$/i.test((e.target.tagName || ""));
    if (modal.hidden) {
      if ((e.key === "/" && !typing) || ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === "k")) {
        e.preventDefault();
        open();
      }
      return;
    }
    if (e.key === "Escape") { close(); return; }
    if (e.key === "ArrowDown") { e.preventDefault(); sel = Math.min(results.length - 1, sel + 1); mark(); }
    if (e.key === "ArrowUp") { e.preventDefault(); sel = Math.max(0, sel - 1); mark(); }
    if (e.key === "Enter" && results[sel]) { window.location.href = results[sel].d.url; }
  });
})();
