// Separate file because MV3's CSP forbids inline script.
const params = new URLSearchParams(location.search);
document.getElementById("reason").textContent = params.get("reason") || "unknown";
document.getElementById("from").textContent = params.get("from") || "";
