# Converts FIRST's 2026 REBUILT playing field STEP into field-cad.glb for the 3D view (web/js/scene/cadfield.js),
# and measures the BUMP's real cross-section (bump-profile.json; the constants in internal/world/fuel.go groundHeight
# came from it).
#
# The field model is FIRST's and is not shipped with Freezy Sim: download the STEP yourself from
# https://www.firstinspires.org/resources/library/frc/playing-field ("Official Field Assets", STEP File) and convert
# your own copy. Step by step: docs/FIELD_MODEL.md.
#
# Run with FreeCAD's headless interpreter (portable FreeCAD 1.1 works; keep the install path short, FreeCAD fails on
# paths over 260 characters):
#   set FIELD_STEP=C:\fc\FE-2026-rev-rebuilt-playing-field.step
#   set OUT_DIR=C:\fc\out
#   freecadcmd.exe -c "exec(open(r'tools/cad/export_field.py').read())"
# then import OUT_DIR\field-cad.glb from the sim's graphics menu (⚙ > Field model > Import .glb…), or copy it to
# run\models\field-cad.glb in the sim folder. No rebuild is needed.
#
# What it does: fasteners, tape, cable ties, FUEL, carpet, the scoring table block and the AprilTags (the view draws its
# own) are left out, as are the chute door and the hub light diffusers (the view animates/lights its own). Each
# remaining part is meshed (5 mm / 0.6 rad deflection; diamond plate as a flat box, the view textures it) and merged
# into one mesh per element, material class and alliance side; nodes are named "Element|class|side". Positions are
# meters in the CAD frame (field center origin, z up). About a million triangles, 14 MB.
import FreeCAD, Part, MeshPart, re, json, struct, time, collections
import os
STEP = os.environ["FIELD_STEP"]  # FE-2026-rev-rebuilt-playing-field.step
OUT = os.environ.get("OUT_DIR", os.path.dirname(STEP))
import Import
if not os.path.exists(OUT + "/field.FCStd"):
    # The STEP import takes a couple of minutes; keep the document for later runs.
    FreeCAD.newDocument("field")
    Import.insert(STEP, "field")
    FreeCAD.ActiveDocument.saveAs(OUT + "/field.FCStd")
    FreeCAD.closeDocument("field")
doc = FreeCAD.openDocument(OUT + "/field.FCStd")
log = open(OUT + "/export.txt", "w", encoding="utf-8")
SKIP = re.compile(r"Fuel|Rivet|bolt|nut|PEM |Cable Tie|screw|Screw|[Ww]asher|Hook Tape|Loop Tape|Gaff|Carpet|Seaming|Quick Release|Connector Pin|Bushing|[Gg]rommet|SCORING TABLE|Thread_Forming|AprilTag|Spacer|shoulder_|Knob|Flange|Flanged Bearing|REV-21-|Pulley|Spur Gear|Leveling Mount|Lever Handle|Gas Spring|Pin Body|Riser Pin|Arm Pin|9600K|Chute Door|Diffuser", re.I)
# Material classes by part name (first match wins).
CLASSES = [
    ("alliance", r"Bump Plastic|Vinyl|Exchange Border"),
    ("diamond", r"Diamond Plate|FE-00004-0[2378]|FE-00014-02"),
    ("diffuser", r"Diffuser"),
    ("clear", r"Panel|Funnel Side|Field Plastic|Acrylic|Chute|Internal Wall|Inlet Funnel|Gate Plastic|Upper Plastic|Lower Plastic|Skirt"),
    ("hdpe", r"Ramp, |Exchange Ramp|Sponsor Panel|Counter Side Plate|Top Plate|Bottom Plate|Middle Divider|Bump Body|Trip Guard"),
    ("net", r"Hub Net \("),
    ("rubber", r"Tread|Belt|Cushion|Edge Tape|Profile"),
    ("dark", r"CIM Motor|Case|Counting Sensor|Gearbox Cover|Sensor Reflector|REV-29-|MAXSpline|Roller Tube|Roller Shaft|Tube Plug|Wheel"),
    ("steel", r"Hub Base|Bump Pin|Pivot Shaft|Rung"),
]
def classify(label):
    for name, pattern in CLASSES:
        if re.search(pattern, label):
            return name
    return "alu"
root = [o for o in doc.Objects if not o.InList][0]
def leaves(o):
    if o.TypeId == "Part::Feature":
        yield o
    for k in getattr(o, "Group", []) or []:
        yield from leaves(k)
LINEAR, ANGULAR = 5.0, 0.6  # mm, rad
MIN_SIZE = 45.0  # mm: smaller parts are not worth drawing at field scale
groups = collections.OrderedDict()  # (component, class, side) -> [positions(list), indices(list)]
stats = collections.Counter()
t0 = time.time()
for comp in root.Group:
    if comp.TypeId != "App::Part":
        continue
    cname = re.sub(r"[^A-Za-z]", "", comp.Label.split(":")[1].split("<")[0]) if ":" in comp.Label else comp.Label
    for leaf in leaves(comp):
        if SKIP.search(leaf.Label):
            continue
        cls = classify(leaf.Label)
        shape = leaf.Shape
        if shape.BoundBox.DiagonalLength < MIN_SIZE:
            continue
        if cls == "diamond":
            # The tread pattern is modeled as geometry; a flat plate with the diamond texture looks the same.
            b = shape.BoundBox
            shape = Part.makeBox(max(b.XLength, 0.5), max(b.YLength, 0.5), max(b.ZLength, 0.5), FreeCAD.Vector(b.XMin, b.YMin, b.ZMin))
        side = "blue" if shape.BoundBox.Center.x < 0 else "red"
        if cls != "alliance":
            side = "any"
        key = (cname, cls, side)
        g = groups.setdefault(key, [[], []])
        try:
            mesh = MeshPart.meshFromShape(Shape=shape, LinearDeflection=LINEAR, AngularDeflection=ANGULAR, Relative=False)
        except Exception:
            continue
        pts, tris = mesh.Topology
        base = len(g[0]) // 3
        for p in pts:
            g[0].extend((p.x, p.y, p.z))
        for a, b, c in tris:
            g[1].extend((base + a, base + b, base + c))
        stats[cname] += 1
log.write("tessellated in %.1f s; parts per component %r\n" % (time.time() - t0, dict(stats)))

# GLB: one node per group, positions in meters in the CAD frame (field center origin, z up); the viewer turns it.
bins = bytearray()
accessors, views, meshes, nodes = [], [], [], []
for (cname, cls, side), (pos, idx) in groups.items():
    if not idx:
        continue
    n = len(pos) // 3
    xs, ys, zs = pos[0::3], pos[1::3], pos[2::3]
    mins = [min(xs) / 1000, min(ys) / 1000, min(zs) / 1000]
    maxs = [max(xs) / 1000, max(ys) / 1000, max(zs) / 1000]
    pbytes = struct.pack("<%df" % len(pos), *[v / 1000 for v in pos])
    views.append({"buffer": 0, "byteOffset": len(bins), "byteLength": len(pbytes), "target": 34962})
    bins += pbytes
    while len(bins) % 4: bins += b"\0"
    accessors.append({"bufferView": len(views) - 1, "componentType": 5126, "count": n, "type": "VEC3", "min": mins, "max": maxs})
    big = n > 65535
    ibytes = struct.pack("<%d%s" % (len(idx), "I" if big else "H"), *idx)
    views.append({"buffer": 0, "byteOffset": len(bins), "byteLength": len(ibytes), "target": 34963})
    bins += ibytes
    while len(bins) % 4: bins += b"\0"
    accessors.append({"bufferView": len(views) - 1, "componentType": 5125 if big else 5123, "count": len(idx), "type": "SCALAR"})
    meshes.append({"name": "%s|%s|%s" % (cname, cls, side), "primitives": [{"attributes": {"POSITION": len(accessors) - 2}, "indices": len(accessors) - 1}]})
    nodes.append({"name": "%s|%s|%s" % (cname, cls, side), "mesh": len(meshes) - 1})
    log.write("%-28s %-9s %-5s verts %7d tris %7d\n" % (cname, cls, side, n, len(idx) // 3))
gltf = {"asset": {"version": "2.0", "generator": "freezy-sim STEP export (FreeCAD tessellation)"},
        "scene": 0, "scenes": [{"nodes": list(range(len(nodes)))}], "nodes": nodes, "meshes": meshes,
        "accessors": accessors, "bufferViews": views, "buffers": [{"byteLength": len(bins)}]}
js = json.dumps(gltf, separators=(",", ":")).encode()
while len(js) % 4: js += b" "
glb = struct.pack("<III", 0x46546C67, 2, 12 + 8 + len(js) + 8 + len(bins)) + struct.pack("<II", len(js), 0x4E4F534A) + js + struct.pack("<II", len(bins), 0x004E4942) + bytes(bins)
open(OUT + "/field-cad.glb", "wb").write(glb)
log.write("glb %.1f MB\n" % (len(glb) / 1e6))

# The BUMP's real cross-section (P5): the top surface height across the bump (x) through its middle, for bump <1>.
bump = [c for c in root.Group if c.Label.startswith("GE-26100: Bump <1>")][0]
solids = [l.Shape for l in leaves(bump) if not SKIP.search(l.Label)]
comp = Part.makeCompound(solids)
bb = comp.BoundBox
yc = bb.Center.y
profile = []
x = bb.XMin - 5
while x <= bb.XMax + 5:
    line = Part.makeLine(FreeCAD.Vector(x, yc, -50), FreeCAD.Vector(x, yc, 400))
    top = 0.0
    sec = comp.section(line)
    for v in sec.Vertexes:
        top = max(top, v.Point.z)
    profile.append((round(x - bb.Center.x, 1), round(top, 2)))
    x += 10
log.write("bump bb x %.1f..%.1f (%.1f) y %.1f z %.1f\n" % (bb.XMin, bb.XMax, bb.XLength, bb.YLength, bb.ZMax))
json.dump({"centerX": bb.Center.x, "lengthY": bb.YLength, "profile": profile}, open(OUT + "/bump-profile.json", "w"))
log.close()
