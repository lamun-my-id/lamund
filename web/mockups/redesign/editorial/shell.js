// Terapkan tema tersimpan sedini mungkin (hindari flash).
(function () {
  var t = localStorage.getItem('lamund-theme') || 'editorial';
  if (t !== 'editorial') document.documentElement.setAttribute('data-theme', t);
})();
// Ganti tema (dipakai halaman Pengaturan) + simpan preferensi.
window.lamundSetTheme = function (name) {
  if (name === 'editorial') document.documentElement.removeAttribute('data-theme');
  else document.documentElement.setAttribute('data-theme', name);
  localStorage.setItem('lamund-theme', name);
  document.querySelectorAll('.theme-card').forEach(function (c) {
    c.classList.toggle('on', c.getAttribute('data-theme') === name);
  });
};
// Modal sederhana (dipakai halaman Pengaturan).
window.openM = function (id) { var m = document.getElementById(id); if (m) m.classList.add('show'); };
window.closeM = function (id) { var m = document.getElementById(id); if (m) m.classList.remove('show'); };
document.addEventListener('click', function (e) {
  if (e.target.classList && e.target.classList.contains('modal')) e.target.classList.remove('show');
});
document.addEventListener('keydown', function (e) {
  if (e.key === 'Escape') document.querySelectorAll('.modal.show').forEach(function (m) { m.classList.remove('show'); });
});
// Menyuntikkan sidebar konsisten ke <aside class="side" data-active="...">.
(function () {
  var I = {
    index: '<rect x="3" y="3" width="7" height="7" rx="1.5"/><rect x="14" y="3" width="7" height="7" rx="1.5"/><rect x="3" y="14" width="7" height="7" rx="1.5"/><rect x="14" y="14" width="7" height="7" rx="1.5"/>',
    sites: '<path d="M3 4h18v6H3zM3 14h18v6H3z"/><path d="M7 7h.01M7 17h.01"/>',
    domain: '<circle cx="12" cy="12" r="9"/><path d="M3 12h18M12 3c3 3 3 15 0 18M12 3c-3 3-3 15 0 18"/>',
    certs: '<path d="M12 3l7 3v6c0 4-3 7-7 9-4-2-7-5-7-9V6z"/>',
    users: '<circle cx="9" cy="8" r="3"/><path d="M3 20a6 6 0 0 1 12 0"/><path d="M16 6a3 3 0 0 1 0 6"/>',
    settings: '<circle cx="12" cy="12" r="3"/><path d="M12 2v3M12 19v3M2 12h3M19 12h3"/>',
  };
  function link(k, href, label, cnt, active) {
    return '<a class="' + (k === active ? 'on' : '') + '" href="' + href + '">' +
      '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor">' + I[k] + '</svg> ' + label +
      (cnt ? ' <span class="cnt">' + cnt + '</span>' : '') + '</a>';
  }
  document.querySelectorAll('aside.side[data-active]').forEach(function (el) {
    var a = el.getAttribute('data-active');
    el.innerHTML =
      '<div class="brand"><div class="mk"><b>L</b></div><div><h1>Lamund</h1><small>hosting platform</small></div></div>' +
      '<div class="scope"><span class="dot"></span><div class="sc-t"><b>Personal</b><small>rani@studio.dev</small></div>' +
      '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor"><path d="M8 9l4 4 4-4"/></svg></div>' +
      '<nav class="nav">' +
      '<span class="lbl">Kelola</span>' +
      link('index', 'index.html', 'Overview', '', a) +
      link('sites', 'sites.html', 'Situs', '7', a) +
      link('domain', 'domain.html', 'Domain', '3', a) +
      link('certs', 'certs.html', 'Sertifikat', '', a) +
      '<span class="lbl">Akun</span>' +
      link('users', 'users.html', 'Pengguna', '', a) +
      link('settings', 'settings.html', 'Pengaturan', '', a) +
      '</nav>' +
      '<div class="foot"><div class="ava">A</div><div class="who">Admin<small><a href="login.html">Keluar</a></small></div></div>';
  });
})();
