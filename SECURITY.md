# Security

## Reporting a vulnerability

Please **don't open a public issue** for a security problem. Report it privately through GitHub instead: the
repository's **Security** tab → **Report a vulnerability**. The maintainer will reply as soon as they can. A fix will go
into the next release, with credit to you if you'd like it.

Only the latest release gets security fixes.

## What Freezy Sim exposes

Freezy Sim is a test tool meant to run on your own PC, next to an arena you control. Here is what it opens, and what it
protects:

- **The emulated field devices** (driver stations, PLC, E-stop boxes, sACN, AP, switch, SCCs) bind to **loopback
  addresses only** (127.0.0.x). Nothing on your network can reach them. Their passwords (`simpass` by default) protect
  nothing real: the arena is simply configured to use them.
- **The simulator GUI** (port 8090) listens on loopback only, unless you turn on remote access:
  - `"tailscale"` also admits devices on your tailnet. Tailscale authenticates them; anyone on your tailnet can then
    drive the sim and the arena.
  - `"lan"` admits anyone on the network who has the access token. The token is random per run unless you set
    `gui.token`. It is kept in an HttpOnly, SameSite=Lax cookie for 30 days, and it is never written to the sim's
    logs, recordings or bug bundles.
- **Cross-site requests are refused.** A web page from another site, open in a browser on the same PC, cannot open
  the GUI's websocket or send it commands, even though it can reach loopback.
- **The sandbox arena** runs with `-dev`, and listens on all interfaces on port 8080, like any arena. Windows Firewall
  decides who can reach it. It starts with no admin password, so anyone who can reach port 8080 can control it. Keep it
  to a private network, or set an admin password in the arena (and `arena.adminPassword` in the sim).

Anyone who can open the GUI can control the simulator *and*, through it, the arena: start and abort matches, press
E-stops, reset the sandbox database. Treat GUI access like access to the scoring table.

## What leaves your PC

Nothing, unless you send it. Freezy Sim has no telemetry and makes no network connections of its own except to the
arena. The sandbox arena is configured with The Blue Alliance and Nexus turned off. Building the arena downloads its
Go modules, through Go's normal module proxy.

**Bug-report bundles** are meant to be shared. They have passwords and access tokens removed, but they do include the
sandbox arena's database (teams, schedule, results) and logs. Look inside one before you post it publicly, if your
event data is private.

## Scope

In scope: the simulator itself (`freezy-sim.exe`, its web GUI and its emulators). For example: a way around the remote
access guard, a secret leaking into a recording or bundle, or a file written outside the sim's own folders (the sim
folder and `%LOCALAPPDATA%\freezy-sim`).

Out of scope: vulnerabilities in Freezy Arena or Cheesy Arena (report those to their projects), in WebView2, and
anything that needs an attacker who can already run programs on your PC.
