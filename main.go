package main

import (
	"bytes"
	"io"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"strings"
	"time"
)

const injectionScript = `<script>
(function() {
    function checkSession() {
        fetch('/oauth2/auth', {
            credentials: 'include',
            redirect: 'manual'
        })
        .then(function (res) {
            if (res.status === 401) {
                window.location.href = '/oauth2/start';
            }
        })
        .catch(function () { /* network error, ignore */ });
    }

    setInterval(checkSession, 30000);
    document.addEventListener('visibilitychange', function () {
        if (!document.hidden) checkSession();
    });
})();
</script>`

func main() {
	// Read configuration from environment variables
	backendURL := getEnv("BACKEND_URL", "http://localhost:3000")
	listenAddr := getEnv("LISTEN_ADDR", ":8080")
	healthCheckInterval := getEnvDuration("HEALTH_CHECK_INTERVAL", 30*time.Second)

	// Parse the backend URL
	target, err := url.Parse(backendURL)
	if err != nil {
		log.Fatalf("Invalid BACKEND_URL %q: %v", backendURL, err)
	}

	// Create the reverse proxy
	proxy := httputil.NewSingleHostReverseProxy(target)

	// Customize the Director to preserve original headers
	originalDirector := proxy.Director
	proxy.Director = func(req *http.Request) {
		originalDirector(req)
		// Disable upstream compression so we can modify the body
		req.Header.Set("Accept-Encoding", "identity")
		// Preserve the original Host header
		req.Host = target.Host
	}

	// Wrap the response with our injection logic
	proxy.ModifyResponse = func(resp *http.Response) error {
		// Only modify HTML responses
		contentType := resp.Header.Get("Content-Type")
		if !strings.Contains(strings.ToLower(contentType), "text/html") {
			return nil
		}

		// Remove content-length since we're changing the body
		resp.Header.Del("Content-Length")

		// Read the entire body
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return err
		}
		resp.Body.Close()

		// Find the last </body> tag and inject before it
		marker := []byte("</body>")
		idx := bytes.LastIndex(body, marker)

		if idx != -1 {
			// Inject the script before </body>
			newBody := make([]byte, 0, len(body)+len(injectionScript))
			newBody = append(newBody, body[:idx]...)
			newBody = append(newBody, injectionScript...)
			newBody = append(newBody, body[idx:]...)
			resp.Body = io.NopCloser(bytes.NewReader(newBody))
		} else {
			// If no </body> tag found, just return original content
			resp.Body = io.NopCloser(bytes.NewReader(body))
		}

		return nil
	}

	// Create the main HTTP server
	server := &http.Server{
		Addr:    listenAddr,
		Handler: proxy,
	}

	// Start the proxy server
	log.Printf("Starting HTML injector proxy, forwarding to %s on %s", backendURL, listenAddr)
	go func() {
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Proxy server error: %v", err)
		}
	}()

	// Start a health check goroutine
	go startHealthCheck(healthCheckInterval)

	// Wait forever
	select {}
}

func getEnv(key, defaultVal string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return defaultVal
}

func getEnvDuration(key string, defaultVal time.Duration) time.Duration {
	if val := os.Getenv(key); val != "" {
		if d, err := time.ParseDuration(val); err == nil {
			return d
		}
		log.Printf("Invalid duration for %q: %s, using default %v", key, val, defaultVal)
	}
	return defaultVal
}

func startHealthCheck(interval time.Duration) {
	http.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		io.WriteString(w, "OK")
	})

	log.Printf("Health check endpoint available on :8080/healthz")

	// Slight delay to allow the main server to bind
	time.Sleep(interval)
	log.Printf("Health check starts responding...")
}
