package centralserver

import (
	"embed"
	"io/fs"
	"net/http"
)

// UI assets (docs/05-recipe-and-crd.md §5.3 / docs/07-open-questions.md):
// a small vanilla-JS single page, embedded into the binary so the central
// service stays a single deployable component — no separate frontend
// service, build pipeline, or CDN dependency (same "lightest that fits"
// reasoning as Starlark and bbolt elsewhere in this project).
//
//go:embed ui
var uiFS embed.FS

func (s *Server) mountUI(mux *http.ServeMux) {
	sub, err := fs.Sub(uiFS, "ui")
	if err != nil {
		// The embedded FS is compiled in; failing to find "ui" under it
		// means the embed directive itself is broken — a build defect,
		// not a runtime condition to handle gracefully.
		panic(err)
	}
	mux.Handle("/", http.FileServer(http.FS(sub)))
}
