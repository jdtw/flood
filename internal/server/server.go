// package server implements the HTTP handler that parses the road alert feed
// and populates the HTML template based on the closure information.
package server

import (
	"bytes"
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
	"jdtw.dev/flood/internal/genai"
)

type Override int

const (
	None Override = iota
	Open
	Closed
)

// The data directory contains templates and the favicon.
//
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
	override Override
	feedURL  string
	road     string
	loc      *time.Location
	templ    *template.Template
	parser   *gofeed.Parser
	analyzer *genai.TrafficAnalyzer
	*http.ServeMux
}

// Options specifies the feed URL for road alerts, the road to check for closures
// in those alerts, and the timezone in which to display time to the user.
// FeedURL and Road are required. Timezone defaults to UTC.
type Options struct {
	// If override isn't None, use the manual open/closed status
	// instead of the feed data (useful for when the feed isn't updated but
	// the cameras clearly show that the road is open.)
	Override Override
	FeedURL  string
	Road     string
	Timezone string
	// Optional. If empty, Gemini analysis is disabled.
	GeminiAPIKey string
	GeminiModel  string
	// CameraURLs to analyze with Gemini. Ignored if the API key is empty.
	CameraURLs []string
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

	var analyzer *genai.TrafficAnalyzer
	if opts.GeminiAPIKey != "" {
		analyzer, err = genai.NewTrafficAnalyzer(context.Background(), opts.GeminiAPIKey, opts.GeminiModel, opts.CameraURLs)
		if err != nil {
			return nil, err
		}
	}

	s := &handler{
		override: opts.Override,
		feedURL:  opts.FeedURL,
		road:     opts.Road,
		loc:      loc,
		templ:    t,
		parser:   gofeed.NewParser(),
		analyzer: analyzer,
		ServeMux: http.NewServeMux(),
	}
	s.Handle("/favicon.ico", http.FileServer(http.FS(fs)))
	s.HandleFunc("/", logged(s.flood()))

	return s, nil
}

// flood pulls the latest road alerts, gets the latest for the given road,
// and populates the template based on the results.
func (h *handler) flood() http.HandlerFunc {
	// If the manual override is set, execute the template once
	if h.override != None {
		buffy := &bytes.Buffer{}
		td := &templateData{
			Open: h.override == Open,
			Road: h.road,
		}
		log.Printf("Manual override! open=%t", td.Open)
		if err := h.templ.Execute(buffy, td); err != nil {
			log.Fatalf("Failed to execute manual override: %v", err)
		}
		return func(w http.ResponseWriter, r *http.Request) {
			w.Write(buffy.Bytes())
		}
	}

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
					td.Published = i.PublishedParsed.In(h.loc).Format(time.RFC1123)
				}
				break
			}
		}

		// Let the AI override the RSS feed if it disagrees, since the feed is
		// notoriously flaky and slow to update.
		if h.analyzer != nil {
			open, detail, updated, err := h.analyzer.IsRoadOpen(r.Context())
			if err == nil && td.Open != open {
				td = &templateData{
					Open:      open,
					Road:      h.road,
					Detail:    "✨ Analysis: " + detail,
					Published: updated.In(h.loc).Format(time.RFC1123),
				}
				log.Printf("AI Analysis: %s", detail)
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
