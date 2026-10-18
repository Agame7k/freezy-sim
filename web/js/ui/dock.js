// Side dock. Panels register here and are shown in four groups, each with its own row of tabs; any panel can also be
// popped out into its own native window.
import { h, replace } from "../core/dom.js";
import { icon } from "../core/icons.js";
import { openWindow } from "../core/windows.js";

const panels = [];
let active = null;
let selectPanel = null;

// Which group each panel is in, in tab order. A panel not listed here goes in the last group.
const GROUPS = [
  { id: "match", title: "Match", panels: ["match", "reports", "officials", "replay"] },
  { id: "field", title: "Field", panels: ["ds", "robots", "fieldio", "network", "displays"] },
  { id: "sim", title: "Simulation", panels: ["ai", "chaos"] },
  { id: "system", title: "System", panels: ["arena", "inspector", "log"] },
];

/** Brings a docked panel to the front (e.g. from a banner that says where to fix something). */
export function showPanel(id) {
  selectPanel?.(id);
}

/** Registers a panel: { id, title, mount(el), badge?(setBadge) } */
export function registerPanel(panel) {
  panels.push(panel);
}

function groupOf(id) {
  return GROUPS.find((group) => group.panels.includes(id)) || GROUPS[GROUPS.length - 1];
}

export function mountDock(host, { standalonePanel = null } = {}) {
  const groupBar = h("div.dock-groups", { role: "tablist", "aria-label": "Panel groups" });
  const tabs = h("div.dock-tabs", { role: "tablist" });
  const body = h("div.dock-body");
  replace(host, groupBar, tabs, body);

  const views = new Map();
  const groupTabs = new Map();
  for (const group of GROUPS) {
    const dot = h("span.group-dot", { hidden: true });
    const tab = h("button.dock-group", { role: "tab", onclick: () => select(lastInGroup[group.id] || firstPanelOf(group)) },
      group.title, dot);
    groupTabs.set(group.id, { tab, dot });
    groupBar.appendChild(tab);
  }
  const lastInGroup = {};
  try { Object.assign(lastInGroup, JSON.parse(localStorage.getItem("freezy-sim.dockGroups") || "{}")); } catch { /* ignore */ }

  const ordered = [...panels].sort((a, b) => order(a.id) - order(b.id));
  for (const panel of ordered) {
    if (standalonePanel && panel.id !== standalonePanel) continue;
    const badge = h("span.badge", { style: { display: "none" } });
    const tab = h("button.dock-tab", { role: "tab", onclick: () => select(panel.id), title: panel.title }, panel.title, badge);
    const popout = h("button.btn.sm.ghost.icon-only", { title: "Pop out into its own window", "aria-label": "Pop out",
      onclick: () => popOut(panel) }, icon("external"));
    const el = h("div.dock-panel", { "data-panel": panel.id, role: "tabpanel" });
    const head = h("div.panel-head", h("h2", panel.title), standalonePanel ? null : popout);
    el.appendChild(head);
    const content = h("div");
    el.appendChild(content);
    panel.mount(content, {
      setBadge(text) {
        badge.textContent = text ?? "";
        badge.style.display = text ? "" : "none";
        refreshGroupDots();
      },
    });
    tabs.appendChild(tab);
    body.appendChild(el);
    views.set(panel.id, { tab, el, badge, group: groupOf(panel.id).id });
  }
  if (standalonePanel) {
    tabs.style.display = "none";
    groupBar.style.display = "none";
  }

  function order(id) {
    const group = GROUPS.indexOf(groupOf(id));
    const index = groupOf(id).panels.indexOf(id);
    return group * 100 + (index < 0 ? 99 : index);
  }
  function firstPanelOf(group) {
    return [...views.keys()].find((id) => views.get(id).group === group.id);
  }
  // A group tab shows a dot when one of its panels has a count badge (blockers, faults), so it is visible from any
  // group. A status mark such as Replay's "●" (recording) is always on, so it doesn't count.
  function refreshGroupDots() {
    for (const [groupId, { dot }] of groupTabs) {
      dot.hidden = ![...views.values()].some((view) => view.group === groupId && view.badge.style.display !== "none" &&
        /\d/.test(view.badge.textContent));
    }
  }

  function select(id) {
    if (!views.has(id)) return;
    active = id;
    const groupId = views.get(id).group;
    lastInGroup[groupId] = id;
    for (const [gid, { tab }] of groupTabs) {
      tab.classList.toggle("active", gid === groupId);
      tab.setAttribute("aria-selected", gid === groupId);
    }
    for (const [panelId, view] of views) {
      view.tab.hidden = view.group !== groupId;
      view.tab.classList.toggle("active", panelId === id);
      view.tab.setAttribute("aria-selected", panelId === id);
      view.el.classList.toggle("active", panelId === id);
    }
    try {
      localStorage.setItem("freezy-sim.dock", id);
      localStorage.setItem("freezy-sim.dockGroups", JSON.stringify(lastInGroup));
    } catch { /* ignore */ }
  }

  let initial = standalonePanel || null;
  if (!initial) {
    try { initial = localStorage.getItem("freezy-sim.dock"); } catch { /* ignore */ }
  }
  if (!views.has(initial)) initial = views.keys().next().value;
  selectPanel = select;
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
