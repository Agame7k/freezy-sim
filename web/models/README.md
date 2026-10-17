# 3D models

- `human.fbx`: "Animated Human" by Quaternius, CC0 1.0 (see `human-LICENSE.txt`), from
  https://opengameart.org/content/animated-human-low-poly. Used for every person in the venue (`web/js/scene/people.js`).

The official field CAD is **not** in this folder and is not shipped with Freezy Sim. FIRST publishes the field model for
teams but does not license it for redistribution, so each user converts their own copy of FIRST's STEP file with
`tools/cad/export_field.py` and imports the `.glb` from the graphics menu. It is kept in `run/models/field-cad.glb`. See
[docs/FIELD_MODEL.md](../../docs/FIELD_MODEL.md).
