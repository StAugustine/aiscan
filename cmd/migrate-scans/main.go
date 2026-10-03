// migrate-scans removes the legacy serialized copy of scan records offline.
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
	count, err := migrateScans(context.Background(), *path)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Printf("Converted %d scans to relational storage; IDs and session links retained.\n", count)
}
