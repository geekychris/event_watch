// Package web serves the embedded htmx UI.
package web

import (
	"embed"
	"io/fs"
	"net/http"
)

//go:embed all:static all:templates
var assets embed.FS

// Handler serves the UI at "/" (index.html) and static assets under
// /static/. Everything is embedded — no filesystem access at runtime.
func Handler() http.Handler {
	staticFS, err := fs.Sub(assets, "static")
	if err != nil {
		panic(err)
	}
	tmplFS, err := fs.Sub(assets, "templates")
	if err != nil {
		panic(err)
	}
	mux := http.NewServeMux()
	mux.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.FS(staticFS))))
	mux.Handle("/", http.FileServer(http.FS(tmplFS)))
	return mux
}
