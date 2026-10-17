// Front-end entry point: connects to the simulator, builds the match bar, dock panels and 3D venue.
import { connect, onEvent } from "./core/store.js";
import { toast } from "./core/dom.js";
import { mountMatchbar } from "./ui/matchbar.js";
import { mountDock, setupResizer } from "./ui/dock.js";
import { mountTicker } from "./ui/ticker.js";

// Panels register themselves with the dock on import (tab order follows import order).
import "./panels/match.js";
import "./panels/ds.js";
import "./panels/fieldio.js";
import "./panels/robots.js";
import "./panels/ai.js";
import "./panels/officials.js";
import "./panels/chaos.js";
import "./panels/reports.js";
import "./panels/network.js";
import "./panels/inspector.js";
import "./panels/displays.js";
import "./panels/replay.js";
import "./panels/arena.js";
import "./panels/log.js";

const params = new URLSearchParams(location.search);
const standalonePanel = params.get("panel");

connect();
// Surface rejected arena actions (e.g. Start while a blocker is active) instead of failing silently.
let lastRejection = 0;
onEvent((event) => {
  if (event.source === "arena" && event.msg.startsWith("Arena rejected command") && Date.now() - new Date(event.time) < 5000 &&
    Date.now() - lastRejection > 500) {
    lastRejection = Date.now();
    toast(event.msg.replace("Arena rejected command: ", "Arena: "), "error", 6000);
  }
});
mountMatchbar(document.getElementById("matchbar"));

if (standalonePanel) {
  document.body.classList.add("standalone");
  mountDock(document.getElementById("dock"), { standalonePanel });
} else {
  mountDock(document.getElementById("dock"));
  setupResizer(document.getElementById("dock-resizer"), document.getElementById("workspace"));
  mountTicker(document.getElementById("ticker"));
  import("./scene/venue.js")
    .then(({ createVenue }) => createVenue({
      host: document.getElementById("scene-host"),
      cssHost: document.getElementById("css3d-host"),
      hud: document.getElementById("hud"),
    }))
    .catch((err) => {
      console.error(err);
      document.getElementById("scene-host").innerHTML =
        `<div class="no-webgl">3D view failed to start: ${err.message}</div>`;
    });
}
