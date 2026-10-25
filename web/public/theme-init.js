(function () {
  try {
    var stored = localStorage.getItem("commentloop:theme");
    var dark = window.matchMedia("(prefers-color-scheme: dark)").matches;
    var theme = stored === "light" || stored === "dark" ? stored : dark ? "dark" : "light";
    document.documentElement.dataset.theme = theme;
    document.documentElement.style.colorScheme = theme;
    var link = document.getElementById("commentloop-highlight");
    if (link) link.href = "/api/highlight.css?theme=" + theme;
  } catch (e) {
    document.documentElement.dataset.theme = "light";
  }
})();
