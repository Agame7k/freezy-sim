// Network panel: the emulated field network (access point, field switch, SCCs) and the hub lighting data (sACN).
import { h, replace, led, tag, toast, STATIONS } from "../core/dom.js";
import { on, cmd } from "../core/store.js";
import { registerPanel } from "../ui/dock.js";

const run = (command, args) => cmd(command, args).catch((e) => toast(e.message, "error"));
const AP_NAMES = { R1: "red1", R2: "red2", R3: "red3", B1: "blue1", B2: "blue2", B3: "blue3" };
const QUALITY = { excellent: "good", good: "good", warning: "warn", caution: "bad" };

registerPanel({
  id: "network",
  title: "Network",
  mount(el) {
    const topCard = h("div.card");
    const apCard = h("div.card");
    const switchCard = h("div.card");
    const sccCard = h("div.card");
    const lightsCard = h("div.card");
    el.append(topCard, apCard, switchCard, sccCard, lightsCard);

    on("network.state", (n) => {
      if (!n) return;
      const device = (name, label, online) => h("button.btn.sm", { class: online ? "on" : "off",
        onclick: () => run("network.device", { device: name, online: !online }) }, online ? `${label} online` : `${label} OFFLINE`);
      replace(topCard,
        h("h3", "Managed field network", h("span.grow"), n.enabled ? tag("network security ON", "good") : tag("off")),
        h("div.row.wrap",
          h("button.btn.sm", { class: n.enabled ? "on" : "", onclick: () => run("network.enable", { enabled: !n.enabled }) },
            n.enabled ? "Disable network emulation" : "Enable network emulation"),
          device("ap", "AP", n.ap.online), device("switch", "Switch", n.switch.online),
          device("redScc", "Red SCC", n.scc[0].online), device("blueScc", "Blue SCC", n.scc[1].online)),
        h("div.row.small", { style: { marginTop: "6px" } }, h("span.muted", "Wi-Fi interference"),
          h("input", { type: "range", min: 0, max: 30, step: 1, value: n.ap.interference, style: { flex: 1 },
            onchange: (e) => run("network.interference", { db: Number(e.target.value) }) }),
          h("span.mono", `${n.ap.interference} dB`)),
        h("div.hint", "With network security on, each match load makes the arena reprogram the AP (team SSIDs/WPA keys), ",
          "rebuild the switch VLANs and cycle the SCC ports. Robot radios only link to their own team's SSID, and driver ",
          "stations only reach the FMS through an enabled SCC port."));

      const ap = n.ap;
      replace(apCard,
        h("h3", "Access point", h("span.grow"), tag(ap.status, ap.status === "ACTIVE" ? "good" : "warn"),
          h("span.small.muted", ` ch ${ap.channel} · ${ap.address}`)),
        ap.bindError ? h("div.hint", { style: { color: "var(--red)" } }, ap.bindError) : null,
        h("table.tbl",
          h("tr", h("th", "Stn"), h("th", "SSID"), h("th", "Link"), h("th", "SNR"), h("th", "Quality"), h("th", "Rx/Tx"), h("th", "Used")),
          STATIONS.map((s) => {
            const st = ap.stations?.[AP_NAMES[s]];
            return h("tr",
              h("td.mono", { style: { color: s[0] === "R" ? "#ffb1b3" : "#b7d2ff" } }, s),
              h("td.mono", st?.ssid || "—"),
              h("td", led(st?.isLinked ? "green" : "red")),
              h("td.mono", st?.isLinked ? `${st.signalNoiseRatio} dB` : "—"),
              h("td", st?.isLinked ? tag(st.connectionQuality, QUALITY[st.connectionQuality]) : ""),
              h("td.mono.small", st?.isLinked ? `${st.rxRateMbps}/${st.txRateMbps}` : ""),
              h("td.mono", st?.isLinked ? `${st.bandwidthUsedMbps} Mb` : ""));
          })),
        h("div.small.muted", { style: { marginTop: "4px" } },
          `${ap.requests} status polls · ${ap.configures} configurations` +
          (ap.lastConfigAt && !ap.lastConfigAt.startsWith("0001") ? ` · last ${new Date(ap.lastConfigAt).toLocaleTimeString([], { hour12: false })}` : "")));

      const sw = n.switch;
      replace(switchCard,
        h("h3", "Field switch (telnet)", h("span.grow"), led(sw.online && !sw.bindError ? "green" : "red"),
          h("span.small.muted", ` ${sw.address} · ${sw.sessions} sessions`)),
        h("div.row.wrap", { style: { gap: "6px" } }, STATIONS.map((s) => {
          const team = sw.vlans?.[s];
          return tag(`${s} VLAN${(STATIONS.indexOf(s) + 1) * 10}: ${team ? `10.${Math.floor(team / 100)}.${team % 100}.0/24` : "—"}`, team ? "good" : "");
        })),
        h("details", h("summary.small.muted", "Session transcript"),
          h("div.log", { style: { height: "160px" } }, (sw.transcript || []).slice().reverse().map((line) => h("div.line", h("span.m", line))))));

      replace(sccCard, h("h3", "Station control cabinets (SSH)"),
        h("div.grid2", n.scc.map((scc) => h("div",
          h("div.row", led(scc.online && !scc.bindError ? "green" : "red"), h("b", scc.name), h("span.small.muted", scc.address)),
          h("div.row", { style: { gap: "6px", margin: "4px 0" } }, scc.ports.map((up, i) => tag(`DS${i + 1} ${up ? "up" : "SHUT"}`, up ? "good" : "bad"))),
          h("details", h("summary.small.muted", `${scc.sessions} sessions`),
            h("div.log", { style: { height: "110px" } }, (scc.transcript || []).slice().reverse().map((line) => h("div.line", h("span.m", line)))))))));
    });

    // Hub lighting (sACN) — the exact pixels the arena is sending.
    let status = null;
    on("sacn.status", (s) => { status = s; });
    on("sacn.hubs", (hubs) => {
      if (!hubs) return;
      const strip = (hex) => h("div", { style: { display: "grid", gridTemplateColumns: "repeat(16, 1fr)", gap: "2px" } },
        Array.from({ length: 64 }, (_, i) => h("div", { title: `side ${Math.floor(i / 16) + 1} ${(i % 16) < 8 ? "bottom" : "top"} px ${i % 8}`,
          style: { height: "12px", borderRadius: "2px", background: `#${hex.substr(i * 6, 6)}`, boxShadow: "inset 0 0 0 1px #0006" } })));
      replace(lightsCard,
        h("h3", "Hub lights (sACN from the arena)", h("span.grow"),
          status ? h("span.small.muted", `${status.mode} universe · ${status.packetHz.toFixed(1)} Hz · ${status.packets} packets · src ${status.source || "—"}`) : null),
        h("div.small.muted", "Red hub — 4 sides × bottom/top bar × 8 px"), strip(hubs.red),
        h("div.small.muted", { style: { marginTop: "6px" } }, "Blue hub"), strip(hubs.blue));
    });
  },
});
