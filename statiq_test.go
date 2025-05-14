package statiq_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	statiq "github.com/hhftechnology/statiq"
)

func TestStatiqBasicServing(t *testing.T) {
	t.Parallel()

	// Create config with current directory as root
	cfg := statiq.CreateConfig()
	
	// Create a next handler that should never be called
	next := http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		// Should NEVER go through the next handler
		t.Fatal("next handler was called unexpectedly")
	})

	// Create the handler
	handler, err := statiq.New(context.Background(), next, cfg, "statiq")
	if err != nil {
		t.Fatal(err)
	}

	// Create a test recorder
	recorder := httptest.NewRecorder()

	// Request the statiq.go file
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "http://localhost/statiq.go", nil)
	if err != nil {
		t.Fatal(err)
	}

	// Serve the request
	handler.ServeHTTP(recorder, req)

	// Verify response code
	if recorder.Code != http.StatusOK {
		t.Errorf("invalid recorder status code, expected: %d, got: %d", http.StatusOK, recorder.Code)
	}

	// Verify content type
	if recorder.Header().Get("Content-Type") != "text/x-go; charset=utf-8" {
		t.Errorf("invalid Content-Type, expected: %q, got: %q",
			"text/x-go; charset=utf-8", recorder.Header().Get("Content-Type"))
	}
}

func TestStatiqWithCustomRoot(t *testing.T) {
	t.Parallel()
	
	// Create a temporary directory
	tempDir, err := os.MkdirTemp("", "statiq-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)
	
	// Create a test file in the temp directory
	testFilePath := filepath.Join(tempDir, "test.txt")
	testContent := "Hello, Statiq!"
	if err := os.WriteFile(testFilePath, []byte(testContent), 0644); err != nil {
		t.Fatal(err)
	}
	
	// Configure Statiq with the temp directory as root
	cfg := statiq.CreateConfig()
	cfg.Root = tempDir
	
	// Create a next handler that should never be called
	next := http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		t.Fatal("next handler was called unexpectedly")
	})
	
	// Create the handler
	handler, err := statiq.New(context.Background(), next, cfg, "statiq")
	if err != nil {
		t.Fatal(err)
	}
	
	// Create a test recorder
	recorder := httptest.NewRecorder()
	
	// Request the test file
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "http://localhost/test.txt", nil)
	if err != nil {
		t.Fatal(err)
	}
	
	// Serve the request
	handler.ServeHTTP(recorder, req)
	
	// Verify response code
	if recorder.Code != http.StatusOK {
		t.Errorf("invalid recorder status code, expected: %d, got: %d", http.StatusOK, recorder.Code)
	}
	
	// Verify content
	if recorder.Body.String() != testContent {
		t.Errorf("invalid body content, expected: %q, got: %q", testContent, recorder.Body.String())
	}
}

func TestIndexFiles(t *testing.T) {
	t.Parallel()
	
	// Create a temporary directory structure
	tempDir, err := os.MkdirTemp("", "statiq-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)
	
	// Create a subdirectory
	subDir := filepath.Join(tempDir, "subdir")
	if err := os.Mkdir(subDir, 0755); err != nil {
		t.Fatal(err)
	}
	
	// Create a custom index file
	indexPath := filepath.Join(subDir, "custom.html")
	indexContent := "<html><body>Custom Index</body></html>"
	if err := os.WriteFile(indexPath, []byte(indexContent), 0644); err != nil {
		t.Fatal(err)
	}
	
	// Configure Statiq with custom index files
	cfg := statiq.CreateConfig()
	cfg.Root = tempDir
	cfg.IndexFiles = []string{"custom.html", "index.html"}
	
	// Create the handler
	handler, err := statiq.New(context.Background(), next(t), cfg, "statiq")
	if err != nil {
		t.Fatal(err)
	}
	
	// Test directory request with trailing slash
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "http://localhost/subdir/", nil)
	if err != nil {
		t.Fatal(err)
	}
	
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)
	
	if recorder.Code != http.StatusMovedPermanently {
		t.Errorf("Expected redirect for directory, got status code %d", recorder.Code)
	}
	
	// Get the redirect location and follow it
	location := recorder.Header().Get("Location")
	if location != "/subdir/custom.html" {
		t.Errorf("Expected redirect to /subdir/custom.html, got %s", location)
	}
	
	req, err = http.NewRequestWithContext(context.Background(), http.MethodGet, "http://localhost"+location, nil)
	if err != nil {
		t.Fatal(err)
	}
	
	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)
	
	if recorder.Code != http.StatusOK {
		t.Errorf("Expected 200 OK, got %d", recorder.Code)
	}
	
	if recorder.Body.String() != indexContent {
		t.Errorf("Expected index content, got %s", recorder.Body.String())
	}
}

func TestSPAMode(t *testing.T) {
	t.Parallel()
	
	// Create a temporary directory
	tempDir, err := os.MkdirTemp("", "statiq-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)
	
	// Create an index.html file for SPA
	spaContent := "<html><body>SPA Root</body></html>"
	if err := os.WriteFile(filepath.Join(tempDir, "index.html"), []byte(spaContent), 0644); err != nil {
		t.Fatal(err)
	}
	
	// Create a real file that should be served directly
	realFileContent := "This is a real file"
	if err := os.WriteFile(filepath.Join(tempDir, "real.txt"), []byte(realFileContent), 0644); err != nil {
		t.Fatal(err)
	}
	
	// Configure Statiq with SPA mode
	cfg := statiq.CreateConfig()
	cfg.Root = tempDir
	cfg.SPAMode = true
	
	// Create the handler
	handler, err := statiq.New(context.Background(), next(t), cfg, "statiq")
	if err != nil {
		t.Fatal(err)
	}
	
	// Test real file request
	recorder := httptest.NewRecorder()
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "http://localhost/real.txt", nil)
	if err != nil {
		t.Fatal(err)
	}
	
	handler.ServeHTTP(recorder, req)
	
	if recorder.Code != http.StatusOK {
		t.Errorf("Expected 200 OK for real file, got %d", recorder.Code)
	}
	
	if recorder.Body.String() != realFileContent {
		t.Errorf("Expected real file content, got %s", recorder.Body.String())
	}
	
	// Test non-existent route that should fall back to index.html
	recorder = httptest.NewRecorder()
	req, err = http.NewRequestWithContext(context.Background(), http.MethodGet, "http://localhost/non-existent-route", nil)
	if err != nil {
		t.Fatal(err)
	}
	
	handler.ServeHTTP(recorder, req)
	
	if recorder.Code != http.StatusOK {
		t.Errorf("Expected 200 OK for SPA route, got %d", recorder.Code)
	}
	
	if recorder.Body.String() != spaContent {
		t.Errorf("Expected SPA content, got %s", recorder.Body.String())
	}
}

func TestCustomErrorPage(t *testing.T) {
	t.Parallel()
	
	// Create a temporary directory
	tempDir, err := os.MkdirTemp("", "statiq-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)
	
	// Create a custom 404 page
	errorContent := "<html><body>Custom 404 Error</body></html>"
	if err := os.WriteFile(filepath.Join(tempDir, "404.html"), []byte(errorContent), 0644); err != nil {
		t.Fatal(err)
	}
	
	// Configure Statiq with custom error page
	cfg := statiq.CreateConfig()
	cfg.Root = tempDir
	cfg.ErrorPage404 = "404.html"
	
	// Create the handler
	handler, err := statiq.New(context.Background(), next(t), cfg, "statiq")
	if err != nil {
		t.Fatal(err)
	}
	
	// Test non-existent file request
	recorder := httptest.NewRecorder()
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "http://localhost/non-existent.txt", nil)
	if err != nil {
		t.Fatal(err)
	}
	
	handler.ServeHTTP(recorder, req)
	
	// Should serve the custom error page with 200 status
	if recorder.Code != http.StatusOK {
		t.Errorf("Expected 200 OK for custom error page, got %d", recorder.Code)
	}
	
	if !strings.Contains(recorder.Body.String(), "Custom 404 Error") {
		t.Errorf("Expected custom error content, got %s", recorder.Body.String())
	}
}

func TestCacheControl(t *testing.T) {
	t.Parallel()
	
	// Create a temporary directory
	tempDir, err := os.MkdirTemp("", "statiq-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)
	
	// Create test files with different extensions
	if err := os.WriteFile(filepath.Join(tempDir, "test.html"), []byte("<html></html>"), 0644); err != nil {
		t.Fatal(err)
	}
	
	if err := os.WriteFile(filepath.Join(tempDir, "test.css"), []byte("body {}"), 0644); err != nil {
		t.Fatal(err)
	}
	
	// Configure Statiq with cache control settings
	cfg := statiq.CreateConfig()
	cfg.Root = tempDir
	cfg.CacheControl = map[string]string{
		".html": "max-age=3600",
		".css":  "max-age=86400",
		"*":     "max-age=600",
	}
	
	// Create the handler
	handler, err := statiq.New(context.Background(), next(t), cfg, "statiq")
	if err != nil {
		t.Fatal(err)
	}
	
	// Test HTML file
	recorder := httptest.NewRecorder()
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "http://localhost/test.html", nil)
	if err != nil {
		t.Fatal(err)
	}
	
	handler.ServeHTTP(recorder, req)
	
	if recorder.Header().Get("Cache-Control") != "max-age=3600" {
		t.Errorf("Expected Cache-Control: max-age=3600 for HTML, got %s", recorder.Header().Get("Cache-Control"))
	}
	
	// Test CSS file
	recorder = httptest.NewRecorder()
	req, err = http.NewRequestWithContext(context.Background(), http.MethodGet, "http://localhost/test.css", nil)
	if err != nil {
		t.Fatal(err)
	}
	
	handler.ServeHTTP(recorder, req)
	
	if recorder.Header().Get("Cache-Control") != "max-age=86400" {
		t.Errorf("Expected Cache-Control: max-age=86400 for CSS, got %s", recorder.Header().Get("Cache-Control"))
	}
}

func TestDirectoryListing(t *testing.T) {
	t.Parallel()
	
	// Create a temporary directory
	tempDir, err := os.MkdirTemp("", "statiq-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)
	
	// Create a subdirectory
	subDir := filepath.Join(tempDir, "subdir")
	if err := os.Mkdir(subDir, 0755); err != nil {
		t.Fatal(err)
	}
	
	// Create a file in the subdirectory
	if err := os.WriteFile(filepath.Join(subDir, "test.txt"), []byte("test"), 0644); err != nil {
		t.Fatal(err)
	}
	
	// Test with directory listing disabled (default)
	cfg := statiq.CreateConfig()
	cfg.Root = tempDir
	
	handler, err := statiq.New(context.Background(), next(t), cfg, "statiq")
	if err != nil {
		t.Fatal(err)
	}
	
	recorder := httptest.NewRecorder()
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "http://localhost/subdir/", nil)
	if err != nil {
		t.Fatal(err)
	}
	
	handler.ServeHTTP(recorder, req)
	
	// Should return 404 when listing is disabled
	if recorder.Code != http.StatusNotFound {
		t.Errorf("Expected 404 Not Found for disabled directory listing, got %d", recorder.Code)
	}
	
	// Test with directory listing enabled
	cfg = statiq.CreateConfig()
	cfg.Root = tempDir
	cfg.EnableDirectoryListing = true
	
	handler, err = statiq.New(context.Background(), next(t), cfg, "statiq")
	if err != nil {
		t.Fatal(err)
	}
	
	recorder = httptest.NewRecorder()
	req, err = http.NewRequestWithContext(context.Background(), http.MethodGet, "http://localhost/subdir/", nil)
	if err != nil {
		t.Fatal(err)
	}
	
	handler.ServeHTTP(recorder, req)
	
	// Should return 200 when listing is enabled
	if recorder.Code != http.StatusOK {
		t.Errorf("Expected 200 OK for enabled directory listing, got %d", recorder.Code)
	}
	
	// Directory listing should contain the filename
	body := recorder.Body.String()
	if !strings.Contains(body, "test.txt") {
		t.Errorf("Directory listing should contain 'test.txt', got: %s", body)
	}
}

// Helper function to create a next handler that fails the test if called
func next(t *testing.T) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Helper()
		t.Fatal("next handler was called unexpectedly")
	})
}