(function() {
  var repo = "https://github.com/diega/go-ethereum-classic/releases/tag/";
  setInterval(function() {
    var m = location.pathname.match(/\/(\d+\.\d+\.\d+)\//);
    if (!m) return;
    var tag = "v" + m[1] + "-etc.1";
    var tagURL = repo + tag;
    document.querySelectorAll(".md-source__fact--version").forEach(function(el) {
      if (el.textContent !== tag) el.textContent = tag;
    });
    document.querySelectorAll("a.md-source").forEach(function(link) {
      if (link.href !== tagURL) link.href = tagURL;
    });
  }, 500);
})();
