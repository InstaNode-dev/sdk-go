// provision-all provisions a Postgres database, a Redis cache, and a NATS queue
// in parallel and prints the connection URLs for each. This is useful for
// bootstrapping a new project environment in a single command.
//
// Usage:
//
//	go run .
//
// Set INSTANT_API_KEY to get permanent resources; without it, all resources
// expire after 24 hours.
package main

import (
	"context"
	"fmt"
	"log"
	"sync"

	"github.com/InstaNode-dev/sdk-go/instant"
)

type results struct {
	db    *instant.ProvisionResult
	cache *instant.ProvisionResult
	queue *instant.ProvisionResult
	mu    sync.Mutex
	errs  []error
}

func main() {
	ctx := context.Background()
	client := instant.New()

	fmt.Println("Provisioning all infrastructure...")

	var res results
	var wg sync.WaitGroup

	provision := func(name string, fn func() error) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := fn(); err != nil {
				res.mu.Lock()
				res.errs = append(res.errs, fmt.Errorf("%s: %w", name, err))
				res.mu.Unlock()
			}
		}()
	}

	provision("postgres", func() error {
		db, err := client.ProvisionDatabase(ctx, &instant.ProvisionOpts{Name: "app-db"})
		if err != nil {
			return err
		}
		res.mu.Lock()
		res.db = db
		res.mu.Unlock()
		return nil
	})

	provision("redis", func() error {
		cache, err := client.ProvisionCache(ctx, &instant.ProvisionOpts{Name: "app-cache"})
		if err != nil {
			return err
		}
		res.mu.Lock()
		res.cache = cache
		res.mu.Unlock()
		return nil
	})

	provision("queue", func() error {
		q, err := client.ProvisionQueue(ctx, &instant.ProvisionOpts{Name: "app-queue"})
		if err != nil {
			return err
		}
		res.mu.Lock()
		res.queue = q
		res.mu.Unlock()
		return nil
	})

	wg.Wait()

	if len(res.errs) > 0 {
		for _, e := range res.errs {
			log.Println("error:", e)
		}
		log.Fatalf("%d provisioning error(s)", len(res.errs))
	}

	fmt.Println()
	fmt.Println("=== instant.dev resources ===")
	fmt.Println()

	if res.db != nil {
		fmt.Printf("POSTGRES\n")
		fmt.Printf("  token:   %s\n", res.db.Token)
		fmt.Printf("  url:     %s\n", res.db.ConnectionURL)
		fmt.Printf("  tier:    %s  |  storage: %d MB  |  connections: %d\n",
			res.db.Tier, res.db.Limits.StorageMB, res.db.Limits.Connections)
		fmt.Println()
	}

	if res.cache != nil {
		fmt.Printf("REDIS\n")
		fmt.Printf("  token:   %s\n", res.cache.Token)
		fmt.Printf("  url:     %s\n", res.cache.ConnectionURL)
		if res.cache.KeyPrefix != "" {
			fmt.Printf("  prefix:  %s\n", res.cache.KeyPrefix)
		}
		fmt.Printf("  tier:    %s  |  memory: %d MB\n",
			res.cache.Tier, res.cache.Limits.MemoryMB)
		fmt.Println()
	}

	if res.queue != nil {
		fmt.Printf("NATS QUEUE\n")
		fmt.Printf("  token:   %s\n", res.queue.Token)
		fmt.Printf("  url:     %s\n", res.queue.ConnectionURL)
		fmt.Printf("  tier:    %s  |  storage: %d MB\n",
			res.queue.Tier, res.queue.Limits.StorageMB)
		fmt.Println()
	}

	fmt.Println("Copy the URLs above into your .env file or secret manager.")
	if res.db != nil && res.db.Tier == "anonymous" {
		fmt.Println()
		fmt.Println("These are anonymous (24h TTL). Claim them permanently at https://instant.dev")
	}
}
