package api

import (
	"bytes"
	"io/fs"
	"net/http"
	"strings"
	"time"
)

// webUIHandler serves the web bundle injected via WithWebUI: real files
// by path, index.html for "/" and as the SPA fallback for every other
// path. It is registered as the mux's "/" catch-all, so all the /api/*
// patterns above it win on specificity; the explicit /api guard here
// only catches API paths that match no registered pattern, which must
// stay JSON 404s instead of turning into the SPA page.
func (s *Server) webUIHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			// The bundle is read-only; mirror the API's JSON 405 shape.
			w.Header().Set("Allow", "GET, HEAD")
			writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		p := cleanRequestPath(r.URL.Path)
		if p == "/api" || strings.HasPrefix(p, "/api/") {
			writeError(w, http.StatusNotFound, "not found")
			return
		}
		if name := strings.TrimPrefix(p, "/"); name != "" {
			if data, err := fs.ReadFile(s.ui, name); err == nil {
				serveUIFile(w, r, name, data)
				return
			}
		}
		// "/" and any path that is not a bundled file get index.html with
		// 200: the SPA owns routing (hash-based), so an unknown path must
		// load the app rather than 404.
		data, err := fs.ReadFile(s.ui, "index.html")
		if err != nil {
			s.logger.Printf("webui: read index.html: %v", err)
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
		serveUIFile(w, r, "index.html", data)
	})
}

// serveUIFile writes one bundle file with caching fit for a Vite build:
// files under assets/ carry a content hash in their name and are
// immutable for a year, while everything else (index.html, root-level
// icons) must revalidate on every load so a new release is picked up
// immediately. ServeContent supplies the Content-Type from the file
// extension plus HEAD and Range handling; the zero modtime suppresses
// Last-Modified, matching embed.FS's lack of file times.
func serveUIFile(w http.ResponseWriter, r *http.Request, name string, data []byte) {
	if strings.HasPrefix(name, "assets/") {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	} else {
		w.Header().Set("Cache-Control", "no-cache")
	}
	http.ServeContent(w, r, name, time.Time{}, bytes.NewReader(data))
}
