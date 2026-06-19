// enterprise-contact submits an Enterprise interest form to instanode.dev
// when a team's requirements exceed the self-serve Pro tier.
//
// Usage:
//
//	INSTANT_TOKEN=<your-token> go run . \
//	  -email cto@acme.com \
//	  -company "Acme Corp" \
//	  -use_case "We need 10 TB Postgres, dedicated infra, and SOC 2 Type II compliance."
//
// INSTANT_TOKEN is optional — anonymous callers are accepted. When set, the
// lead is automatically linked to the caller's team so the instanode.dev team
// can see current usage without a back-and-forth.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"os"

	"github.com/InstaNode-dev/sdk-go/instant"
)

// run is the testable core: creates the lead, writes output to out.
func run(ctx context.Context, c *instant.Client, params *instant.LeadParams, out io.Writer) error {
	lead, err := c.CreateLead(ctx, params)
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "Enterprise inquiry submitted.\nLead ID: %s\n", lead.ID)
	fmt.Fprintf(out, "The instanode.dev team will follow up at %s\n", params.Email)
	return nil
}

func main() {
	email := flag.String("email", "", "Contact email address (required)")
	name := flag.String("name", "", "Contact full name (optional)")
	company := flag.String("company", "", "Company name (optional)")
	useCase := flag.String("use_case", "", "Description of requirements (optional)")
	flag.Parse()

	if *email == "" {
		fmt.Fprintln(os.Stderr, "error: -email is required")
		flag.Usage()
		os.Exit(1)
	}

	opts := []instant.Option{}
	if tok := os.Getenv("INSTANT_TOKEN"); tok != "" {
		opts = append(opts, instant.WithAPIKey(tok))
	}

	c := instant.New(opts...)
	if err := run(context.Background(), c, &instant.LeadParams{
		Email:   *email,
		Name:    *name,
		Company: *company,
		UseCase: *useCase,
	}, os.Stdout); err != nil {
		log.Fatalf("CreateLead: %v", err)
	}
}
