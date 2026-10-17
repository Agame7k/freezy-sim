// Arena panel: sandbox launcher, websocket health, live start blockers and the arena process log.
import { h, replace, led, tag, cmdButton } from "../core/dom.js";
import { on, onEvent, recentEvents, cmd } from "../core/store.js";
import { registerPanel } from "../ui/dock.js";

registerPanel({
  id: "arena",
  title: "Arena",
  mount(el, { setBadge }) {
    const launcherCard = h("div.card");
    const socketsCard = h("div.card");
    const blockersCard = h("div.card");
    const servicesCard = h("div.card");
    const portsCard = h("div.card", { hidden: true });
    const logBox = h("div.log", { style: { height: "320px" } });

    // Emulated devices whose address another program already holds (checked at startup).
    on("sim.ports", (problems) => {
      portsCard.hidden = !problems?.length;
      if (!problems?.length) return;
      replace(portsCard,
        h("h3", "Address conflicts", h("span.grow"), tag(`${problems.length}`, "bad")),
        h("div.hint", "These emulated devices could not claim their address, so the arena will not be able to reach them."),
        h("table.tbl", problems.map((p) => h("tr",
          h("td", h("b", p.device), h("div.small.mono.muted", p.address)),
          h("td.small", p.fix)))));
    });

    // Other devices (gui.remote in freezy-sim.json): where to point them.
    const remoteCard = h("div.card");
    const renderRemote = (remote, urls) => {
      replace(remoteCard,
        h("h3", "Remote access", h("span.grow"), tag(remote?.mode || "off", remote?.mode ? "ok" : "")),
        !remote?.mode ? h("div.hint", 'Only this PC can open the simulator. Set "gui": { "remote": "tailscale" } (or "lan") in freezy-sim.json and restart to let other devices in.') :
          urls.length ? urls.map((url) => h("div.mono.small", h("a", { href: url, target: "_blank" }, url))) :
            h("div.hint", remote.mode === "tailscale" ? "Waiting for a Tailscale address on this PC." : "No network address found."),
        remote?.tokenRequired && urls.length ? h("div.small.muted", "These links include the access token. Don't post them or share screenshots of them.") : null,
        remote?.mode ? h("div.small.muted", "The arena's own pages (displays, panels) open from the same address on port 8080. If a device can't connect, allow freezy-sim.exe and cheesy-arena.exe through Windows Firewall.") : null);
    };
    on("gui.remote", (remote) => {
      renderRemote(remote, remote?.urls || []);
      // The topic carries the LAN addresses without the token (topics are recorded); ask for the full links.
      if (remote?.tokenRequired) cmd("gui.remoteLinks").then((links) => renderRemote(remote, links?.urls || [])).catch(() => {});
    });

    el.append(
      portsCard,
      remoteCard,
      launcherCard,
      blockersCard,
      servicesCard,
      socketsCard,
      h("div.card", h("h3", "Arena process log"), logBox),
    );

    on("arena.launcher", (s) => {
      if (!s) return;
      const good = s.state === "running" || s.state === "external";
      const color = good ? (s.ready ? "green" : "amber") : s.state === "error" || s.state === "exited" ? "red" : "amber";
      replace(launcherCard,
        h("h3", "Freezy Arena sandbox", h("span.grow"), led(color), h("span.mono.small", s.state.toUpperCase())),
        h("div.hint", s.detail || ""),
        h("dl.kv",
          h("dt", "Mode"), h("dd", s.mode),
          h("dt", "Source (read-only)"), h("dd", s.sourceDir || "— set arena.sourceDir in freezy-sim.json"),
          h("dt", "Sandbox"), h("dd", s.sandboxDir),
          h("dt", "Binary"), h("dd", s.binary || "—"),
          h("dt", "PID"), h("dd", s.pid || "—"),
          h("dt", "URL"), h("dd", h("a", { href: s.url, target: "_blank" }, s.url)),
        ),
        h("div.btn-row",
          cmdButton("Start", "arena.start", {}, "go"),
          cmdButton("Stop", "arena.stop", {}, "danger"),
          cmdButton("Restart", "arena.restart"),
          cmdButton("Rebuild from source", "arena.rebuild"),
          cmdButton("Reset sandbox DB", "arena.resetDb", {}, "warn"),
        ),
      );
    });

    on("arena.sockets", (sockets) => {
      replace(socketsCard, h("h3", "Arena websockets"),
        h("table.tbl",
          h("tr", h("th", ""), h("th", "Socket"), h("th", "Messages"), h("th", "Last error")),
          (sockets || []).map((s) => h("tr",
            h("td", led(s.connected ? "green" : "red")),
            h("td.mono", s.name),
            h("td.mono", s.received),
            h("td.small.muted", s.lastError || ""),
          )),
        ),
      );
    });

    on("arena.arenaStatus", (status) => {
      if (!status) return;
      const conditions = status.StartMatchConditions || [];
      setBadge(conditions.length ? String(conditions.length) : "");
      replace(blockersCard,
        h("h3", "Start-match conditions", h("span.grow"),
          status.CanStartMatch ? tag("READY", "good") : tag(`${conditions.length} blocking`, "bad")),
        conditions.length
          ? h("ul", { style: { margin: "0", paddingLeft: "18px" } }, conditions.map((c) => h("li", c)))
          : h("div.hint", "The arena will allow a match start."),
      );
      replace(servicesCard, h("h3", "Field services (as the arena sees them)"),
        h("div.grid2",
          service("PLC healthy", status.PlcIsHealthy),
          service("Field E-stop clear", !status.FieldEStop),
          service("FTA ready", status.IsFtaReady),
          service("Score table IO", status.ScoreTableIOIsHealthy, status.ScoreTableIOEnabled),
          service("Red E-stop box", status.RedEstopsIsHealthy, status.RedEstopsEnabled),
          service("Blue E-stop box", status.BlueEStopsIsHealthy, status.BlueEstopsEnabled),
          h("div.row", h("span.muted", "Access point"), h("span.mono", status.AccessPointStatus || "—")),
          h("div.row", h("span.muted", "Switch"), h("span.mono", status.SwitchStatus || "—")),
          h("div.row", h("span.muted", "Red SCC"), h("span.mono", status.RedSCCStatus || "—")),
          h("div.row", h("span.muted", "Blue SCC"), h("span.mono", status.BlueSCCStatus || "—")),
        ),
      );
    });

    function service(label, healthy, enabled = true) {
      return h("div.row", led(!enabled ? "" : healthy ? "green" : "red"), h("span", label),
        !enabled ? h("span.small.muted", "(disabled)") : null);
    }

    const addLine = (event) => {
      if (event.source !== "arena" && event.source !== "launcher") return;
      const atBottom = logBox.scrollTop + logBox.clientHeight >= logBox.scrollHeight - 8;
      logBox.appendChild(h("div.line", { class: "lvl-" + event.level },
        h("span.t", new Date(event.time).toLocaleTimeString([], { hour12: false })),
        h("span.s", event.source), h("span.m", event.msg)));
      while (logBox.childElementCount > 1500) logBox.firstChild.remove();
      if (atBottom) logBox.scrollTop = logBox.scrollHeight;
    };
    recentEvents().forEach(addLine);
    onEvent(addLine);
  },
});
