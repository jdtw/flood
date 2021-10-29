// flood is a little server that displays the latest information about
// 124th during flood season. Updates are read from the KC road alert
// RSS feed.
package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"

	"github.com/jdtw/flood/internal/server"
)

var (
	port         = flag.Int("port", 8080, "Port to listen on")
	timezone     = flag.String("timezone", "America/Los_Angeles", "Location in which to display update times")
	road         = flag.String("road", "124th", "The road to search for in feed updates")
	templatePath = flag.String("template_path", "flood.html", "The HTML template path")
	// The KC road alert feed. By convention, updates start with "Open", "Closed", or "Restricted".
	feed = flag.String("feed", "https://gismaps.kingcounty.gov/roadalert/rss.aspx", "The RSS feed")
)

func main() {
	flag.Parse()
	handler, err := server.NewServer(*feed, *road, *timezone, *templatePath)
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("Listening on port %d", *port)
	log.Fatal(http.ListenAndServe(fmt.Sprintf("localhost:%d", *port), handler))
}
