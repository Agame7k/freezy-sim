// Tiny DOM helpers used by every panel.
import { cmd } from "./store.js";

/** h("div.card#id", {attrs/on*}, ...children) */
export function h(spec, props, ...children) {
  if (props == null || typeof props !== "object" || props instanceof Node || Array.isArray(props)) {
    if (props !== undefined && props !== null) children.unshift(props);
    props = {};
  }
  const [tagPart, ...rest] = spec.split(/(?=[.#])/);
  const el = document.createElement(tagPart || "div");
  for (const token of rest) {
    if (token[0] === ".") el.classList.add(token.slice(1));
    else if (token[0] === "#") el.id = token.slice(1);
  }
  for (const [key, value] of Object.entries(props)) {
    if (value == null || value === false) continue;
    if (key.startsWith("on") && typeof value === "function") el.addEventListener(key.slice(2).toLowerCase(), value);
    else if (key === "class") el.className += " " + value;
    else if (key === "style" && typeof value === "object") Object.assign(el.style, value);
    else if (key === "html") el.innerHTML = value;
    else if (key in el && typeof value !== "string") el[key] = value;
    else el.setAttribute(key, value === true ? "" : value);
  }
  append(el, children);
  return el;
}

function append(el, children) {
  for (const child of children.flat(Infinity)) {
    if (child == null || child === false) continue;
    el.appendChild(child instanceof Node ? child : document.createTextNode(String(child)));
  }
}

export function clear(el) {
  while (el.firstChild) el.removeChild(el.firstChild);
  return el;
}

// Panels re-render several times a second. A click only registers if the press and release land on the same element,
// so a re-render between them silently swallows the click. While the pointer is held down inside an element (or the
// user is typing in / choosing from a control inside it), its re-render is deferred until the interaction ends.
let pressTarget = null;
const deferred = new Map();

function flushDeferred() {
  for (const [el, children] of [...deferred]) {
    if (isBusy(el)) continue;
    deferred.delete(el);
    clear(el);
    append(el, children);
  }
}

function isBusy(el) {
  if (pressTarget && el.contains(pressTarget)) return true;
  const active = document.activeElement;
  if (active && active !== document.body && el.contains(active)) {
    if (active.tagName === "SELECT" || active.tagName === "TEXTAREA") return true;
    if (active.tagName === "INPUT" && !["checkbox", "radio", "button"].includes(active.type)) return true;
  }
  return false;
}

if (typeof window !== "undefined") {
  window.addEventListener("pointerdown", (e) => { pressTarget = e.target; }, true);
  // Flush after the click event (which follows pointerup in the same task) has been dispatched.
  window.addEventListener("pointerup", () => setTimeout(() => { pressTarget = null; flushDeferred(); }, 0), true);
  window.addEventListener("pointercancel", () => { pressTarget = null; flushDeferred(); }, true);
  window.addEventListener("focusout", () => setTimeout(flushDeferred, 0), true);
  window.addEventListener("change", (e) => {
    // A finished dropdown choice should not keep the panel frozen until the user clicks elsewhere.
    if (e.target?.tagName === "SELECT") setTimeout(() => e.target.blur(), 0);
    setTimeout(flushDeferred, 10);
  }, true);
}

export function replace(el, ...children) {
  if (isBusy(el)) {
    deferred.set(el, children);
    return el;
  }
  deferred.delete(el);
  clear(el);
  append(el, children);
  return el;
}

export function led(color, blink = false) {
  return h("span.led", { class: (color || "") + (blink ? " blink" : "") });
}

export function tag(text, kind = "") {
  return h("span.tag", { class: kind }, text);
}

/**
 * Button that runs a simulator command and toasts errors. options.title says what it does (shown on hover);
 * options.confirm ({ title, message, okLabel, danger }) asks first, for actions that can't be undone.
 */
export function cmdButton(label, command, args = {}, cls = "", options = {}) {
  return h("button.btn", {
    class: cls,
    title: options.title,
    onclick: async (e) => {
      const button = e.currentTarget;
      if (options.confirm && !(await confirmAction(options.confirm.title, options.confirm))) return;
      button.disabled = true;
      try {
        await cmd(command, typeof args === "function" ? args() : args);
      } catch (err) {
        toast(err.message, "error");
      } finally {
        button.disabled = false;
      }
    },
  }, label);
}

let toastHost = null;
export function toast(text, kind = "info", ms = 4000) {
  if (!toastHost) {
    toastHost = h("div#toasts");
    document.body.appendChild(toastHost);
  }
  const el = h("div.toast", { class: kind }, text);
  toastHost.appendChild(el);
  setTimeout(() => el.remove(), ms);
}

// In-app dialogs, instead of the browser's prompt() and confirm(), which look out of place in the app window.
function dialog(title, { message = "", input = null, okLabel = "OK", danger = false }) {
  return new Promise((resolve) => {
    const ok = h("button.btn", { class: danger ? "danger" : "primary", onclick: () => close(true) }, okLabel);
    const close = (accepted) => {
      overlay.remove();
      document.removeEventListener("keydown", onKey, true);
      resolve(accepted);
    };
    const onKey = (e) => {
      if (e.key === "Escape") { e.stopPropagation(); close(false); }
      if (e.key === "Enter" && (!input || e.ctrlKey || e.metaKey)) { e.preventDefault(); close(true); }
    };
    const overlay = h("div.dialog-overlay", { onmousedown: (e) => { if (e.target === overlay) close(false); } },
      h("div.dialog", { role: "dialog", "aria-modal": "true", "aria-label": title },
        h("h2", title),
        message ? h("p.dialog-message", message) : null,
        input,
        h("div.dialog-actions", h("button.btn.ghost", { onclick: () => close(false) }, "Cancel"), ok)));
    document.addEventListener("keydown", onKey, true);
    document.body.appendChild(overlay);
    (input || ok).focus();
  });
}

/** Asks for some text; resolves to it, or null when cancelled. */
export async function ask(title, { message = "", placeholder = "", okLabel = "OK" } = {}) {
  const input = h("textarea.dialog-input", { rows: 3, placeholder, "aria-label": title });
  return (await dialog(title, { message, input, okLabel })) ? input.value : null;
}

/** Asks to go ahead with an action; resolves to true or false. danger styles the button for destructive actions. */
export function confirmAction(title, { message = "", okLabel = "OK", danger = false } = {}) {
  return dialog(title, { message, okLabel, danger });
}

export function fmtTime(date) {
  const d = new Date(date);
  return d.toLocaleTimeString([], { hour12: false }) + "." + String(d.getMilliseconds()).padStart(3, "0").slice(0, 1);
}

export function fmtClock(seconds) {
  seconds = Math.max(0, Math.ceil(seconds));
  return `${Math.floor(seconds / 60)}:${String(seconds % 60).padStart(2, "0")}`;
}

export const STATIONS = ["R1", "R2", "R3", "B1", "B2", "B3"];
export const MATCH_STATES = [
  "PreMatch", "StartMatch", "AutoPeriod", "PausePeriod", "TeleopPeriod", "PostMatch", "TimeoutActive", "PostTimeout",
];

/** Keyed cache so panels can update elements in place instead of re-rendering. */
export function keyed() {
  const map = new Map();
  return (key, make) => {
    if (!map.has(key)) map.set(key, make());
    return map.get(key);
  };
}
