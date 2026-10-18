// Protocol inspector: every packet and command the arena exchanges with the simulated field — FMS control and DS
// packets, Modbus polls, sACN light frames, the access point's HTTP API, the switch's telnet and the SCC SSH sessions —
// filterable, searchable, pausable, with rates per protocol and a JSON export.
import { h, replace, toast } from "../core/dom.js";
import { cmd } from "../core/store.js";
import { registerPanel } from "../ui/dock.js";

const PROTOCOLS = [
  ["ds", "Driver stations"], ["modbus", "Modbus PLC"], ["sacn", "sACN lights"], ["ap", "Access point"],
  ["telnet", "Switch (telnet)"], ["ssh", "SCC (SSH)"], ["altio", "Alternate IO / E-stop boxes"],
];
const KEEP = 1500;

registerPanel({
  id: "inspector",
  title: "Inspector",
  mount(el) {
    const controls = h("div.card");
    const rates = h("div.row.wrap.small");
    const list = h("div.log.packets", { style: { height: "460px" } });
    const detail = h("div.small.mono", { style: { whiteSpace: "pre-wrap", wordBreak: "break-all", marginTop: "6px" } });
    el.append(controls, h("div.card", rates, list, detail));

    const enabled = new Set(["ds", "modbus", "ap", "telnet", "ssh", "altio"]); // sACN is 40+ frames/s: off until asked for
    let paused = false;
    let text = "";
    let last = 0;
    let packets = [];
    let lastCounts = null;
    let lastCountsAt = 0;

    replace(controls,
      h("h3", "Protocol inspector"),
      h("div.row.wrap", PROTOCOLS.map(([id, label]) => h("label.check.small",
        h("input", { type: "checkbox", checked: enabled.has(id), onchange: (e) => {
          if (e.target.checked) enabled.add(id); else enabled.delete(id);
          restart();
        } }), label))),
      h("div.row", { style: { marginTop: "6px" } },
        h("input", { type: "search", placeholder: "Filter (station, FC15, universe, POST…)", style: { flex: "1" },
          oninput: (e) => { text = e.target.value; restart(); } }),
        h("button.btn.sm", { onclick: (e) => { paused = !paused; e.target.textContent = paused ? "Resume" : "Pause"; } }, "Pause"),
        h("button.btn.sm", { onclick: () => { cmd("inspector.clear").then(restart); } }, "Clear"),
        h("button.btn.sm", { onclick: exportJson }, "Export")),
      h("div.hint", "Click a line for its raw bytes. Driver-station status packets are sampled (1 in 25); everything else is complete."));

    function restart() {
      last = 0;
      packets = [];
      render();
    }

    function exportJson() {
      const blob = new Blob([JSON.stringify(packets, null, 2)], { type: "application/json" });
      const link = h("a", { href: URL.createObjectURL(blob), download: `freezy-sim-packets-${Date.now()}.json` });
      document.body.append(link);
      link.click();
      link.remove();
      toast(`Exported ${packets.length} packets`);
    }

    function render() {
      const atBottom = list.scrollTop + list.clientHeight >= list.scrollHeight - 30;
      replace(list, packets.map((p) => h("div.line", {
        style: { cursor: p.hex ? "pointer" : "default" },
        onclick: () => { detail.textContent = `${p.protocol} ${p.direction} ${p.peer} · ${p.length} bytes\n${p.hex ? p.hex.replace(/(..)/g, "$1 ") : "(text)"}`; },
      },
      h("span.t", new Date(p.time).toLocaleTimeString([], { hour12: false }) + "." + String(new Date(p.time).getMilliseconds()).padStart(3, "0")),
      h("span.s", `${p.direction === "in" ? "→" : "←"} ${p.protocol}`),
      h("span.m", h("b", p.peer + " "), p.summary))));
      if (atBottom) list.scrollTop = list.scrollHeight;
    }

    async function poll() {
      if (!paused && el.offsetParent !== null) {
        try {
          const result = await cmd("inspector.query", { after: last, protocols: [...enabled], text, limit: 400 });
          if (result) {
            last = result.last;
            if (result.packets && result.packets.length) {
              packets = packets.concat(result.packets).slice(-KEEP);
              render();
            }
            const now = performance.now();
            if (lastCounts && now - lastCountsAt > 900) {
              const seconds = (now - lastCountsAt) / 1000;
              replace(rates, PROTOCOLS.map(([id, label]) => h("span.mono", { style: { marginRight: "12px" } },
                `${label}: ${(((result.counts[id] || 0) - (lastCounts[id] || 0)) / seconds).toFixed(1)}/s`)));
              lastCounts = result.counts;
              lastCountsAt = now;
            } else if (!lastCounts) {
              lastCounts = result.counts;
              lastCountsAt = now;
            }
          }
        } catch { /* the simulator is restarting */ }
      }
      setTimeout(poll, 500);
    }
    poll();
  },
});
