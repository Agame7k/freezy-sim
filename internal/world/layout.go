// Package world is the authoritative physical simulation of the field: robots, FUEL, hubs, outposts and towers.
// It runs in Go at a fixed rate so the same simulation drives the 3D view and headless test runs.
package world

import (
	"encoding/json"
	"io/fs"
	"math"
)

// Layout mirrors web/layouts/field-2026.json (WPILib field frame, meters).
type Layout struct {
	Field struct {
		Length float64 `json:"length"`
		Width  float64 `json:"width"`
	} `json:"field"`
	Guardrail struct {
		Height float64 `json:"height"`
	} `json:"guardrail"`
	Zones struct {
		AllianceZoneDepth float64 `json:"allianceZoneDepth"`
	} `json:"zones"`
	Hub struct {
		Size            float64               `json:"size"`
		BodyHeight      float64               `json:"bodyHeight"` // top of the vertical side panels (GE-26300: 49.75 in)
		RoofHeight      float64               `json:"roofHeight"` // top of the sloped roof, where the clear hopper starts
		OpeningHeight   float64               `json:"openingHeight"`
		OpeningDiameter float64               `json:"openingDiameter"`
		ThroatDiameter  float64               `json:"throatDiameter"`
		Positions       map[string][2]float64 `json:"positions"`
		ExitCount       int                   `json:"exitCount"`
		// Exit is the FUEL exit slot in the neutral-zone face, just under the AprilTag band (GE-26300 sheet 4).
		Exit struct {
			Bottom         float64    `json:"bottom"`
			Top            float64    `json:"top"`
			Lanes          []float64  `json:"lanes"` // lateral offsets of the fuel-counter lanes from the hub center
			DeflectorDepth float64    `json:"deflectorDepth"`
			Speed          [2]float64 `json:"speed"` // m/s range FUEL leaves the slot lip at
		} `json:"exit"`
		// Net is the aesthetic net over the neutral-zone side; it leans out and stops shots from the wrong side.
		Net struct {
			Bottom     float64 `json:"bottom"`
			MeshBottom float64 `json:"meshBottom"`
			Top        float64 `json:"top"`
			Width      float64 `json:"width"`
			Overhang   float64 `json:"overhang"`
		} `json:"net"`
	} `json:"hub"`
	Bump struct {
		Width       float64 `json:"width"`
		Depth       float64 `json:"depth"`
		Height      float64 `json:"height"`
		RampDegrees float64 `json:"rampDegrees"`
	} `json:"bump"`
	Trench struct {
		Width float64 `json:"width"`
		Depth float64 `json:"depth"` // the TRENCH zone under the structure (along x)
		// The structure itself is a thin frame across the field on the hub line: two legs and the panel over the
		// opening that carries the AprilTags on both faces (tags 7.5 cm apart). The opening is centered on the tags.
		Thickness      float64 `json:"thickness"`
		OpeningCenterY float64 `json:"openingCenterY"`
		Height         float64 `json:"height"`
		ClearWidth     float64 `json:"clearWidth"`
		ClearHeight    float64 `json:"clearHeight"`
	} `json:"trench"`
	Tower struct {
		Width          float64            `json:"width"`
		UprightSpacing float64            `json:"uprightSpacing"`
		UprightSetback float64            `json:"uprightSetback"`
		UprightDepth   float64            `json:"uprightDepth"`
		RungOverhang   float64            `json:"rungOverhang"`
		UprightHeight  float64            `json:"uprightHeight"`
		BaseDepth      float64            `json:"baseDepth"`
		Rungs          map[string]float64 `json:"rungs"`
		CenterY        map[string]float64 `json:"centerY"`
	} `json:"tower"`
	Outpost struct {
		Width         float64               `json:"width"`
		ChuteOpening  Opening               `json:"chuteOpening"`
		FloorOpening  Opening               `json:"floorOpening"`
		ChuteCapacity int                   `json:"chuteCapacity"`
		OpeningY      float64               `json:"openingY"` // blue-frame y of the chute and floor openings
		Range         map[string][2]float64 `json:"range"`
	} `json:"outpost"`
	Depot struct {
		Width   float64            `json:"width"`
		Depth   float64            `json:"depth"`
		CenterY map[string]float64 `json:"centerY"`
		Count   int                `json:"count"`
	} `json:"depot"`
	DriverStation struct {
		Centers map[string]float64 `json:"centers"`
	} `json:"driverStation"`
	Fuel struct {
		Diameter        float64 `json:"diameter"`
		Mass            float64 `json:"mass"`
		Total           int     `json:"total"`
		PreloadPerRobot int     `json:"preloadPerRobot"`
		NeutralBox      struct {
			CenterX float64 `json:"centerX"`
			CenterY float64 `json:"centerY"`
			LengthX float64 `json:"lengthX"`
			WidthY  float64 `json:"widthY"`
		} `json:"neutralBox"`
	} `json:"fuel"`
	AprilTags []AprilTag `json:"aprilTags"`
}

// AprilTag is one field tag's pose: centre position and orientation as a WPILib quaternion (w, x, y, z). The tag's
// face points along its +X axis.
type AprilTag struct {
	Id int        `json:"id"`
	X  float64    `json:"x"`
	Y  float64    `json:"y"`
	Z  float64    `json:"z"`
	Q  [4]float64 `json:"q"`
}

// Yaw is the direction the tag faces in the field plane.
func (tag AprilTag) Yaw() float64 { return 2 * math.Atan2(tag.Q[3], tag.Q[0]) }

type Opening struct {
	Width  float64 `json:"width"`
	Height float64 `json:"height"`
	Bottom float64 `json:"bottom"`
}

// LoadLayout reads the shared field layout from the embedded web assets.
func LoadLayout(files fs.FS) (*Layout, error) {
	data, err := fs.ReadFile(files, "layouts/field-2026.json")
	if err != nil {
		return nil, err
	}
	layout := new(Layout)
	if err := json.Unmarshal(data, layout); err != nil {
		return nil, err
	}
	return layout, nil
}

// mirror converts a blue-frame point into the red half (180° rotation about the field center).
func (layout *Layout) mirror(x, y float64) (float64, float64) {
	return layout.Field.Length - x, layout.Field.Width - y
}

// onAlliance maps a blue-frame point onto the given alliance's half.
func (layout *Layout) onAlliance(alliance int, x, y float64) (float64, float64) {
	if alliance == Red {
		return layout.mirror(x, y)
	}
	return x, y
}
