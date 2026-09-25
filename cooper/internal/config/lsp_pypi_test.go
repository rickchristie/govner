package config

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func usePyPIFixture(t *testing.T, handler http.HandlerFunc) {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	previousURL, previousSleep := pyPIBaseURL, pyPIRetrySleep
	pyPIBaseURL, pyPIRetrySleep = server.URL, func(time.Duration) {}
	t.Cleanup(func() { pyPIBaseURL, pyPIRetrySleep = previousURL, previousSleep })
}

func TestPyPIMetadataRetriesIncompleteResponses(t *testing.T) {
	for _, failure := range []string{"transport", "body", "unavailable", "rate limit"} {
		t.Run(failure, func(t *testing.T) {
			calls := 0
			usePyPIFixture(t, func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.URL.Path != "/python-lsp-server/1.15.0/json" {
					t.Errorf("request path = %q", r.URL.Path)
				}
				if calls == pyPIRequestMaxAttempts {
					fmt.Fprint(w, `{"info":{"version":"1.15.0","requires_python":">=3.9"}}`)
					return
				}
				switch failure {
				case "transport":
					connection, _, err := w.(http.Hijacker).Hijack()
					if err != nil {
						t.Error(err)
						return
					}
					connection.Close()
				case "body":
					w.Header().Set("Content-Length", "100")
					fmt.Fprint(w, "{")
				case "unavailable":
					w.WriteHeader(http.StatusServiceUnavailable)
				case "rate limit":
					w.WriteHeader(http.StatusTooManyRequests)
				}
			})
			metadata, err := ResolvePyPIPackageVersionMetadata("python-lsp-server", "1.15.0")
			if err != nil || metadata.Version != "1.15.0" || metadata.RequiresPython != ">=3.9" {
				t.Fatalf("metadata = %#v, error = %v", metadata, err)
			}
			if calls != pyPIRequestMaxAttempts {
				t.Fatalf("requests = %d, want %d", calls, pyPIRequestMaxAttempts)
			}
		})
	}
}

func TestPyPIMetadataStopsAtRetryLimit(t *testing.T) {
	calls := 0
	usePyPIFixture(t, func(w http.ResponseWriter, _ *http.Request) {
		calls++
		w.WriteHeader(http.StatusBadGateway)
	})
	_, err := ResolvePyPIPackageLatest("python-lsp-server")
	if err == nil || !strings.Contains(err.Error(), "after 3 attempts") || !strings.Contains(err.Error(), pyPIBaseURL) {
		t.Fatalf("missing bounded failure with endpoint: %v", err)
	}
	if calls != pyPIRequestMaxAttempts {
		t.Fatalf("requests = %d, want %d", calls, pyPIRequestMaxAttempts)
	}
}

func TestPyPIMetadataDoesNotRetryDefinitiveErrors(t *testing.T) {
	for _, status := range []int{http.StatusNotFound, http.StatusForbidden, http.StatusOK} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			calls := 0
			usePyPIFixture(t, func(w http.ResponseWriter, _ *http.Request) {
				calls++
				w.WriteHeader(status)
				fmt.Fprint(w, "invalid metadata")
			})
			if _, err := ResolvePyPIPackageLatest("python-lsp-server"); err == nil {
				t.Fatal("invalid metadata was accepted")
			}
			if calls != 1 {
				t.Fatalf("definitive error retried %d times", calls)
			}
		})
	}
}
