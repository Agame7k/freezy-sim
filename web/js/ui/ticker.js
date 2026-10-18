// Footer ticker showing the most recent event worth a glance. The arena's own console lines (timing warnings and the
// like) stay in the Log and Arena panels unless they are errors, so the footer isn't a stream of orange noise.
import { h, replace } from "../core/dom.js";
import { onEvent } from "../core/store.js";

export function mountTicker(host) {
  onEvent((event) => {
    if (event.level === "debug") return;
    if (event.source === "arena" && event.level !== "error") return;
    if (event.level === "info" && /^[\w.]+(\[\d+\])? = /.test(event.msg)) return; // field I/O value changes ("ftaReady[0] = true")
    replace(host,
      h("span.t", new Date(event.time).toLocaleTimeString([], { hour12: false })),
      h("span.src", event.source),
      h("span", { class: "lvl-" + event.level }, event.msg));
  });
}
