package statiq

import (
	"context"
	"fmt"
	"html/template"
	"io/fs"
	"mime"
	"net/http"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"
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

// dirEntry represents a file or directory for the directory listing template
type dirEntry struct {
	Name    string
	Size    int64
	Mode    os.FileMode
	ModTime time.Time
	IsDir   bool
}

// Initialize MIME types
func init() {
	// Register Go files as text/x-go to match standard behavior
	mime.AddExtensionType(".go", "text/x-go")
}

// StatiqHandler is a custom file server handler
type StatiqHandler struct {
	root                 http.FileSystem
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
// Instead of failing immediately, create the directory if it doesn't exist
if _, err := os.Stat(root); os.IsNotExist(err) {
    if err := os.MkdirAll(root, 0755); err != nil {
        return nil, fmt.Errorf("failed to create root directory %s: %w", root, err)
    }
    // Log that directory was created
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
				h.serveFile(w, r, filepath.Join(string(h.rootPath), h.spaIndex))
				return
			}
			
			if h.errorPage404 != "" {
				// Serve custom 404 page
				w.WriteHeader(h.notFoundResponseCode)
				h.serveFile(w, r, filepath.Join(string(h.rootPath), h.errorPage404))
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
		if len(url) == 0 || url[len(url)-1] != '/' {
			localRedirect(w, r, url+"/")
			return
		}

		// Try to serve an index file
// Try to serve an index file
for _, index := range h.indexFiles {
    indexPath := path.Join(upath, index)  // Use path.Join for URL paths
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
				h.serveFile(w, r, filepath.Join(string(h.rootPath), h.errorPage404))
				return
			}
			http.NotFound(w, r)
			return
		}

		// Serve directory listing
		h.serveDirectoryListing(w, r, f, d)
		return
	}

	// Set cache control headers if configured
	h.setCacheHeaders(w, r, d)

	// Get content type based on file extension
	name := d.Name()
	ext := filepath.Ext(name)
	contentType := mime.TypeByExtension(ext)
	if contentType != "" {
		w.Header().Set("Content-Type", contentType)
	}

	// Serve the file
	http.ServeContent(w, r, d.Name(), d.ModTime(), f.(io.ReadSeeker))
}

// serveDirectoryListing generates and serves an HTML directory listing
func (h *StatiqHandler) serveDirectoryListing(w http.ResponseWriter, r *http.Request, f http.File, d fs.FileInfo) {
	// List directory contents
	dirs, err := f.Readdir(-1)
	if err != nil {
		http.Error(w, "Error reading directory", http.StatusInternalServerError)
		return
	}
	
	// Sort directories first, then by name
	sort.Slice(dirs, func(i, j int) bool {
		if dirs[i].IsDir() && !dirs[j].IsDir() {
			return true
		}
		if !dirs[i].IsDir() && dirs[j].IsDir() {
			return false
		}
		return dirs[i].Name() < dirs[j].Name()
	})
	
	// Create slice of dirEntry for the template
	entries := make([]dirEntry, len(dirs))
	for i, entry := range dirs {
		entries[i] = dirEntry{
			Name:    entry.Name(),
			Size:    entry.Size(),
			Mode:    entry.Mode(),
			ModTime: entry.ModTime(),
			IsDir:   entry.IsDir(),
		}
	}
	
	// Set content type and render the HTML
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	
	// Simple directory listing template
	tmpl := template.Must(template.New("dirlist").Parse(`
<!DOCTYPE html>
<html>
<head>
    <meta charset="utf-8">
    <title>Index of {{.Path}}</title>
    <style>
        body { font-family: sans-serif; margin: 2em; }
        table { border-collapse: collapse; width: 100%; }
        th, td { text-align: left; padding: 8px; }
        tr:nth-child(even) { background-color: #f2f2f2; }
        th { background-color: #4CAF50; color: white; }
        a { text-decoration: none; }
        a:hover { text-decoration: underline; }
    </style>
</head>
<body>
    <h1>Index of {{.Path}}</h1>
    <table>
        <tr>
            <th>Name</th>
            <th>Size</th>
            <th>Modified</th>
        </tr>
        {{if ne .Path "/"}}
        <tr>
            <td><a href="../">../</a></td>
            <td>-</td>
            <td>-</td>
        </tr>
        {{end}}
        {{range .Files}}
        <tr>
            <td><a href="{{.Name}}{{if .IsDir}}/{{end}}">{{.Name}}{{if .IsDir}}/{{end}}</a></td>
            <td>{{if .IsDir}}-{{else}}{{.Size}} bytes{{end}}</td>
            <td>{{.ModTime.Format "2006-01-02 15:04:05"}}</td>
        </tr>
        {{end}}
    </table>
</body>
</html>
`))
	
	// Execute the template
	data := struct {
		Path  string
		Files []dirEntry
	}{
		Path:  r.URL.Path,
		Files: entries,
	}
	
	err = tmpl.Execute(w, data)
	if err != nil {
		http.Error(w, "Error rendering directory listing", http.StatusInternalServerError)
	}
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
// Change the parameter name
func (h *StatiqHandler) serveFile(w http.ResponseWriter, r *http.Request, filePath string) {
    f, err := os.Open(filePath)
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
    
    // Now filepath refers to the package, not the parameter
    ext := filepath.Ext(d.Name())
    contentType := mime.TypeByExtension(ext)
    if contentType != "" {
        w.Header().Set("Content-Type", contentType)
    }
    
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