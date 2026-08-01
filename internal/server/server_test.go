package server

import (
	"bytes"
	"context"
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/feeds"
)

type fakeFeedGenerator struct {
	*http.ServeMux
}

func newFeedGenerator(t *testing.T, items []*feeds.Item) *fakeFeedGenerator {
	feed := &feeds.Feed{
		Title: "test feed",
		Link:  &feeds.Link{Href: "localhost"},
	}
	feed.Items = items
	fg := &fakeFeedGenerator{http.NewServeMux()}
	rss, err := feed.ToRss()
	if err != nil {
		t.Fatalf("feed.ToRss failed: %v", err)
	}
	fg.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, rss)
	})
	return fg
}

func startTestServer(t *testing.T, h http.Handler) string {
	t.Helper()
	addr, err := net.ResolveTCPAddr("tcp", "localhost:0")
	if err != nil {
		t.Fatalf("net.ResolveTCPAddr failed: %v", err)
	}
	l, err := net.ListenTCP("tcp", addr)
	if err != nil {
		t.Fatalf("net.ListenTCP failed: %v", err)
	}
	s := &http.Server{Handler: h}
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		s.Serve(l)
	}()
	t.Cleanup(func() {
		s.Shutdown(context.Background())
		wg.Wait()
	})
	return "http://" + l.Addr().String()
}

func TestOpen(t *testing.T) {
	// Disable AI analysis for this test
	t.Setenv("GEMINI_API_KEY", "")

	link := &feeds.Link{Href: "http://localhost"}
	tests := []struct {
		desc     string
		override Override
		items    []*feeds.Item
		detail   string
		open     bool
	}{{
		desc:  "default open",
		items: []*feeds.Item{},
		open:  true,
	}, {
		desc: "explicitly closed",
		items: []*feeds.Item{{
			Title: "Closed - 124th",
			Link:  link,
		}},
		open:   false,
		detail: "Closed - 124th",
	}, {
		desc: "explicitly open",
		items: []*feeds.Item{{
			Title: "Open - 124th",
			Link:  link,
		}},
		open:   true,
		detail: "Open - 124th",
	}, {
		desc:     "override to open",
		override: Open,
		items: []*feeds.Item{{
			Title: "Closed - 124th",
			Link:  link,
		}},
		open: true,
	}, {
		desc:     "override to closed",
		override: Closed,
		items: []*feeds.Item{{
			Title: "Open - 124th",
			Link:  link,
		}},
		open: false,
	}, {
		desc: "latest update",
		items: []*feeds.Item{{
			Title: "Open - Some other road",
			Link:  link,
		}, {
			Title: "Closed - 124th",
			Link:  link,
		}, {
			Title: "Open - 124th",
			Link:  link,
		}},
		open: false,
	}, {
		desc: "updated at",
		items: []*feeds.Item{{
			Title:   "Closed - 124th",
			Link:    link,
			Created: time.Now(),
		}},
		open:   false,
		detail: "Updated on",
	}}

	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			feed := startTestServer(t, newFeedGenerator(t, tc.items))
			h, err := NewHandler(&Options{
				Override: tc.override,
				FeedURL:  feed,
				Road:     "124th",
			})
			if err != nil {
				t.Fatalf("NewHandler failed: %v", err)
			}
			server := startTestServer(t, h)
			resp, err := http.Get(server)
			if err != nil {
				t.Fatalf("http.Get(%s) failed: %v", server, err)
			}
			defer resp.Body.Close()
			if sc := resp.StatusCode; sc != http.StatusOK {
				t.Fatalf("Expected OK, got %d", sc)
			}
			body, err := ioutil.ReadAll(resp.Body)
			if err != nil {
				t.Fatalf("Failed to read response (%+v) body: %v", resp, err)
			}
			// If we expect open but it's not...
			if tc.open && !bytes.Contains(body, []byte("124th is Open")) {
				t.Errorf("Expected road to be open, got: %s", body)
			}
			// If we expect closed but it's not...
			if !tc.open && !bytes.Contains(body, []byte("124th is Closed")) {
				t.Errorf("Expected road to be closed, got: %s", body)
			}
			if tc.detail != "" && !bytes.Contains(body, []byte(tc.detail)) {
				t.Errorf("Body missing detail %q: %s", tc.detail, body)
			}
		})
	}
}

// countingHandler records how many requests reached the wrapped handler.
type countingHandler struct {
	h http.Handler
	n atomic.Int64
}

func (c *countingHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	c.n.Add(1)
	c.h.ServeHTTP(w, r)
}

// Only the root path is served. Anything else -- overwhelmingly vulnerability
// scanners probing for PHP and WordPress -- must 404 from the mux without
// reaching the handler, because the handler fetches the road alert feed and
// may spend a Gemini call.
func TestNonRootPathsAreNotServed(t *testing.T) {
	feed := &countingHandler{h: newFeedGenerator(t, nil)}
	feedURL := startTestServer(t, feed)
	h, err := NewHandler(&Options{FeedURL: feedURL, Road: "124th"})
	if err != nil {
		t.Fatalf("NewHandler failed: %v", err)
	}
	server := startTestServer(t, h)

	probes := []string{
		"/wp-login.php",
		"/admin/config.php",
		"/666.php",
		"/wp-content/plugins/core-plugin/include.php",
		"/backup/",
		"/.well-known/acme-challenge/index.php",
		"/some/deep/nested/path",
	}
	for _, p := range probes {
		resp, err := http.Get(server + p)
		if err != nil {
			t.Fatalf("http.Get(%s) failed: %v", p, err)
		}
		resp.Body.Close()
		if sc := resp.StatusCode; sc != http.StatusNotFound {
			t.Errorf("GET %s returned %d, want 404", p, sc)
		}
	}

	// The point of the 404 is avoiding work, not just the status code.
	if n := feed.n.Load(); n != 0 {
		t.Errorf("scanner probes fetched the road alert feed %d times, want 0", n)
	}
}

// The root path must still be served, and it does fetch the feed.
func TestRootIsStillServed(t *testing.T) {
	feed := &countingHandler{h: newFeedGenerator(t, nil)}
	feedURL := startTestServer(t, feed)
	h, err := NewHandler(&Options{FeedURL: feedURL, Road: "124th"})
	if err != nil {
		t.Fatalf("NewHandler failed: %v", err)
	}
	server := startTestServer(t, h)

	resp, err := http.Get(server + "/")
	if err != nil {
		t.Fatalf("http.Get(/) failed: %v", err)
	}
	defer resp.Body.Close()
	if sc := resp.StatusCode; sc != http.StatusOK {
		t.Fatalf("GET / returned %d, want 200", sc)
	}
	if n := feed.n.Load(); n != 1 {
		t.Errorf("GET / fetched the road alert feed %d times, want 1", n)
	}
}

func TestFavicon(t *testing.T) {
	h, err := NewHandler(&Options{})
	if err != nil {
		t.Fatalf("NewHandler failed: %v", err)
	}
	server := startTestServer(t, h)
	resp, err := http.Get(server + "/favicon.ico")
	if err != nil {
		t.Fatalf("http.Get(favicon.ico) failed: %v", err)
	}
	defer resp.Body.Close()
	if sc := resp.StatusCode; sc != http.StatusOK {
		t.Fatalf("Expected OK, got %d", sc)
	}
}
