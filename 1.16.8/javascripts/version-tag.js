(function() {
  var repo = "https://github.com/diega/go-ethereum-classic/releases/tag/";
  setInterval(function() {
    var m = location.pathname.match(/\/(\d+\.\d+\.\d+)\//);
    if (!m) return;
    var tag = "v" + m[1] + "-etc.1";
    var el = document.querySelector(".md-source__fact--version");
    if (el && el.textContent !== tag) {
      el.textContent = tag;
      var link = document.querySelector("a.md-source");
      if (link) link.href = repo + tag;
    }
  }, 500);
})();
