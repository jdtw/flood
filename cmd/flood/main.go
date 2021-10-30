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

func main() {
	var (
		port         = flag.Int("port", 8080, "Port to listen on")
		templatePath = flag.String("template_path", "flood.html", "The HTML template path")
		faviconPath  = flag.String("favicon_path", "favicon.ico", "The favicon path")
	)
	flag.Parse()

	handler, err := server.NewHandler(&server.Options{
		FeedURL:      "https://gismaps.kingcounty.gov/roadalert/rss.aspx",
		Road:         "124th",
		Timezone:     "America/Los_Angeles",
		TemplatePath: *templatePath,
		FaviconPath:  *faviconPath,
	})
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("Listening on port %d", *port)
	log.Fatal(http.ListenAndServe(fmt.Sprintf("localhost:%d", *port), handler))
}
