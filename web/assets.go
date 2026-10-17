// Package web embeds the simulator's browser front end (3D venue and tool panels).
package web

import "embed"

// The field CAD mesh is deliberately not embedded: users import their own (internal/gui/fieldmodel.go).
//
//go:embed *.html css js vendor layouts models/human.fbx
var Files embed.FS
