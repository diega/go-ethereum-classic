(function() {
  var repo = "https://github.com/diega/go-ethereum-classic/releases/tag/";
  setInterval(function() {
    var m = location.pathname.match(/\/(\d+\.\d+\.\d+)\//);
    if (!m) return;
    var tagURL = repo + "v" + m[1] + "-etc.1";
    document.querySelectorAll("a.md-source").forEach(function(link) {
      if (link.href !== tagURL) link.href = tagURL;
    });
  }, 500);
})();
