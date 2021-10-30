package server

import (
	"bytes"
	"context"
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"sync"
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
	link := &feeds.Link{Href: "http://localhost"}
	tests := []struct {
		desc   string
		items  []*feeds.Item
		detail string
		open   bool
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
		detail: "Updated at",
	}}

	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			feed := startTestServer(t, newFeedGenerator(t, tc.items))
			h, err := NewHandler(&Options{
				FeedURL:      feed,
				Road:         "124th",
				Timezone:     "America/Los_Angeles",
				TemplatePath: "flood.html",
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

func TestFavicon(t *testing.T) {
	h, err := NewHandler(&Options{
		TemplatePath: "flood.html",
		FaviconPath:  "favicon.ico",
	})
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
