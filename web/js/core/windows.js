// Opening extra windows. On the simulator PC a page asks the simulator for a native window; on another device
// (remote access over Tailscale or the LAN) a native window would pop up on the simulator PC instead, so it opens a
// browser tab on the device.
import { cmd } from "./store.js";
import { toast } from "./dom.js";

const LOOPBACK = new Set(["127.0.0.1", "localhost", "[::1]", "::1"]);

// isLocalPage reports whether this page was loaded on the simulator PC itself.
export function isLocalPage() {
  return LOOPBACK.has(location.hostname);
}

export async function openWindow(url, title, width, height) {
  if (!isLocalPage()) {
    window.open(url, "_blank");
    return;
  }
  try {
    const result = await cmd("window.open", { url, title, width, height });
    if (!result?.native) window.open(url, "_blank");
  } catch (err) {
    toast(err.message, "error");
  }
}
