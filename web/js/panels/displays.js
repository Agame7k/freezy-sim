// Displays panel: every Freezy Arena display and operator panel a real event uses — open any of them in its own
// native window, see a live wall of them, or open a whole event's worth of screens at once.
import { h, replace, toast } from "../core/dom.js";
import { cmd, on } from "../core/store.js";
import { registerPanel } from "../ui/dock.js";
import { DISPLAYS, displayUrl, openDisplay } from "../core/displays.js";

// A typical event's screens, opened together for "event mode".
const EVENT_SET = ["audience", "ftaMonitor", "matchPlay", "referee", "scoringRed", "scoringBlue", "queueing", "rankings", "announcer"];

registerPanel({
  id: "displays",
  title: "Displays",
  mount(el) {
    const listCard = h("div.card");
    const wallCard = h("div.card");
    el.append(listCard, wallCard);
    const wallSelection = new Set(["audience", "ftaMonitor", "as-R1", "as-B1"]);
    try {
      const saved = JSON.parse(localStorage.getItem("freezy-sim.wall") || "null");
      if (Array.isArray(saved)) { wallSelection.clear(); saved.forEach((id) => wallSelection.add(id)); }
    } catch { /* ignore */ }

    function saveWall() {
      try { localStorage.setItem("freezy-sim.wall", JSON.stringify([...wallSelection])); } catch { /* ignore */ }
    }

    function renderList() {
      const groups = { display: "Displays (register with the arena)", panel: "Operator panels" };
      replace(listCard,
        h("h3", "Every screen at the event", h("span.grow"),
          h("button.btn.sm.primary", {
            title: "Open the screens a real event runs, each in its own window",
            onclick: async () => {
              for (const id of EVENT_SET) {
                await openDisplay(id);
                await new Promise((resolve) => setTimeout(resolve, 250));
              }
              toast(`Opened ${EVENT_SET.length} event screens`);
            },
          }, "Open event screens")),
        Object.entries(groups).map(([kind, title]) => h("div", { style: { marginBottom: "8px" } },
          h("div.small.muted", title),
          h("table.tbl",
            DISPLAYS.filter((d) => d.kind === kind).map((d) => h("tr",
              h("td", h("label.check", h("input", {
                type: "checkbox", checked: wallSelection.has(d.id), title: "Show on the display wall",
                onchange: (e) => { e.target.checked ? wallSelection.add(d.id) : wallSelection.delete(d.id); saveWall(); renderWall(); },
              }), d.title)),
              h("td.small.muted", d.where),
              h("td", { style: { whiteSpace: "nowrap" } },
                h("button.btn.sm", { onclick: () => openDisplay(d.id), title: "Open in its own window" }, "⧉ Open"),
                h("a.btn.sm.ghost", { href: displayUrl(d), target: "_blank", title: "Open in the default browser" }, "↗")),
            ))))));
    }

    function renderWall() {
      const tiles = DISPLAYS.filter((d) => wallSelection.has(d.id));
      replace(wallCard,
        h("h3", "Display wall", h("span.grow"), h("span.small.muted", `${tiles.length} live`)),
        tiles.length === 0 ? h("div.hint", "Tick displays above to put them on the wall.") : h("div", {
          style: { display: "grid", gridTemplateColumns: "repeat(auto-fill, minmax(320px, 1fr))", gap: "8px" },
        }, tiles.map((d) => h("div", { style: { border: "1px solid var(--line)", borderRadius: "6px", overflow: "hidden", background: "#000" } },
          h("div.row.small", { style: { padding: "3px 6px", background: "var(--panel)" } }, h("b", d.title), h("span.grow"),
            h("button.btn.sm.ghost", { onclick: () => openDisplay(d.id) }, "⧉")),
          h("div", { style: { position: "relative", width: "100%", aspectRatio: "16 / 9", overflow: "hidden" } },
            h("iframe", {
              src: displayUrl(d), loading: "lazy", allow: "autoplay",
              style: { position: "absolute", left: 0, top: 0, width: "1280px", height: "720px", border: 0,
                transformOrigin: "0 0", transform: "scale(var(--wall-scale, 0.25))" },
            }))))));
      // Scale the 1280×720 pages to the tile width.
      requestAnimationFrame(() => {
        for (const frame of wallCard.querySelectorAll("iframe")) {
          const width = frame.parentElement.clientWidth;
          frame.style.setProperty("--wall-scale", String(width / 1280));
        }
      });
    }

    new ResizeObserver(() => {
      for (const frame of wallCard.querySelectorAll("iframe")) {
        frame.style.setProperty("--wall-scale", String(frame.parentElement.clientWidth / 1280));
      }
    }).observe(wallCard);

    // Display URLs depend on the arena address in sim.info.
    let rendered = false;
    on("sim.info", () => {
      if (rendered) return;
      rendered = true;
      renderList();
      renderWall();
    });
  },
});
