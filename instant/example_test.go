package instant_test

import (
	"context"
	"fmt"
	"log"

	"github.com/InstaNode-dev/sdk-go/instant"
)

func ExampleClient_ProvisionDatabase() {
	ctx := context.Background()
	client := instant.New(instant.WithBaseURL("http://localhost:30080"))

	db, err := client.ProvisionDatabase(ctx, &instant.ProvisionOpts{Name: "app-db"})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("postgres URL:", db.ConnectionURL)
	fmt.Println("storage limit:", db.Limits.StorageMB, "MB")
	_ = db
}

func ExampleClient_ProvisionCache() {
	ctx := context.Background()
	client := instant.New(instant.WithBaseURL("http://localhost:30080"))

	cache, err := client.ProvisionCache(ctx, &instant.ProvisionOpts{Name: "app-cache"})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("redis URL:", cache.ConnectionURL)
	_ = cache
}

func ExampleClient_ProvisionMongoDB() {
	ctx := context.Background()
	client := instant.New(instant.WithBaseURL("http://localhost:30080"))

	mdb, err := client.ProvisionMongoDB(ctx, &instant.ProvisionOpts{Name: "app-mongo"})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("mongodb URL:", mdb.ConnectionURL)
	_ = mdb
}

func ExampleClient_ProvisionQueue() {
	ctx := context.Background()
	client := instant.New(instant.WithBaseURL("http://localhost:30080"))

	q, err := client.ProvisionQueue(ctx, &instant.ProvisionOpts{Name: "app-queue"})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("nats URL:", q.ConnectionURL)
	_ = q
}

func ExampleClient_ListResources() {
	ctx := context.Background()
	client := instant.New(
		instant.WithBaseURL("http://localhost:30080"),
		instant.WithAPIKey("sk_live_your_key_here"),
	)

	list, err := client.ListResources(ctx)
	if err != nil {
		log.Fatal(err)
	}
	for _, r := range list.Items {
		fmt.Printf("%s  %s  %s\n", r.ResourceType, r.Token, r.Status)
	}
	_ = list
}

func ExampleClient_Claim() {
	ctx := context.Background()
	client := instant.New(instant.WithBaseURL("http://localhost:30080"))

	result, err := client.Claim(ctx, instant.ClaimOpts{
		JWT:      "eyJhbGci...", // from ?t= query param on the upgrade URL
		Email:    "dev@example.com",
		TeamName: "Acme Corp",
	})
	if instant.IsConflict(err) {
		fmt.Println("already claimed — log in at https://instant.dev")
		return
	}
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("team_id:", result.TeamID)
}

func ExampleClient_ClaimTokens() {
	ctx := context.Background()
	client := instant.New(instant.WithBaseURL("http://localhost:30080"))

	// Provision anonymously first — a name is required
	cache, _ := client.ProvisionCache(ctx, &instant.ProvisionOpts{Name: "app-cache"})
	db, _ := client.ProvisionDatabase(ctx, &instant.ProvisionOpts{Name: "app-db"})

	// Then associate both tokens with an existing authenticated account
	result, err := client.ClaimTokens(ctx, "sk_live_your_key_here", []string{
		cache.Token,
		db.Token,
	})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("claimed:", result.Message)
}

func ExampleNew() {
	// Anonymous client — no account needed
	anon := instant.New()
	_ = anon

	// Authenticated client via environment variable
	// Set INSTANT_API_KEY=sk_live_... and then:
	auth := instant.New()
	_ = auth

	// Explicit API key
	explicit := instant.New(instant.WithAPIKey("sk_live_..."))
	_ = explicit

	// Local development server
	local := instant.New(instant.WithBaseURL("http://localhost:30080"))
	_ = local
}

func ExampleIsNotFound() {
	ctx := context.Background()
	client := instant.New(
		instant.WithBaseURL("http://localhost:30080"),
		instant.WithAPIKey("sk_live_your_key_here"),
	)

	_, err := client.GetResource(ctx, "nonexistent-token")
	if instant.IsNotFound(err) {
		fmt.Println("resource not found")
		return
	}
	if err != nil {
		log.Fatal(err)
	}
}
