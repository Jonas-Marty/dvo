package devops

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func testClient(srv *httptest.Server) *Client {
	c := NewClient(srv.URL)
	c.TokenSource = func(context.Context) (string, error) { return "tok", nil }
	c.backoff = time.Millisecond
	return c
}

func TestClientSendsAuthAndAPIVersion(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer tok" {
			t.Errorf("Authorization = %q", r.Header.Get("Authorization"))
		}
		if r.URL.Query().Get("api-version") != "7.1" {
			t.Errorf("api-version = %q", r.URL.Query().Get("api-version"))
		}
		w.Write([]byte(`{"a":1}`))
	}))
	defer srv.Close()

	var got struct{ A int }
	if err := testClient(srv).Get(context.Background(), "/x", nil, &got); err != nil {
		t.Fatal(err)
	}
	if got.A != 1 {
		t.Fatalf("got %+v", got)
	}
}

func TestClientRetriesTransientErrors(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	if err := testClient(srv).Get(context.Background(), "/x", nil, nil); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 3 {
		t.Fatalf("calls = %d, want 3", calls.Load())
	}
}

func TestClientRefreshesTokenOnceOn401(t *testing.T) {
	var issued atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer tok2" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	c := testClient(srv)
	c.TokenSource = func(context.Context) (string, error) {
		return "tok" + string(rune('0'+issued.Add(1))), nil
	}
	if err := c.Get(context.Background(), "/x", nil, nil); err != nil {
		t.Fatal(err)
	}
	if issued.Load() != 2 {
		t.Fatalf("tokens issued = %d, want 2", issued.Load())
	}
}

func TestClientReportsErrorMessage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"message":"VS402337: too many"}`))
	}))
	defer srv.Close()

	err := testClient(srv).Post(context.Background(), "/x", map[string]string{"q": "1"}, nil)
	if err == nil || !strings.Contains(err.Error(), "VS402337: too many") {
		t.Fatalf("err = %v", err)
	}
}

func TestClientStopsOnCancelledContext(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := testClient(srv).Get(ctx, "/x", nil, nil); err == nil {
		t.Fatal("expected an error from a cancelled context")
	}
}

func TestDiscoverTenantReadsResourceTenantHeader(t *testing.T) {
	for header, want := range map[string]string{
		"aaaa-bbbb":                            "aaaa-bbbb",
		"aaaa-bbbb,cccc-dddd":                  "aaaa-bbbb",
		"00000000-0000-0000-0000-000000000000": "",
		"":                                     "",
	} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("Authorization") != "" {
				t.Error("tenant discovery must not send credentials")
			}
			if header != "" {
				w.Header().Set("X-VSS-ResourceTenant", header)
			}
			w.WriteHeader(http.StatusUnauthorized)
		}))
		if got := testClient(srv).discoverTenant(context.Background()); got != want {
			t.Errorf("header %q: tenant = %q, want %q", header, got, want)
		}
		srv.Close()
	}
}

func TestClientFallsBackToPATWhenTokenIsRejected(t *testing.T) {
	var auths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		auths = append(auths, auth)
		if !strings.HasPrefix(auth, "Basic ") {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	c := testClient(srv)
	c.PAT = "secret"
	for i := 0; i < 2; i++ {
		if err := c.Get(context.Background(), "/x", nil, nil); err != nil {
			t.Fatal(err)
		}
	}
	// bearer, refreshed bearer, then PAT; the second call goes straight to PAT.
	if len(auths) != 4 || !strings.HasPrefix(auths[2], "Basic ") || !strings.HasPrefix(auths[3], "Basic ") {
		t.Errorf("auth sequence = %v", auths)
	}
}

func TestClientWithoutPATReportsHowToSignIn(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"message":"TF400813: not authorized"}`))
	}))
	defer srv.Close()
	err := testClient(srv).Get(context.Background(), "/x", nil, nil)
	if err == nil || !strings.Contains(err.Error(), "az login --tenant") {
		t.Errorf("err = %v", err)
	}
}

func TestAcceptsDistinguishesTenants(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer good" {
			http.Redirect(w, r, "/_signin", http.StatusFound)
			return
		}
		w.Write([]byte(`{}`))
	}))
	defer srv.Close()
	c := testClient(srv)
	if !c.accepts(context.Background(), "good") || c.accepts(context.Background(), "bad") {
		t.Error("accepts must be true only for the token the service takes")
	}
}

func TestRejectedTenantHintIsDropped(t *testing.T) {
	c := NewClient("https://example.invalid")
	c.token, c.Tenant, c.TenantIsHint = "tok", "cached", true
	c.invalidate("tok")
	if c.Tenant != "" || c.TenantIsHint {
		t.Errorf("a rejected cached tenant must be re-detected: %q", c.Tenant)
	}

	c.token, c.Tenant, c.TenantIsHint = "tok", "explicit", false
	c.invalidate("tok")
	if c.Tenant != "explicit" {
		t.Error("an explicit tenant must never be dropped")
	}
}
