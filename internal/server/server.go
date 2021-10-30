// package server implements the HTTP handler that parses the road alert feed
// and populates the HTML template based on the closure information.
package server

import (
	"context"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/mmcdole/gofeed"
)

type Server struct {
	feedURL string
	road    string
	loc     *time.Location
	templ   *template.Template
	parser  *gofeed.Parser
	*http.ServeMux
}

func NewServer(feedURL string, road string, tz string, templatePath string) (*Server, error) {
	loc, err := time.LoadLocation(tz)
	if err != nil {
		return nil, err
	}

	t, err := template.ParseFiles(templatePath)
	if err != nil {
		return nil, err
	}

	s := &Server{feedURL, road, loc, t, gofeed.NewParser(), http.NewServeMux()}
	s.HandleFunc("/", s.handler())
	return s, nil
}

func (s *Server) handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		item, err := s.getLatestAlert(r.Context())
		if err != nil {
			internalError(w, "failed to fetch the road alert feed: %v", err)
			return
		}
		d := s.templateData(item)
		if err := s.templ.Execute(w, d); err != nil {
			internalError(w, "internal error: %v", err)
		}
		remote := strings.Join(r.Header["X-Forwarded-For"], ", ")
		if remote == "" {
			remote = r.RemoteAddr
		}
		log.Printf("%s %s %s %s", remote, r.Method, r.URL.Path, r.UserAgent())
	}
}

func (s *Server) templateData(item *gofeed.Item) interface{} {
	d := struct {
		Road      string
		Open      bool
		Detail    string
		Link      string
		Published string
	}{Open: true, Road: s.road}
	if item != nil {
		d.Open = !strings.HasPrefix(item.Title, "Closed")
		d.Detail = item.Title
		d.Link = item.Link
		if item.PublishedParsed != nil {
			d.Published = item.PublishedParsed.In(s.loc).Format(time.RFC822)
		}
	}
	return d
}

// Return the first item that contains the given substr or nil if it's not mentioned.
func (s *Server) getLatestAlert(ctx context.Context) (*gofeed.Item, error) {
	fp := gofeed.NewParser()
	feed, err := fp.ParseURLWithContext(s.feedURL, ctx)
	if err != nil {
		return nil, err
	}
	for _, i := range feed.Items {
		if strings.Contains(i.Title, s.road) {
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
