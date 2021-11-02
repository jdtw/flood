// package server implements the HTTP handler that parses the road alert feed
// and populates the HTML template based on the closure information.
package server

import (
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

// The data directory contains templates and the favicon.
//go:embed data
var data embed.FS

// templateData contains the fields needed to populate the flood.html
// template.
type templateData struct {
	Road      string
	Open      bool
	Detail    string
	Link      string
	Published string
}

// handler is the HTTP handler for the flood detection service.
type handler struct {
	feedURL string
	road    string
	loc     *time.Location
	templ   *template.Template
	parser  *gofeed.Parser
	*http.ServeMux
}

// Options specifies the feed URL for road alerts, the road to check for closures
// in those alerts, and the timezone in which to display time to the user.
// FeedURL and Road are required. Timezone defaults to UTC.
type Options struct {
	FeedURL  string
	Road     string
	Timezone string
}

// NewHandler returns an http.Handler for
func NewHandler(opts *Options) (http.Handler, error) {
	loc, err := time.LoadLocation(opts.Timezone)
	if err != nil {
		return nil, err
	}

	t, err := template.ParseFS(data, "data/flood.html")
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

// flood pulls the latest road alerts, gets the latest for the given road,
// and populates the template based on the results.
func (h *handler) flood() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		feed, err := h.parser.ParseURLWithContext(h.feedURL, r.Context())
		if err != nil {
			internalError(w, "failed to fetch the road alert feed: %v", err)
			return
		}

		// The road is assumed to be open by default. It is only considered
		// closed if the item is mentioned in the feed and the feed item's
		// title starts with the literal "Closed". This is potentially fragile,
		// but the KC RSS feed seems to follow this convention.
		td := &templateData{Open: true, Road: h.road}
		for _, i := range feed.Items {
			if strings.Contains(i.Title, h.road) {
				td.Open = !strings.HasPrefix(i.Title, "Closed")
				td.Detail = i.Title
				td.Link = i.Link
				if i.PublishedParsed != nil {
					td.Published = i.PublishedParsed.In(h.loc).Format(time.RFC822)
				}
				break
			}
		}

		if err := h.templ.Execute(w, td); err != nil {
			internalError(w, "internal error: %v", err)
		}
	}
}

// internalError responds with a 500 code and the given message.
func internalError(w http.ResponseWriter, format string, v ...interface{}) {
	error := fmt.Sprintf(format, v...)
	log.Print(error)
	http.Error(w, error, http.StatusInternalServerError)
}

// logged logs the HTTP request, respecting the X-Forwarded-For header to support
// running behind a proxy.
func logged(hf http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		remote := strings.Join(r.Header["X-Forwarded-For"], " ")
		if remote == "" {
			remote = r.RemoteAddr
		}
		log.Printf("%s %s %s %s", remote, r.Method, r.URL, r.UserAgent())
		hf(w, r)
	}
}
