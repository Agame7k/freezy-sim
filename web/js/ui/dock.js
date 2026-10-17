// Tabbed side dock. Panels register here; each can also be popped out into its own native window.
import { h, replace } from "../core/dom.js";
import { openWindow } from "../core/windows.js";

const panels = [];
let active = null;

/** Registers a panel: { id, title, mount(el), badge?(setBadge) } */
export function registerPanel(panel) {
  panels.push(panel);
}

export function mountDock(host, { standalonePanel = null } = {}) {
  const tabs = h("div.dock-tabs");
  const body = h("div.dock-body");
  replace(host, tabs, body);

  const views = new Map();
  for (const panel of panels) {
    if (standalonePanel && panel.id !== standalonePanel) continue;
    const badge = h("span.badge", { style: { display: "none" } });
    const tab = h("button.dock-tab", { onclick: () => select(panel.id), title: panel.title }, panel.title, badge);
    const popout = h("button.btn.sm.ghost", { title: "Pop out into its own window", onclick: () => popOut(panel) }, "⧉");
    const el = h("div.dock-panel", { "data-panel": panel.id });
    const head = h("div.panel-head", h("h2", panel.title), standalonePanel ? null : popout);
    el.appendChild(head);
    const content = h("div");
    el.appendChild(content);
    panel.mount(content, {
      setBadge(text) {
        badge.textContent = text ?? "";
        badge.style.display = text ? "" : "none";
      },
    });
    tabs.appendChild(tab);
    body.appendChild(el);
    views.set(panel.id, { tab, el });
  }
  if (standalonePanel) tabs.style.display = "none";

  function select(id) {
    active = id;
    for (const [panelId, view] of views) {
      view.tab.classList.toggle("active", panelId === id);
      view.el.classList.toggle("active", panelId === id);
    }
    try { localStorage.setItem("freezy-sim.dock", id); } catch { /* ignore */ }
  }

  let initial = standalonePanel || null;
  if (!initial) {
    try { initial = localStorage.getItem("freezy-sim.dock"); } catch { /* ignore */ }
  }
  if (!views.has(initial)) initial = views.keys().next().value;
  select(initial);
  return { select };
}

export async function popOut(panel) {
  const url = `${location.origin}/?panel=${encodeURIComponent(panel.id)}`;
  await openWindow(url, `Freezy Sim — ${panel.title}`, 720, 860);
}

export function setupResizer(resizer, workspace) {
  let dragging = false;
  try {
    const saved = localStorage.getItem("freezy-sim.dockWidth");
    if (saved) workspace.style.setProperty("--dock-width", saved);
  } catch { /* ignore */ }
  resizer.addEventListener("mousedown", (e) => {
    dragging = true;
    resizer.classList.add("dragging");
    e.preventDefault();
  });
  window.addEventListener("mousemove", (e) => {
    if (!dragging) return;
    const width = Math.min(Math.max(window.innerWidth - e.clientX, 300), window.innerWidth - 300);
    workspace.style.setProperty("--dock-width", `${width}px`);
    window.dispatchEvent(new Event("resize"));
  });
  window.addEventListener("mouseup", () => {
    if (!dragging) return;
    dragging = false;
    resizer.classList.remove("dragging");
    try {
      localStorage.setItem("freezy-sim.dockWidth", getComputedStyle(workspace).getPropertyValue("--dock-width"));
    } catch { /* ignore */ }
  });
}
