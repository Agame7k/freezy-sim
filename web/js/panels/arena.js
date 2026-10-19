// Arena panel: sandbox launcher, websocket health, live start blockers and the arena process log.
import { h, replace, led, tag, cmdButton, toast } from "../core/dom.js";
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

    // First-run setup: the one thing a new user must do is point the simulator at their arena folder.
    const setupCard = h("div.card.setup-card", { hidden: true });
    const sourceInput = h("input", { type: "text", placeholder: "C:\\path\\to\\freezy-arena", "aria-label": "Arena folder path" });
    let setupKey = "";
    async function useSource(path) {
      try {
        await cmd("arena.setSource", { path });
        toast("Arena folder saved — starting the arena…", "good");
      } catch (err) {
        toast(err.message, "error", 8000);
      }
    }
    async function pickSource() {
      try {
        const result = await cmd("arena.pickSource");
        if (result?.path) toast("Arena folder saved — starting the arena…", "good");
      } catch (err) {
        toast(err.message, "error", 8000);
      }
    }
    function renderSetup(s) {
      setupCard.hidden = !s?.needsSource;
      if (!s?.needsSource) { setupKey = ""; return; }
      const key = JSON.stringify([s.detail, s.suggestions]);
      if (key === setupKey) return; // keep whatever is being typed
      setupKey = key;
      const found = s.suggestions || [];
      replace(setupCard,
        h("h3", "Set up: choose your arena folder"),
        h("div.hint", "Freezy Sim runs a private copy of Freezy Arena (or Cheesy Arena) and needs to know where it is. ",
          "Pick the folder that has ", h("span.mono", "go.mod"), " in it — usually called ",
          h("span.mono", "freezy-arena"), " or ", h("span.mono", "cheesy-arena"), ". Your folder is only read, never changed."),
        s.detail ? h("div.small.bad-text", s.detail.split(". Choose the arena folder")[0] + ".") : null,
        found.length ? h("div",
          h("div.small.muted", found.length === 1 ? "Found one on this PC:" : "Found these on this PC:"),
          found.map((path) => h("div.btn-row.setup-found",
            h("span.mono.small", path),
            h("button.btn.go", { onclick: () => useSource(path) }, "Use this one")))) : null,
        h("div.btn-row", h("button.btn", { class: found.length ? "" : "go", onclick: pickSource }, "Browse for the folder…")),
        h("div.btn-row", sourceInput,
          h("button.btn", { title: "Use the folder typed in the box",
            onclick: () => sourceInput.value.trim() ? useSource(sourceInput.value.trim()) : toast("Type the arena folder's path first.", "error") },
          "Use this path")),
        h("div.hint", "No arena yet? Download Freezy Arena from github.com/Freezy-Arena/freezy-arena (Code → Download ZIP), ",
          "extract it, then choose that folder here."),
      );
    }

    el.append(
      setupCard,
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
      renderSetup(s);
      const good = s.state === "running" || s.state === "external";
      const color = good ? (s.ready ? "green" : "amber") : s.state === "error" || s.state === "exited" ? "red" : "amber";
      replace(launcherCard,
        h("h3", "Freezy Arena sandbox", h("span.grow"), led(color), h("span.mono.small", s.state.toUpperCase())),
        h("div.hint", s.detail || ""),
        h("dl.kv",
          h("dt", "Mode"), h("dd", s.mode),
          h("dt", "Source (read-only)"), h("dd", s.sourceDir || "— not chosen yet"),
          h("dt", "Sandbox"), h("dd", s.sandboxDir),
          h("dt", "Binary"), h("dd", s.binary || "—"),
          h("dt", "PID"), h("dd", s.pid || "—"),
          h("dt", "URL"), h("dd", h("a", { href: s.url, target: "_blank" }, s.url)),
        ),
        h("div.btn-row", arenaButtons(s)),
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

    // Launcher controls, enabled only when they can do something. "external" is an arena the sim did not start.
    function arenaButtons(s) {
      const stopped = ["stopped", "exited", "error", "idle", ""].includes(s.state || "");
      const external = s.state === "external";
      const buttons = [
        cmdButton("Start", "arena.start", {}, "go", { title: "Start the sandbox arena (builds it first if needed)" }),
        cmdButton("Stop", "arena.stop", {}, "danger",
          { title: "Stop the sandbox arena. Matches, displays and scoring stop until it is started again." }),
        cmdButton("Restart", "arena.restart", {}, "", { title: "Stop the sandbox arena and start it again" }),
        cmdButton("Rebuild from source", "arena.rebuild", {}, "", {
          title: "Build the arena again from your arena folder (after you pull new arena code), then start it",
          confirm: { title: "Rebuild Freezy Arena?", message: "The arena stops, is rebuilt from your arena folder and starts again. This takes a minute or two.", okLabel: "Rebuild" },
        }),
        cmdButton("Reset sandbox DB", "arena.resetDb", {}, "warn", {
          title: stopped ? "Delete the sandbox arena's database: teams, schedule, results and settings" : "Stop the arena first to reset its database",
          confirm: { title: "Reset the sandbox database?", message: "The sandbox arena's teams, schedule, match results and settings are deleted. " +
            "Your own arena folder is not touched. This can't be undone.", okLabel: "Delete database", danger: true },
        }),
        h("button.btn", { title: "Point the simulator at a different arena folder", onclick: pickSource }, "Change arena folder…"),
      ];
      const [start, stop, restart, rebuild, reset] = buttons;
      start.disabled = !stopped || !!s.needsSource;
      stop.disabled = stopped || external;
      restart.disabled = external || !!s.needsSource;
      rebuild.disabled = external || !!s.needsSource;
      reset.disabled = !stopped;
      return buttons;
    }

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
