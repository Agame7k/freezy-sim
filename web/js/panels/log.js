// Unified event log with level/source filters and text search.
import { h, replace } from "../core/dom.js";
import { onEvent, recentEvents } from "../core/store.js";
import { registerPanel } from "../ui/dock.js";

registerPanel({
  id: "log",
  title: "Log",
  mount(el) {
    const search = h("input", { type: "text", placeholder: "Filter text…", style: { flex: "1" } });
    const level = h("select", h("option", { value: "" }, "All levels"), h("option", { value: "warn" }, "Warn+"),
      h("option", { value: "error" }, "Errors"));
    const source = h("select", h("option", { value: "" }, "All sources"));
    const autoscroll = h("input", { type: "checkbox", checked: true });
    const box = h("div.log", { style: { height: "calc(100vh - 220px)" } });
    const sources = new Set();

    el.append(
      h("div.row", { style: { marginBottom: "8px" } }, search, level, source,
        h("label.check", autoscroll, "Follow"),
        h("button.btn.sm", { onclick: () => { replace(box); } }, "Clear")),
      box,
    );

    const rank = { debug: 0, info: 1, warn: 2, error: 3 };
    function visible(event) {
      if (level.value && rank[event.level] < rank[level.value]) return false;
      if (source.value && event.source !== source.value) return false;
      const q = search.value.trim().toLowerCase();
      return !q || event.msg.toLowerCase().includes(q) || event.source.includes(q);
    }
    function add(event) {
      if (!sources.has(event.source)) {
        sources.add(event.source);
        source.appendChild(h("option", { value: event.source }, event.source));
      }
      if (!visible(event)) return;
      box.appendChild(h("div.line", { class: "lvl-" + event.level },
        h("span.t", new Date(event.time).toLocaleTimeString([], { hour12: false })),
        h("span.s", event.source), h("span.m", event.msg)));
      while (box.childElementCount > 3000) box.firstChild.remove();
      if (autoscroll.checked) box.scrollTop = box.scrollHeight;
    }
    function rebuild() {
      replace(box);
      recentEvents().forEach(add);
    }
    for (const control of [search, level, source]) control.addEventListener("input", rebuild);
    rebuild();
    onEvent(add);
  },
});
