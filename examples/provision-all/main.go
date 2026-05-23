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
	"io"
	"log"
	"os"
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
	if err := Run(ctx, client, os.Stdout); err != nil {
		log.Fatal(err)
	}
}

// Run executes the parallel provisioning flow against client, writing
// progress + results to out. Extracted from main() so tests can drive the
// happy + error paths via httptest without printing to stderr or calling
// log.Fatalf. Returns the first joined-error encountered (or nil on success).
//
// Behaviour matches the original main():
//   - Provisions postgres, redis, queue in parallel.
//   - On any failure, prints every error to out and returns a non-nil error
//     summarising the count.
//   - On success, prints the resource list and an anonymous-tier upsell hint
//     when applicable.
func Run(ctx context.Context, client *instant.Client, out io.Writer) error {
	fmt.Fprintln(out, "Provisioning all infrastructure...")

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
			fmt.Fprintln(out, "error:", e)
		}
		return fmt.Errorf("%d provisioning error(s)", len(res.errs))
	}

	fmt.Fprintln(out)
	fmt.Fprintln(out, "=== instant.dev resources ===")
	fmt.Fprintln(out)

	if res.db != nil {
		fmt.Fprintf(out, "POSTGRES\n")
		fmt.Fprintf(out, "  token:   %s\n", res.db.Token)
		fmt.Fprintf(out, "  url:     %s\n", res.db.ConnectionURL)
		fmt.Fprintf(out, "  tier:    %s  |  storage: %d MB  |  connections: %d\n",
			res.db.Tier, res.db.Limits.StorageMB, res.db.Limits.Connections)
		fmt.Fprintln(out)
	}

	if res.cache != nil {
		fmt.Fprintf(out, "REDIS\n")
		fmt.Fprintf(out, "  token:   %s\n", res.cache.Token)
		fmt.Fprintf(out, "  url:     %s\n", res.cache.ConnectionURL)
		if res.cache.KeyPrefix != "" {
			fmt.Fprintf(out, "  prefix:  %s\n", res.cache.KeyPrefix)
		}
		fmt.Fprintf(out, "  tier:    %s  |  memory: %d MB\n",
			res.cache.Tier, res.cache.Limits.MemoryMB)
		fmt.Fprintln(out)
	}

	if res.queue != nil {
		fmt.Fprintf(out, "NATS QUEUE\n")
		fmt.Fprintf(out, "  token:   %s\n", res.queue.Token)
		fmt.Fprintf(out, "  url:     %s\n", res.queue.ConnectionURL)
		fmt.Fprintf(out, "  tier:    %s  |  storage: %d MB\n",
			res.queue.Tier, res.queue.Limits.StorageMB)
		fmt.Fprintln(out)
	}

	fmt.Fprintln(out, "Copy the URLs above into your .env file or secret manager.")
	if res.db != nil && res.db.Tier == "anonymous" {
		fmt.Fprintln(out)
		fmt.Fprintln(out, "These are anonymous (24h TTL). Claim them permanently at https://instant.dev")
	}
	return nil
}
