// Dev token minting for local scripts and tests. Not a public API.
package main

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/ghrushneshrathod/gateflow/libs/go/authn"
)

func main() {
	if len(os.Args) < 3 {
		fmt.Fprintf(os.Stderr, "usage: go run ./scripts/devtoken <principal_type> <scope,scope,...> [subject]\n")
		os.Exit(2)
	}
	ptype := os.Args[1]
	scopes := strings.Split(os.Args[2], ",")
	sub := ptype
	if len(os.Args) > 3 {
		sub = os.Args[3]
	}
	secret := os.Getenv("GATEFLOW_DEV_JWT_SECRET")
	var tok string
	var err error
	switch ptype {
	case authn.PrincipalService:
		tok, err = authn.MintServiceJWT(secret, sub, scopes, 24*time.Hour)
	case authn.PrincipalOperator:
		tok, err = authn.MintOperatorJWT(secret, sub, scopes, 24*time.Hour)
	case authn.PrincipalBuyer:
		tok, err = authn.MintBuyerJWT(secret, sub, scopes, 24*time.Hour)
	default:
		fmt.Fprintf(os.Stderr, "unknown principal_type %q (use buyer|operator|service)\n", ptype)
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "mint: %v\n", err)
		os.Exit(1)
	}
	fmt.Print(tok)
}
