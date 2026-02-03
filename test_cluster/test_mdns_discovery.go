package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/hashicorp/mdns"
)

func main() {
	fmt.Println("Browsing for _underleaf._tcp.local services...")
	fmt.Println(strings.Repeat("=", 60))

	entries := make(chan *mdns.ServiceEntry, 20)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	go func() {
		params := &mdns.QueryParam{
			Service:             "_underleaf._tcp",
			Domain:              "local.",
			Timeout:             10 * time.Second,
			Entries:             entries,
			DisableIPv6:         true,
			WantUnicastResponse: false,
		}
		if err := mdns.Query(params); err != nil {
			fmt.Printf("Query error: %v\n", err)
		}
		close(entries)
	}()

	count := 0
	for entry := range entries {
		count++
		fmt.Printf("\n✓ Service #%d found:\n", count)
		fmt.Printf("  Name: %s\n", entry.Name)
		fmt.Printf("  Host: %s\n", entry.Host)
		fmt.Printf("  AddrV4: %v\n", entry.AddrV4)
		fmt.Printf("  Port: %d\n", entry.Port)
		fmt.Printf("  Info: %s\n", entry.Info)
		fmt.Printf("  InfoFields: %v\n", entry.InfoFields)
	}

	<-ctx.Done()

	fmt.Println(strings.Repeat("=", 60))
	fmt.Printf("Total services found: %d\n", count)
}
