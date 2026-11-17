// Applies the saved theme before the first paint; CSP forbids inline scripts.
(function () {
  var choice = "system";
  try { choice = localStorage.getItem("fixgw-theme") || "system"; } catch (e) { /* storage unavailable */ }
  var dark = choice === "dark" || (choice !== "light" && window.matchMedia("(prefers-color-scheme: dark)").matches);
  document.documentElement.classList.toggle("dark", dark);
  document.documentElement.style.colorScheme = dark ? "dark" : "light";
})();
