(function() {
  var tag = "v1.17.1-etc.1";
  var tagURL = "https://github.com/diega/go-ethereum-classic/releases/tag/" + tag;
  var obs = new MutationObserver(function() {
    var el = document.querySelector(".md-source__fact--version");
    if (el && el.textContent !== tag) {
      el.textContent = tag;
      var link = document.querySelector("a.md-source");
      if (link) link.href = tagURL;
    }
  });
  document.addEventListener("DOMContentLoaded", function() {
    var src = document.querySelector(".md-source");
    if (src) obs.observe(src, {childList: true, subtree: true});
  });
})();
