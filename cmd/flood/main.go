// flood is a little server that displays the latest information about
// 124th during flood season. Updates are read from the KC road alert
// RSS feed.
package main

import (
	"context"
	"flag"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/mmcdole/gofeed"
)

var (
	port         = flag.Int("port", 8080, "Port to listen on")
	timezone     = flag.String("timezone", "America/Los_Angeles", "Location in which to display update times")
	road         = flag.String("road", "124th", "The road to search for in feed updates")
	templatePath = flag.String("template_path", "flood.html", "The HTML template path")
)

// The KC road alert feed. By convention, updates start with "Open", "Closed", or "Restricted".
const roadAlertFeed = "https://gismaps.kingcounty.gov/roadalert/rss.aspx"

func main() {
	flag.Parse()

	t := template.Must(template.ParseFiles(*templatePath))

	loc, err := time.LoadLocation(*timezone)
	if err != nil {
		log.Fatalf("time.LoadLocation failed: %v", err)
	}

	log.Printf("Listening on port %d", *port)
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		item, err := getLatestAlert(r.Context(), *road)
		if err != nil {
			internalError(w, "failed to fetch the road alert feed: %v", err)
			return
		}
		data := templateData(item, loc)
		if err := t.Execute(w, data); err != nil {
			internalError(w, "internal error: %v", err)
			return
		}
	})
	log.Fatal(http.ListenAndServe(fmt.Sprintf("localhost:%d", *port), nil))
}

type data struct {
	Road      string
	Open      bool
	Detail    string
	Link      string
	Published string
}

// templateData populates the HTML template data given a feed item, which may be nil.
func templateData(item *gofeed.Item, loc *time.Location) *data {
	// The road is considered open unless explicitly closed.
	d := &data{Open: true, Road: *road}
	if item != nil {
		d.Open = !strings.HasPrefix(item.Title, "Closed")
		d.Detail = item.Title
		d.Link = item.Link
		d.Published = item.PublishedParsed.In(loc).Format(time.RFC822)
	}
	return d
}

// Return the first item that contains the given substr or nil if it's not mentioned.
func getLatestAlert(ctx context.Context, substr string) (*gofeed.Item, error) {
	fp := gofeed.NewParser()
	feed, err := fp.ParseURLWithContext(roadAlertFeed, ctx)
	if err != nil {
		return nil, err
	}
	for _, i := range feed.Items {
		if strings.Contains(i.Title, substr) {
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
