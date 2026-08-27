// Tema panel Lamund: terapkan pilihan tersimpan saat load; default ikut OS.
// Dipakai semua layar mockup. Nilai: "light" | "dark" | null(ikut OS).
(function () {
  var saved = null;
  try { saved = localStorage.getItem("lamund-theme"); } catch (e) {}
  if (saved) document.documentElement.setAttribute("data-theme", saved);
})();

window.LamundTheme = {
  set: function (mode) { // "light" | "dark" | "system"
    var el = document.documentElement;
    try {
      if (mode === "system") { localStorage.removeItem("lamund-theme"); el.removeAttribute("data-theme"); }
      else { localStorage.setItem("lamund-theme", mode); el.setAttribute("data-theme", mode); }
    } catch (e) {}
  },
  current: function () {
    return document.documentElement.getAttribute("data-theme") || "system";
  }
};
