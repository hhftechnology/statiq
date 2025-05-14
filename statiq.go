// Package statiq builds a middleware that works like a static file server
package statiq

import (
	"context"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"io"
)

// Config the plugin configuration.
type Config struct {
	// Root directory to serve files from
	Root string `json:"root,omitempty"`

	// EnableDirectoryListing enables directory listing
	EnableDirectoryListing bool `json:"enableDirectoryListing,omitempty"`

	// IndexFiles is a list of filenames to try when a directory is requested
	IndexFiles []string `json:"indexFiles,omitempty"`

	// SPAMode redirects all not-found requests to a single page
	SPAMode bool `json:"spaMode,omitempty"`

	// SPAIndex is the file to serve in SPA mode
	SPAIndex string `json:"spaIndex,omitempty"`

	// ErrorPage404 is the path to a custom 404 error page
	ErrorPage404 string `json:"errorPage404,omitempty"`

	// CacheControl sets cache control headers for static files
	CacheControl map[string]string `json:"cacheControl,omitempty"`
}

// CreateConfig creates the default plugin configuration.
func CreateConfig() *Config {
	return &Config{
		Root:                  ".",
		EnableDirectoryListing: false,
		IndexFiles:            []string{"index.html", "index.htm"},
		SPAMode:               false,
		SPAIndex:              "index.html",
		ErrorPage404:          "",
		CacheControl:          map[string]string{},
	}
}

// StatiqHandler is a custom file server handler
type StatiqHandler struct {
	root                 http.Dir
	rootPath             string
	enableDirListing     bool
	indexFiles           []string
	spaMode              bool
	spaIndex             string
	errorPage404         string
	cacheControl         map[string]string
	notFoundResponseCode int
}

// New creates a new Statiq plugin.
func New(_ context.Context, next http.Handler, config *Config, _ string) (http.Handler, error) {
	// Ensure the root path is absolute
	root, err := filepath.Abs(config.Root)
	if err != nil {
		return nil, fmt.Errorf("invalid root path: %w", err)
	}

	// Verify the directory exists
	if _, err := os.Stat(root); os.IsNotExist(err) {
		return nil, fmt.Errorf("root directory does not exist: %s", root)
	}

	// Check if custom 404 page exists
	notFoundResponseCode := http.StatusNotFound
	if config.ErrorPage404 != "" {
		errorPagePath := filepath.Join(root, config.ErrorPage404)
		_, err := os.Stat(errorPagePath)
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("error page not found: %s", errorPagePath)
		}
		notFoundResponseCode = http.StatusOK // We'll serve the error page with 200 OK
	}

	// Create a custom handler
	handler := &StatiqHandler{
		root:                 http.Dir(root),
		rootPath:             root,
		enableDirListing:     config.EnableDirectoryListing,
		indexFiles:           config.IndexFiles,
		spaMode:              config.SPAMode,
		spaIndex:             config.SPAIndex,
		errorPage404:         config.ErrorPage404,
		cacheControl:         config.CacheControl,
		notFoundResponseCode: notFoundResponseCode,
	}

	// Return our custom handler
	return handler, nil
}

// ServeHTTP serves HTTP requests with static files
func (h *StatiqHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Clean the path
	upath := r.URL.Path
	if !strings.HasPrefix(upath, "/") {
		upath = "/" + upath
	}
	
	// Try to open the file
	f, err := h.root.Open(upath)
	if err != nil {
		// Handle not found
		if os.IsNotExist(err) {
			if h.spaMode {
				// In SPA mode, serve the SPA index file
				h.serveFile(w, r, filepath.Join(string(h.root), h.spaIndex))
				return
			}
			
			if h.errorPage404 != "" {
				// Serve custom 404 page
				w.WriteHeader(h.notFoundResponseCode)
				h.serveFile(w, r, filepath.Join(string(h.root), h.errorPage404))
				return
			}
			
			http.NotFound(w, r)
			return
		}
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}
	defer f.Close()

	// Get file info
	d, err := f.Stat()
	if err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	// Handle directory
	if d.IsDir() {
		// Redirect if the directory name doesn't end in a slash
		url := r.URL.Path
		if url[len(url)-1] != '/' {
			localRedirect(w, r, url+"/")
			return
		}

		// Try to serve an index file
		for _, index := range h.indexFiles {
			indexPath := filepath.Join(upath, index)
			indexFile, err := h.root.Open(indexPath)
			if err == nil {
				indexFile.Close()
				localRedirect(w, r, indexPath)
				return
			}
		}

		// If directory listing is disabled, return 404
		if !h.enableDirListing {
			if h.errorPage404 != "" {
				w.WriteHeader(h.notFoundResponseCode)
				h.serveFile(w, r, filepath.Join(string(h.root), h.errorPage404))
				return
			}
			http.NotFound(w, r)
			return
		}

		// Serve directory listing
		dirList := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.ServeContent(w, r, d.Name(), d.ModTime(), f.(io.ReadSeeker))
		})
		dirList.ServeHTTP(w, r)
		return
	}

	// Set cache control headers if configured
	h.setCacheHeaders(w, r, d)

	// Serve the file
	http.ServeContent(w, r, d.Name(), d.ModTime(), f.(io.ReadSeeker))
}

// setCacheHeaders sets cache control headers based on file extension
func (h *StatiqHandler) setCacheHeaders(w http.ResponseWriter, r *http.Request, d fs.FileInfo) {
	// Get file extension
	ext := filepath.Ext(d.Name())
	
	// Check if we have a cache control setting for this extension
	if maxAge, ok := h.cacheControl[ext]; ok {
		w.Header().Set("Cache-Control", maxAge)
	} else if maxAge, ok := h.cacheControl["*"]; ok {
		// Use default setting if available
		w.Header().Set("Cache-Control", maxAge)
	} else {
		// Default cache control
		w.Header().Set("Cache-Control", "max-age=86400") // 24 hours
	}
	
	// Set Last-Modified header
	w.Header().Set("Last-Modified", d.ModTime().UTC().Format(http.TimeFormat))
}

// serveFile serves a file directly from the filesystem
func (h *StatiqHandler) serveFile(w http.ResponseWriter, r *http.Request, filepath string) {
	f, err := os.Open(filepath)
	if err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	defer f.Close()

	d, err := f.Stat()
	if err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	h.setCacheHeaders(w, r, d)
	http.ServeContent(w, r, d.Name(), d.ModTime(), f)
}

// localRedirect gives a Moved Permanently response
func localRedirect(w http.ResponseWriter, r *http.Request, newPath string) {
	if q := r.URL.RawQuery; q != "" {
		newPath += "?" + q
	}
	w.Header().Set("Location", newPath)
	w.WriteHeader(http.StatusMovedPermanently)
}