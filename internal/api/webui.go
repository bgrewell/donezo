package api

import (
	"bytes"
	"html/template"
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
		serveUIFile(w, r, "index.html", s.crawlableIndex(data))
	})
}

// crawlableIndex injects a plain-HTML block naming the brand and linking the
// policy pages into the SPA shell, so a crawler that does not run JavaScript
// can find them from the site root.
//
// This is a carrier-vetting requirement, learned the hard way: the app renders
// its policy links with React, so the raw HTML a non-JS A2P verifier fetches
// from "/" contained no link to the policy at all — a stated cause of the
// privacy-policy-not-found rejection. The block is inside <noscript>, so it is
// present in the served HTML (and rendered by a non-JS client) without showing
// under the real app. It is added only when this instance publishes the pages;
// otherwise the shell is served untouched.
func (s *Server) crawlableIndex(data []byte) []byte {
	if s.operatorName == "" || s.supportEmail == "" {
		return data
	}
	block := `<noscript><footer>` +
		`<p>` + template.HTMLEscapeString(s.operatorName) +
		` operates ` + template.HTMLEscapeString(programName) +
		`, an SMS reminder service.</p>` +
		`<p><a href="/privacy">Privacy Policy</a> · ` +
		`<a href="/terms">Terms of Service</a> · ` +
		`<a href="/sms-opt-in">SMS opt-in</a></p>` +
		`</footer></noscript>`
	if idx := bytes.LastIndex(data, []byte("</body>")); idx >= 0 {
		out := make([]byte, 0, len(data)+len(block))
		out = append(out, data[:idx]...)
		out = append(out, block...)
		out = append(out, data[idx:]...)
		return out
	}
	// No </body> to anchor on (a shell we do not recognise): append rather
	// than drop the block, since a crawler reads the whole response.
	return append(data, block...)
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
