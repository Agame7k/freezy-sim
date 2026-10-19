// Replay panel: the black box, saved match recordings, and the transport that plays them back into the whole GUI.
import { h, replace, tag, toast, fmtClock, confirmAction } from "../core/dom.js";
import { on, cmd } from "../core/store.js";
import { registerPanel } from "../ui/dock.js";
import { icon } from "../core/icons.js";

const run = (command, args) => cmd(command, args).catch((e) => toast(e.message, "error"));
const SPEEDS = [0.25, 0.5, 1, 2, 4, 8];

const mb = (bytes) => `${(bytes / 1e6).toFixed(1)} MB`;
const clock = (seconds) => fmtClock(Math.max(0, seconds));

/** Builds a transport bar (play/pause, scrubber, speed, exit) that updates in place from replay.state. */
function createTransport(cls) {
  let state = null;
  let scrubbing = false;
  const play = h("button.btn.sm", { title: "Play", "aria-label": "Play", onclick: () => run(state?.playing ? "replay.pause" : "replay.play") }, icon("play"));
  const back = h("button.btn.sm", { title: "Back 5 s", onclick: () => state && run("replay.seek", { position: Math.max(0, state.position - 5) }) }, "−5s");
  const forward = h("button.btn.sm", { title: "Forward 5 s",
    onclick: () => state && run("replay.seek", { position: Math.min(state.duration, state.position + 5) }) }, "+5s");
  const scrubber = h("input", { type: "range", min: 0, max: 1, step: 0.1, value: 0, style: { flex: "1", minWidth: "80px" },
    title: "Drag to jump to any point in the recording", "aria-label": "Replay position" });
  scrubber.addEventListener("input", () => { scrubbing = true; time.textContent = `${clock(+scrubber.value)} / ${clock(state?.duration || 0)}`; });
  scrubber.addEventListener("change", () => { scrubbing = false; run("replay.seek", { position: +scrubber.value }); });
  const time = h("span.mono.small", "0:00 / 0:00");
  const speed = h("select", { title: "Playback speed", "aria-label": "Playback speed", onchange: (e) => run("replay.speed", { speed: +e.target.value }) },
    SPEEDS.map((s) => h("option", { value: s }, `${s}×`)));
  const label = h("span.replay-label");
  const exit = h("button.btn.sm.warn", { onclick: () => run("replay.stop"), title: "Back to the live simulator" }, "Exit replay");
  const bar = h("div", { class: cls }, h("span.replay-tag", "REPLAY"), label, back, play, forward, scrubber, time, speed, exit);

  on("replay.state", (next) => {
    state = next;
    bar.style.display = next?.active ? "" : "none";
    if (!next?.active) return;
    replace(play, icon(next.playing ? "pause" : "play"));
    play.title = next.playing ? "Pause" : "Play";
    play.setAttribute("aria-label", play.title);
    label.textContent = next.meta.label;
    label.title = `Recorded ${new Date(next.meta.start).toLocaleString()}`;
    scrubber.max = next.duration;
    if (!scrubbing) {
      scrubber.value = next.position;
      time.textContent = `${clock(next.position)} / ${clock(next.duration)}`;
    }
    if (document.activeElement !== speed) speed.value = String(next.speed);
  });
  return bar;
}

// The transport also floats over the 3D view, so a replay can be driven without the dock open.
const viewport = document.getElementById("viewport");
if (viewport && !new URLSearchParams(location.search).get("panel")) viewport.appendChild(createTransport("replay-bar floating"));
on("replay.state", (state) => document.body.classList.toggle("replaying", !!state?.active));

registerPanel({
  id: "replay",
  title: "Replay",
  mount(el, { setBadge }) {
    const blackBoxCard = h("div.card");
    const transportCard = h("div.card", h("h3", "Now replaying"), createTransport("replay-bar"));
    const eventsCard = h("div.card");
    const libraryCard = h("div.card");
    el.append(blackBoxCard, transportCard, eventsCard, libraryCard);
    let recordings = [];
    let savedCount = -1;

    async function refresh() {
      try {
        recordings = await cmd("recorder.list");
      } catch {
        recordings = [];
      }
      renderLibrary();
    }

    on("recorder.state", (state) => {
      if (!state) return;
      setBadge(state.recording ? "●" : "");
      replace(blackBoxCard,
        h("h3", "Black box", h("span.grow"), state.recording ? tag(`Recording ${state.recording}`, "bad") : null),
        h("dl.kv",
          h("dt", "Holding"), h("dd", `${clock(state.seconds)} (of the last 10 min), ${mb(state.bytes)}`),
          h("dt", "Saved"), h("dd", `${state.saved} this session in ${state.dir}`)),
        h("div.btn-row",
          h("button.btn.sm.primary", { title: "Play back the last 10 minutes in the whole GUI (Exit replay goes back to the live field)",
            onclick: () => run("replay.load", { blackBox: true }) }, "Replay the black box"),
          [2, 5, 10].map((minutes) => h("button.btn.sm", {
            title: `Save the last ${minutes} minutes as a recording you can replay later`,
            onclick: async () => {
              const result = await cmd("recorder.saveBlackBox", { minutes }).catch((e) => toast(e.message, "error"));
              if (result) { toast(`Saved ${result.name}`); refresh(); }
            },
          }, `Save last ${minutes} min`))),
        h("div.hint", "Everything the simulator sees is kept for 10 minutes. Every match is saved automatically, from 5 s ",
          "before it starts to 10 s after it ends."));
      if (state.saved !== savedCount) {
        savedCount = state.saved;
        refresh();
      }
    });

    on("replay.state", (state) => {
      transportCard.style.display = state?.active ? "" : "none";
      eventsCard.style.display = state?.active ? "" : "none";
    });
    on("replay.events", (events) => {
      if (!events) return;
      replace(eventsCard,
        h("h3", "Log at the playhead"),
        events.length ? h("div.log", { style: { height: "200px" } },
          events.slice(-120).reverse().map((e) => h("div.line", { class: "lvl-" + e.level },
            h("span.t", new Date(e.time).toLocaleTimeString([], { hour12: false })),
            h("span.s", e.source), h("span.m", e.msg)))) : h("div.hint", "Nothing logged yet at this point."));
    });

    function renderLibrary() {
      replace(libraryCard,
        h("h3", "Recordings", h("span.grow"), h("button.btn.sm", { title: "Look for new recordings", onclick: refresh }, "Refresh")),
        recordings.length ? h("table.tbl",
          h("tr", h("th", "Recording"), h("th", "When"), h("th", "Length"), h("th", "Size"), h("th", "")),
          recordings.map((r) => h("tr",
            h("td", r.meta.label, r.meta.kind === "match" && r.meta.teams?.some((t) => t)
              ? h("div.small.muted", r.meta.teams.map((t) => t || "—").join(" "))
              : null),
            h("td.small", new Date(r.meta.start).toLocaleString([], { month: "short", day: "numeric", hour: "2-digit", minute: "2-digit" })),
            h("td.mono", clock(r.duration)),
            h("td.mono", mb(r.size)),
            h("td", h("div.row",
              h("button.btn.sm.primary", { title: "Play this recording back in the whole GUI",
                onclick: () => run("replay.load", { name: r.name }) }, "Replay"),
              h("button.btn.sm.ghost", {
                title: "Delete this recording", "aria-label": "Delete recording",
                onclick: async () => {
                  if (!(await confirmAction(`Delete ${r.meta.label}?`, { message: "The recording file is deleted. This can't be undone.", okLabel: "Delete", danger: true }))) return;
                  await run("recorder.delete", { name: r.name });
                  refresh();
                },
              }, icon("x"))))))) : h("div.hint", "No recordings yet. Play a match and it will show up here."));
    }
    renderLibrary();
  },
});
