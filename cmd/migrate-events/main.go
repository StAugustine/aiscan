// migrate-events is an offline, one-time conversion of the event_json column.
// The server itself reads and writes only the current protobuf schema.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
)

func main() {
	path := flag.String("db", "", "Existing SQLite database; stop the server before converting")
	flag.Parse()
	if *path == "" || flag.NArg() != 0 {
		flag.Usage()
		os.Exit(2)
	}
	count, err := migrateEvents(context.Background(), *path)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Printf("Converted %d events to protobuf; event IDs and cursors retained.\n", count)
}
