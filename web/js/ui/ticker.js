// Footer ticker showing the most recent event.
import { h, replace } from "../core/dom.js";
import { onEvent } from "../core/store.js";

export function mountTicker(host) {
  onEvent((event) => {
    if (event.level === "debug") return;
    replace(host,
      h("span", new Date(event.time).toLocaleTimeString([], { hour12: false })),
      h("span", { style: { color: "var(--accent)" } }, event.source),
      h("span", { class: "lvl-" + event.level }, event.msg));
  });
}
