// package server implements the HTTP handler that parses the road alert feed
// and populates the HTML template based on the closure information.
package server

import (
	"context"
	"embed"
	"fmt"
	"html/template"
	"io/fs"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/mmcdole/gofeed"
)

//go:embed data
var data embed.FS

type handler struct {
	feedURL string
	road    string
	loc     *time.Location
	templ   *template.Template
	parser  *gofeed.Parser
	*http.ServeMux
}

type Options struct {
	// The RSS feed URL for road alerts.
	FeedURL string
	// The road to search for in the alerts.
	Road string
	// The timezone for which to display times to the end user.
	Timezone string
}

func NewHandler(opts *Options) (http.Handler, error) {
	loc, err := time.LoadLocation(opts.Timezone)
	if err != nil {
		return nil, err
	}

	t, err := template.ParseFS(data, "data/flood.tmpl.html")
	if err != nil {
		return nil, err
	}

	fs, err := fs.Sub(data, "data")
	if err != nil {
		return nil, err
	}

	s := &handler{opts.FeedURL, opts.Road, loc, t, gofeed.NewParser(), http.NewServeMux()}
	s.Handle("/favicon.ico", http.FileServer(http.FS(fs)))
	s.HandleFunc("/", logged(s.flood()))
	return s, nil
}

func (h *handler) flood() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		item, err := h.getLatestAlert(r.Context())
		if err != nil {
			internalError(w, "failed to fetch the road alert feed: %v", err)
			return
		}
		d := h.templateData(item)
		if err := h.templ.Execute(w, d); err != nil {
			internalError(w, "internal error: %v", err)
		}
	}
}

func (h *handler) templateData(item *gofeed.Item) interface{} {
	d := struct {
		Road      string
		Open      bool
		Detail    string
		Link      string
		Published string
	}{Open: true, Road: h.road}
	if item != nil {
		d.Open = !strings.HasPrefix(item.Title, "Closed")
		d.Detail = item.Title
		d.Link = item.Link
		if item.PublishedParsed != nil {
			d.Published = item.PublishedParsed.In(h.loc).Format(time.RFC822)
		}
	}
	return d
}

// Return the first item that contains the given substr or nil if it's not mentioned.
func (h *handler) getLatestAlert(ctx context.Context) (*gofeed.Item, error) {
	fp := gofeed.NewParser()
	feed, err := fp.ParseURLWithContext(h.feedURL, ctx)
	if err != nil {
		return nil, err
	}
	for _, i := range feed.Items {
		if strings.Contains(i.Title, h.road) {
			return i, nil
		}
	}
	return nil, nil
}

func internalError(w http.ResponseWriter, format string, v ...interface{}) {
	error := fmt.Sprintf(format, v...)
	log.Print(error)
	http.Error(w, error, http.StatusInternalServerError)
}

func logged(hf http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		remote := strings.Join(r.Header["X-Forwarded-For"], "; ")
		if remote == "" {
			remote = r.RemoteAddr
		}
		log.Printf("%s %s %s %s", remote, r.Method, r.URL.Path, r.UserAgent())
		hf(w, r)
	}
}
