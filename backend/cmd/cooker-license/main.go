// Command cooker-license is the VENDOR-side tool for Cooker self-hosted
// licensing (M2 — docs/launch/01-billing-monetization.md §4). It is NOT
// shipped to customers; it lives with the maintainer who issues licenses.
//
// Subcommands:
//
//	keygen   generate an Ed25519 keypair, writing pub/priv key files.
//	sign     read a private key + claim flags, emit a signed license token.
//	verify   read a public key + a token, print the decoded claims.
//
// Security: the PRIVATE key is the root of trust for every license ever
// issued. It is read from a file (or, for sign, from a path) at runtime
// and is NEVER committed. keygen writes it with 0600 perms; keep it in a
// secrets manager / offline vault, distribute only the public key to
// operators (they set it as COOKER_LICENSE_PUBLIC_KEY).
//
// The token format and signing scheme are defined by — and shared with —
// internal/license, so there is exactly one implementation of the wire
// format. See that package's doc comment.
package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/santapong/cooker/internal/license"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "keygen":
		err = cmdKeygen(os.Args[2:])
	case "sign":
		err = cmdSign(os.Args[2:])
	case "verify":
		err = cmdVerify(os.Args[2:])
	case "-h", "--help", "help":
		usage()
		return
	default:
		fmt.Fprintf(os.Stderr, "unknown subcommand %q\n\n", os.Args[1])
		usage()
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `cooker-license — vendor tool for Cooker self-hosted licensing

usage:
  cooker-license keygen -out <prefix>
        Generate an Ed25519 keypair. Writes <prefix>.key (private, 0600)
        and <prefix>.pub (public, base64). Distribute only the .pub.

  cooker-license sign -key <priv.key> -plan <plan> [flags]
        Emit a signed license token to stdout.
        flags:
          -plan      crew | constellation | free          (required)
          -customer  licensee name                        (required)
          -seats     licensed allowance (0 = unlimited)   (default 0)
          -features  comma-separated extra feature flags
          -expires   validity, e.g. 8760h, or RFC3339, or 0 for perpetual
          -key       path to the private key file         (required)

  cooker-license verify -pub <pub.pub> -token <token>
        Verify a token against a public key and print the claims.
        -token may be "-" or "@<file>" to read from stdin / a file.
`)
}

// ---- keygen ----

func cmdKeygen(args []string) error {
	fs := flag.NewFlagSet("keygen", flag.ContinueOnError)
	out := fs.String("out", "cooker-license", "output file prefix (writes <prefix>.key and <prefix>.pub)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return fmt.Errorf("generate key: %w", err)
	}
	privPath := *out + ".key"
	pubPath := *out + ".pub"
	// Private key: base64(std) of the 64-byte ed25519 private key, 0600.
	if err := os.WriteFile(privPath, []byte(base64.StdEncoding.EncodeToString(priv)+"\n"), 0o600); err != nil {
		return fmt.Errorf("write private key: %w", err)
	}
	// Public key: base64(std) of the 32-byte public key, 0644.
	if err := os.WriteFile(pubPath, []byte(base64.StdEncoding.EncodeToString(pub)+"\n"), 0o644); err != nil {
		return fmt.Errorf("write public key: %w", err)
	}
	fmt.Printf("wrote private key: %s (keep secret, never commit)\n", privPath)
	fmt.Printf("wrote public key:  %s\n", pubPath)
	fmt.Printf("\nset on operators' Cooker:\n  COOKER_LICENSE_PUBLIC_KEY=%s\n", base64.StdEncoding.EncodeToString(pub))
	return nil
}

// ---- sign ----

func cmdSign(args []string) error {
	fs := flag.NewFlagSet("sign", flag.ContinueOnError)
	keyPath := fs.String("key", "", "path to the Ed25519 private key file (required)")
	plan := fs.String("plan", "", "plan tier: crew | constellation | free (required)")
	customer := fs.String("customer", "", "licensee name (required)")
	seats := fs.Int("seats", 0, "licensed seat/replica allowance (0 = unlimited)")
	features := fs.String("features", "", "comma-separated extra feature flags")
	expires := fs.String("expires", "0", "validity: duration (e.g. 8760h), RFC3339 timestamp, or 0 for perpetual")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *keyPath == "" || *plan == "" || *customer == "" {
		return fmt.Errorf("-key, -plan and -customer are required")
	}
	priv, err := loadPrivateKey(*keyPath)
	if err != nil {
		return err
	}
	now := time.Now()
	exp, err := parseExpiry(*expires, now)
	if err != nil {
		return err
	}
	claims := &license.Claims{
		Plan:      *plan,
		Seats:     *seats,
		Features:  splitCSV(*features),
		Customer:  *customer,
		IssuedAt:  now.Unix(),
		ExpiresAt: exp,
	}
	payload, err := license.MarshalClaims(claims)
	if err != nil {
		return err
	}
	token := license.Encode(payload, ed25519.Sign(priv, payload))
	fmt.Println(token)
	return nil
}

// ---- verify ----

func cmdVerify(args []string) error {
	fs := flag.NewFlagSet("verify", flag.ContinueOnError)
	pubPath := fs.String("pub", "", "path to the Ed25519 public key file (required)")
	token := fs.String("token", "", "license token, or @<file>, or - for stdin (required)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *pubPath == "" || *token == "" {
		return fmt.Errorf("-pub and -token are required")
	}
	pubRaw, err := os.ReadFile(*pubPath)
	if err != nil {
		return fmt.Errorf("read public key: %w", err)
	}
	pub, err := license.DecodePublicKey(strings.TrimSpace(string(pubRaw)))
	if err != nil {
		return err
	}
	tok, err := readTokenArg(*token)
	if err != nil {
		return err
	}
	claims, err := license.Verify(tok, pub)
	if err != nil {
		return err
	}
	fmt.Printf("OK — signature valid\n")
	fmt.Printf("  plan:     %s\n", claims.Plan)
	fmt.Printf("  customer: %s\n", claims.Customer)
	fmt.Printf("  seats:    %d\n", claims.Seats)
	fmt.Printf("  features: %s\n", strings.Join(claims.Features, ", "))
	fmt.Printf("  issued:   %s\n", claims.IssuedAtTime().Format(time.RFC3339))
	if exp := claims.ExpiresAtTime(); exp != nil {
		state := "valid"
		if claims.IsExpired(time.Now()) {
			state = "EXPIRED"
		}
		fmt.Printf("  expires:  %s (%s)\n", exp.Format(time.RFC3339), state)
	} else {
		fmt.Printf("  expires:  never (perpetual)\n")
	}
	return nil
}

// ---- helpers ----

// loadPrivateKey reads a base64(std)-encoded Ed25519 private key from a
// file. The file may have surrounding whitespace/newline.
func loadPrivateKey(path string) (ed25519.PrivateKey, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read private key: %w", err)
	}
	dec, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(raw)))
	if err != nil {
		return nil, fmt.Errorf("decode private key (expected base64): %w", err)
	}
	if len(dec) != ed25519.PrivateKeySize {
		return nil, fmt.Errorf("private key is %d bytes, want %d", len(dec), ed25519.PrivateKeySize)
	}
	return ed25519.PrivateKey(dec), nil
}

// parseExpiry interprets the -expires flag: "0"/"" => perpetual (0); a
// Go duration (e.g. 8760h) => now+duration; otherwise an RFC3339 stamp.
func parseExpiry(s string, now time.Time) (int64, error) {
	s = strings.TrimSpace(s)
	if s == "" || s == "0" {
		return 0, nil
	}
	if d, err := time.ParseDuration(s); err == nil {
		return now.Add(d).Unix(), nil
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t.Unix(), nil
	}
	return 0, fmt.Errorf("invalid -expires %q: want a duration (8760h), RFC3339, or 0", s)
}

func splitCSV(s string) []string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// readTokenArg resolves the -token value: "@path" reads a file, "-" reads
// stdin, anything else is the literal token.
func readTokenArg(v string) (string, error) {
	switch {
	case v == "-":
		b, err := io.ReadAll(os.Stdin)
		return strings.TrimSpace(string(b)), err
	case strings.HasPrefix(v, "@"):
		b, err := os.ReadFile(v[1:])
		if err != nil {
			return "", fmt.Errorf("read token file: %w", err)
		}
		return strings.TrimSpace(string(b)), nil
	default:
		return strings.TrimSpace(v), nil
	}
}
