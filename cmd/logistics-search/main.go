package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"

	logisticssearch "github.com/example/logistics-semantic-search"
)

func main() {
	listen := flag.String("listen", ":8080", "HTTP listen address")
	collection := flag.String("collection", "shipment-events", "vector collection")
	dimension := flag.Int("dimension", 1536, "embedding dimension")
	initCollection := flag.Bool("init", false, "create the collection and exit")
	flag.Parse()

	key := os.Getenv("INFRAI_API_KEY")
	if key == "" {
		log.Fatal("INFRAI_API_KEY is required")
	}
	client := logisticssearch.NewClient(key, nil)
	if *initCollection {
		if err := client.CreateCollection(context.Background(), *collection, *dimension); err != nil {
			log.Fatal(err)
		}
		fmt.Printf("collection %q ready\n", *collection)
		return
	}

	service := logisticssearch.SearchService{Client: client, Collection: *collection}
	log.Printf("logistics search listening on %s", *listen)
	log.Fatal(http.ListenAndServe(*listen, service.Handler()))
}
