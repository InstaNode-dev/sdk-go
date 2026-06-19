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

// realMain is the testable entry point. It accepts args, I/O writers, and an
// env-lookup func so tests can drive every branch without spawning a subprocess.
func realMain(args []string, stdout, stderr io.Writer, lookupEnv func(string) string) int {
	fs := flag.NewFlagSet("enterprise-contact", flag.ContinueOnError)
	fs.SetOutput(stderr)
	email := fs.String("email", "", "Contact email address (required)")
	name := fs.String("name", "", "Contact full name (optional)")
	company := fs.String("company", "", "Company name (optional)")
	useCase := fs.String("use_case", "", "Description of requirements (optional)")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	if *email == "" {
		fmt.Fprintln(stderr, "error: -email is required")
		return 1
	}
	opts := []instant.Option{}
	if tok := lookupEnv("INSTANT_TOKEN"); tok != "" {
		opts = append(opts, instant.WithAPIKey(tok))
	}
	if u := lookupEnv("INSTANODE_API_URL"); u != "" {
		opts = append(opts, instant.WithBaseURL(u))
	}
	c := instant.New(opts...)
	if err := run(context.Background(), c, &instant.LeadParams{
		Email:   *email,
		Name:    *name,
		Company: *company,
		UseCase: *useCase,
	}, stdout); err != nil {
		fmt.Fprintf(stderr, "CreateLead: %v\n", err)
		return 1
	}
	return 0
}

func main() {
	os.Exit(realMain(os.Args[1:], os.Stdout, os.Stderr, os.Getenv))
}
