// Command devlicense issues a dev-only license JWT and prints it as JSON.
//
// Usage:
//
//	go run ./cmd/devlicense                                # all features, 90 days
//	go run ./cmd/devlicense -features goals,documents      # scoped license (e.g. for feature-gate tests)
//	go run ./cmd/devlicense -days 1 -max-users 5           # short-lived, user-capped
//
// Output: {"license_key": "...", "public_key": "-----BEGIN PUBLIC KEY-----..."}
//
// Intended for CI pipelines, E2E scripts, and local troubleshooting. Never for production.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/automax/backend/internal/licensing/devlicense"
)

func main() {
	var (
		featuresFlag = flag.String("features", "", "Comma-separated feature codes (default: all catalog features)")
		daysFlag     = flag.Int("days", 90, "Expiry in days")
		usersFlag    = flag.Int("max-users", 1000, "Max user cap")
	)
	flag.Parse()

	var features []string
	if s := strings.TrimSpace(*featuresFlag); s != "" {
		for _, f := range strings.Split(s, ",") {
			if t := strings.TrimSpace(f); t != "" {
				features = append(features, t)
			}
		}
	}

	token, pubKey, err := devlicense.Issue(features, *daysFlag, *usersFlag)
	if err != nil {
		fmt.Fprintf(os.Stderr, "devlicense: %v\n", err)
		os.Exit(1)
	}

	out := map[string]string{
		"license_key": token,
		"public_key":  pubKey,
	}
	if err := json.NewEncoder(os.Stdout).Encode(out); err != nil {
		fmt.Fprintf(os.Stderr, "devlicense: encode: %v\n", err)
		os.Exit(1)
	}
}
